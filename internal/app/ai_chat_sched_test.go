package app

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/workflow/schedule"
)

// CMP-43: a chat runs again by itself at set times. The user sets it
// (when, message, Cancel-first card); the AI proposes it with
// chat_schedule_set (always a Cancel-first card); a due slot sends the
// message as a new turn in that chat.

func chatSchedModel(t *testing.T) (*Model, *wfxClock) {
	t.Helper()
	m := aiTestModel(t)
	c := &wfxClock{now: time.Date(2026, 11, 7, 8, 0, 0, 0, time.Local)}
	m.ai.chatSched.clock = c
	m.ai.conv.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "is the nightly build green?"},
		{Role: ai.StoredAssistant, Text: "yes"},
	}
	if err := m.ai.store.Save(&m.ai.conv); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.aiChatSchedStop)
	return &m, c
}

func TestAIChatSchedulePaletteIDs(t *testing.T) {
	m, _ := chatSchedModel(t)
	m.ai.provider = fakeAIProvider{reply: "x"}
	for _, id := range []string{aiChatSchedPaletteID, aiChatUnschedPaletteID} {
		if _, ok := aiActionPolicies[id]; !ok {
			t.Fatalf("%s has no AI policy", id)
		}
		if _, ok := webPaletteUserOnly[id]; !ok {
			t.Fatalf("%s is not in webPaletteUserOnly", id)
		}
	}
	if _, ok := m.dispatchAIPalette(aiChatSchedPaletteID); !ok || !m.promptOpen || m.promptKind != promptKindAIChatSchedWhen {
		t.Fatal("AI: Schedule This Chat… does not ask when")
	}
}

func TestAIChatUnscheduleWorksWithoutProvider(t *testing.T) {
	m, _ := chatSchedModel(t)
	m.ai.conv.Schedule = &ai.ChatSchedule{When: "at 09:00", Prompt: "x"}
	if _, ok := m.dispatchAIPalette(aiChatUnschedPaletteID); !ok || !m.confirmOpen || m.confirmKind != confirmKindAIChatUnsched {
		t.Fatal("AI: Stop Chat Schedule needs no provider to stop paid runs")
	}
}

func TestAIChatScheduleNeedsAMessage(t *testing.T) {
	m := aiTestModel(t)
	m.openAIChatSchedule()
	if m.promptOpen {
		t.Fatal("an empty chat must not be scheduled")
	}
}

func TestAIChatScheduleCancelSavesNothing(t *testing.T) {
	m, _ := chatSchedModel(t)
	m.openAIChatSchedule()
	if !m.promptOpen || m.promptKind != promptKindAIChatSchedWhen {
		t.Fatalf("when prompt not open: %v %v", m.promptOpen, m.promptKind)
	}
	m.onAIChatSchedWhen("at 09:00")
	if m.promptKind != promptKindAIChatSchedPrompt || m.prompt.Value() != "is the nightly build green?" {
		t.Fatalf("message prompt %v %q", m.promptKind, m.prompt.Value())
	}
	m.onAIChatSchedPrompt("is the nightly build green?")
	if !m.confirmOpen || m.confirmKind != confirmKindAIChatSched || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card %v %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	m.answerAIChatSchedCard("cancel")
	if m.ai.conv.Schedule != nil {
		t.Fatal("Cancel saved a schedule")
	}
	if _, ok := m.aiChatSchedNext(m.ai.conv.ID); ok {
		t.Fatal("Cancel scheduled the chat")
	}
}

func TestAIChatScheduleBadTimeIsRefused(t *testing.T) {
	m, _ := chatSchedModel(t)
	m.openAIChatSchedule()
	m.onAIChatSchedWhen("*/5 * * * *")
	if m.promptKind == promptKindAIChatSchedPrompt {
		t.Fatal("a schedule more often than hourly went on")
	}
}

func TestAIChatScheduleSavesAndStops(t *testing.T) {
	m, c := chatSchedModel(t)
	m.openAIChatSchedule()
	m.onAIChatSchedWhen("at 09:00")
	m.onAIChatSchedPrompt("check the build")
	m.answerAIChatSchedCard("schedule")
	sc := m.ai.conv.Schedule
	if sc == nil || sc.When != "at 09:00" || sc.Prompt != "check the build" || sc.By != ai.ChatSchedByUser {
		t.Fatalf("schedule %+v", sc)
	}
	next, ok := m.aiChatSchedNext(m.ai.conv.ID)
	want := time.Date(2026, 11, 7, 9, 0, 0, 0, time.Local)
	if !ok || !next.Equal(want) {
		t.Fatalf("next %v %v, want %v (clock %v)", next, ok, want, c.Now())
	}
	stored, err := m.ai.store.Load(m.ai.conv.ID)
	if err != nil || stored.Schedule == nil {
		t.Fatalf("schedule not in the chat file: %v", err)
	}
	m.openAIChatUnschedule()
	if !m.confirmOpen || m.confirmKind != confirmKindAIChatUnsched || m.confirm.SafeID() != "cancel" {
		t.Fatal("stop card is not Cancel-first")
	}
	m.answerAIChatUnschedCard("stop")
	if m.ai.conv.Schedule != nil {
		t.Fatal("schedule still set")
	}
	if _, ok := m.aiChatSchedNext(m.ai.conv.ID); ok {
		t.Fatal("job still scheduled")
	}
}

func TestAIChatScheduleBootLoadsStoredSchedules(t *testing.T) {
	m, _ := chatSchedModel(t)
	m.ai.conv.Schedule = &ai.ChatSchedule{When: "weekdays 09:00", Prompt: "triage", By: ai.ChatSchedByUser}
	if err := m.ai.store.Save(&m.ai.conv); err != nil {
		t.Fatal(err)
	}
	cmd, ok := m.handleAIChatSchedMsg(aiChatSchedBootMsg{})
	if !ok || cmd == nil {
		t.Fatal("boot not handled")
	}
	m.handleAIChatSchedMsg(cmd())
	if _, ok := m.aiChatSchedNext(m.ai.conv.ID); !ok {
		t.Fatal("stored schedule not loaded at start")
	}
}

func TestAIChatScheduleDueSendsTheMessage(t *testing.T) {
	m, c := chatSchedModel(t)
	m.ai.provider = fakeAIProvider{reply: "build is green"}
	m.ai.cfg = ai.Config{Kind: ai.KindOllama, Model: "fake-1"}
	if _, _, err := m.aiChatSchedApply(ai.ChatSchedule{When: "at 09:00", Prompt: "check the build", By: ai.ChatSchedByUser}); err != nil {
		t.Fatal(err)
	}
	h := m.ai.chatSched.host
	slot := time.Date(2026, 11, 7, 9, 0, 0, 0, time.Local)
	c.mu.Lock()
	c.now = slot
	c.mu.Unlock()
	h.s.Tick(slot)
	msg := chatSchedRecv(t, h)
	due, ok := msg.(aiChatSchedDueMsg)
	if !ok {
		t.Fatalf("got %T, want a due slot", msg)
	}
	m.handleAIChatSchedMsg(due)
	users := aiUserTexts(m.ai)
	if users[len(users)-1] != "check the build" {
		t.Fatalf("user messages %q", users)
	}
	note := false
	for _, s := range m.ai.conv.Messages {
		note = note || (s.Role == ai.StoredNote && strings.Contains(s.Text, "Scheduled run"))
	}
	if !note {
		t.Fatal("no scheduled-run note")
	}
	ev, ok := chatSchedRecv(t, h).(aiChatSchedEventMsg)
	if !ok || ev.ev.Outcome != schedule.Ran {
		t.Fatalf("event %+v", ev)
	}
}

func TestAIChatScheduleDueSkipsWhileTyping(t *testing.T) {
	m, _ := chatSchedModel(t)
	m.ai.provider = fakeAIProvider{reply: "x"}
	m.ai.cfg = ai.Config{Kind: ai.KindOllama, Model: "fake-1"}
	if _, _, err := m.aiChatSchedApply(ai.ChatSchedule{When: "at 09:00", Prompt: "check", By: ai.ChatSchedByUser}); err != nil {
		t.Fatal(err)
	}
	id := m.ai.conv.ID
	m.startNewAIChat()
	m.ai.input = []rune("half a thought")
	_, err := m.aiChatSchedRun(schedule.Due{ID: id, Slot: time.Now()})
	if err == nil || !strings.Contains(err.Error(), "typing") {
		t.Fatalf("err %v", err)
	}
	if string(m.ai.input) != "half a thought" {
		t.Fatal("the draft was touched")
	}
}

func TestAIChatScheduleToolIsCancelFirst(t *testing.T) {
	m, _ := chatSchedModel(t)
	var set ai.Tool
	for _, tl := range m.aiChatSchedTools(nil) {
		if tl.Name == "chat_schedule_set" {
			set = tl
		}
	}
	if !set.Mutating || set.Class != "" {
		t.Fatal("chat_schedule_set must always ask")
	}
	b := aiToolCardButtons(set)
	if b[0].ID != "deny" || b[0].Title != "Cancel" || b[1].Title != "Schedule chat" {
		t.Fatalf("buttons %+v", b)
	}
	for _, x := range b {
		if x.ID == "always" {
			t.Fatal("no always-allow for paid scheduled turns")
		}
	}
	if err := set.Validate(map[string]any{"when": "*/5 * * * *", "prompt": "x"}); err == nil {
		t.Fatal("too often accepted")
	}
	if err := set.Validate(map[string]any{"when": "at 09:00", "prompt": "check"}); err != nil {
		t.Fatal(err)
	}
}

func TestAIChatScheduleToolSetsByAI(t *testing.T) {
	m, _ := chatSchedModel(t)
	call := func(_ context.Context, f aiIDEFunc) (string, error) {
		out, _, err := f(m)
		return out, err
	}
	tools := map[string]ai.Tool{}
	for _, tl := range m.aiChatSchedTools(call) {
		tools[tl.Name] = tl
	}
	out, err := tools["chat_schedule_set"].Run(context.Background(), map[string]any{"when": "weekdays 09:00", "prompt": "triage new issues"})
	if err != nil || !strings.Contains(out, "Scheduled") {
		t.Fatalf("%q %v", out, err)
	}
	if sc := m.ai.conv.Schedule; sc == nil || sc.By != ai.ChatSchedByAI {
		t.Fatalf("schedule %+v", sc)
	}
	out, _ = tools["chat_schedule_read"].Run(context.Background(), map[string]any{})
	if !strings.Contains(out, "weekdays 09:00") || !strings.Contains(out, "triage new issues") {
		t.Fatalf("read %q", out)
	}
	tools["chat_schedule_clear"].Run(context.Background(), map[string]any{})
	if m.ai.conv.Schedule != nil {
		t.Fatal("clear left the schedule")
	}
}

func chatSchedRecv(t *testing.T, h *aiChatSchedHost) tea.Msg {
	t.Helper()
	select {
	case msg := <-h.ch:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("no scheduler message")
	}
	return nil
}
