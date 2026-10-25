package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/aimemory"
	"github.com/amin-jalali/termocode/internal/clock"
)

// OM-2: the extractor. After a tool-using or failed turn, with memory on,
// the quick model slot writes up to three lessons.

// memFailAIProvider ends every answer with an error.
type memFailAIProvider struct{}

func (memFailAIProvider) Name() string        { return "fail" }
func (memFailAIProvider) Configured() bool    { return true }
func (memFailAIProvider) SupportsTools() bool { return true }
func (memFailAIProvider) Stream(_ context.Context, _ ai.Request) <-chan ai.Chunk {
	ch := make(chan ai.Chunk, 1)
	ch <- ai.Chunk{Done: true, Err: errors.New("HTTP 500: the build server is down")}
	close(ch)
	return ch
}

// waitAIProvider blocks until its request is cancelled.
type waitAIProvider struct{}

func (waitAIProvider) Name() string        { return "wait" }
func (waitAIProvider) Configured() bool    { return true }
func (waitAIProvider) SupportsTools() bool { return true }
func (waitAIProvider) Stream(ctx context.Context, _ ai.Request) <-chan ai.Chunk {
	ch := make(chan ai.Chunk, 1)
	go func() {
		<-ctx.Done()
		ch <- ai.Chunk{Done: true, Err: ctx.Err()}
		close(ch)
	}()
	return ch
}

const aiMemoryReplyOne = "```json\n{\"lessons\":[{\"title\":\"a.txt holds the greeting\",\"text\":\"Read a.txt for the greeting text.\",\"paths\":[\"a.txt\"]}]}\n```"

func aiListDirCall(id string) ai.Chunk {
	return ai.Chunk{ToolCalls: []ai.ToolCall{{ID: id, Name: "list_dir", Args: map[string]any{"path": "."}}}}
}

// aiMemoryModel is a chat model whose quick slot is quick (nil: no quick
// slot), in a temp workspace with memory in state.
func aiMemoryModel(t *testing.T, main, quick ai.Provider, state string) (*Model, string) {
	t.Helper()
	t.Cleanup(clock.Set(func() time.Time { return time.Date(2026, 10, 25, 9, 5, 0, 0, time.UTC) }))
	m, dir := aiIDETestModel(t, main)
	if quick != nil {
		m.ai.slotFake = map[string]aiSlot{ai.SlotQuick: {p: quick, cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}}}
	}
	setAIMemory(state)
	m.output = newOutputStore()
	return m, dir
}

// driveAIChatMemory drives a turn like driveAIChat (tool cards denied)
// and returns the command onAIChatDone gave back: the memory extractor
// rides on it.
func driveAIChatMemory(t *testing.T, m *Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	for i := 0; cmd != nil && i < 200; i++ {
		msg := nextAIMsg(t, cmd)
		if msg == nil {
			return nil
		}
		if _, ok := msg.(aiChatConfirmMsg); ok {
			m.handleAIMsg(msg)
			m.answerAIToolConfirm("deny")
			m.confirmOpen = false
			cmd = waitAIChat(m.ai.chatCh)
			continue
		}
		next, ok := m.handleAIMsg(msg)
		if !ok {
			t.Fatalf("unrouted msg %T", msg)
		}
		if _, done := msg.(aiChatDoneMsg); done {
			return next
		}
		cmd = waitAIChat(m.ai.chatCh)
	}
	return nil
}

// aiMemoryMsgFrom runs cmd (and the commands of a batch) and returns the
// memory extractor's message; nil when none comes within wait.
func aiMemoryMsgFrom(cmd tea.Cmd, wait time.Duration) *aiMemoryDoneMsg {
	if cmd == nil {
		return nil
	}
	got := make(chan aiMemoryDoneMsg, 8)
	var run func(c tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			switch msg := c().(type) {
			case aiMemoryDoneMsg:
				got <- msg
			case tea.BatchMsg:
				for _, sub := range msg {
					run(sub)
				}
			}
		}()
	}
	run(cmd)
	select {
	case msg := <-got:
		return &msg
	case <-time.After(wait):
		return nil
	}
}

func aiQuickReq(t *testing.T, p *recordingAIProvider) ai.Request {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.reqs) != 1 {
		t.Fatalf("quick slot requests %d, want 1", len(p.reqs))
	}
	return p.reqs[0]
}

// Spec: after a turn that used a tool, the quick slot gets the turn (no
// tools) and its lesson lands in lessons.md with today's date; the
// answer shows "memory: 1 lesson saved"; AI: Usage has an agent
// "memory"; the audit channel names the count, never the lesson.
func TestAIMemoryWritesAfterToolTurn(t *testing.T) {
	main := &recordingAIProvider{replies: []ai.Chunk{aiListDirCall("c1"), {Delta: "There is a.txt."}}}
	quick := &recordingAIProvider{replies: []ai.Chunk{{Delta: aiMemoryReplyOne, Usage: &ai.Usage{Model: "gpt-5-mini", Input: 500, Output: 40, Requests: 1}}}}
	m, dir := aiMemoryModel(t, main, quick, aiMemoryOn)
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi\n"), 0o644)
	msg := aiMemoryMsgFrom(driveAIChatMemory(t, m, m.sendAIChat("what files are here?")), 5*time.Second)
	if msg == nil {
		t.Fatal("no memory extraction after a tool turn")
	}
	req := aiQuickReq(t, quick)
	if len(req.Tools) != 0 || !strings.Contains(req.Messages[0].Content, "lessons") ||
		!strings.Contains(lastUserContent(req), "what files are here?") || !strings.Contains(lastUserContent(req), "There is a.txt.") {
		t.Errorf("quick request: tools %d\n%+v", len(req.Tools), req.Messages)
	}
	m.handleAIMsg(*msg)
	ls, err := aimemory.Load(dir)
	if err != nil || len(ls) != 1 || ls[0].Title != "a.txt holds the greeting" || ls[0].Date != "2026-10-25" || len(ls[0].Paths) != 1 {
		t.Fatalf("lessons.md: %+v %v", ls, err)
	}
	if got := aiTranscriptText(m, 100); !strings.Contains(got, "memory: 1 lesson saved") {
		t.Errorf("transcript lacks the saved line:\n%s", got)
	}
	found := false
	for _, e := range m.ai.usageLog {
		found = found || (e.Agent == "memory" && e.Model == "gpt-5-mini" && e.U.Input == 500)
	}
	if !found {
		t.Errorf("usage log %+v", m.ai.usageLog)
	}
	audit := strings.Join(m.output.Lines(aiAuditChannel), "\n")
	if !strings.Contains(audit, "memory · 1 lesson saved") || strings.Contains(audit, "greeting") {
		t.Errorf("audit:\n%s", audit)
	}
	saved, err := m.ai.store.Load(m.ai.conv.ID)
	if err != nil || saved.Messages[len(saved.Messages)-1].Memory != 1 {
		t.Errorf("chat file does not keep the note: %v", err)
	}
}

// Spec: a turn that failed (no tool call) also qualifies, and the saved
// lesson goes into the next turn's prompt.
func TestAIMemoryFailedTurnLessonInNextPrompt(t *testing.T) {
	quick := &recordingAIProvider{replies: []ai.Chunk{{Delta: "```json\n{\"lessons\":[{\"title\":\"Build server flaky\",\"text\":\"HTTP 500 from the build server: retry later.\"}]}\n```"}}}
	m, dir := aiMemoryModel(t, memFailAIProvider{}, quick, aiMemoryOn)
	msg := aiMemoryMsgFrom(driveAIChatMemory(t, m, m.sendAIChat("build it")), 5*time.Second)
	if msg == nil {
		t.Fatal("no memory extraction after a failed turn")
	}
	if !strings.Contains(lastUserContent(aiQuickReq(t, quick)), "HTTP 500") {
		t.Error("the error is not in the quick request")
	}
	m.handleAIMsg(*msg)
	if ls, _ := aimemory.Load(dir); len(ls) != 1 {
		t.Fatalf("lessons %+v", ls)
	}
	rec := &recordingAIProvider{}
	m.ai.provider = rec
	driveAIChat(t, m, m.sendAIChat("try the build again"), nil)
	if sys := rec.reqs[0].Messages[0].Content; !strings.Contains(sys, "Build server flaky: HTTP 500 from the build server: retry later.") {
		t.Errorf("next prompt lacks the lesson:\n%s", sys)
	}
}

// Spec: a plain question-and-answer turn costs nothing extra: the quick
// slot is not called. Memory off: no extraction either.
func TestAIMemorySkipsPlainTurns(t *testing.T) {
	quick := &recordingAIProvider{replies: []ai.Chunk{{Delta: aiMemoryReplyOne}}}
	m, _ := aiMemoryModel(t, &recordingAIProvider{}, quick, aiMemoryOn)
	if msg := aiMemoryMsgFrom(driveAIChatMemory(t, m, m.sendAIChat("hello")), 300*time.Millisecond); msg != nil {
		t.Fatal("extraction after a plain turn")
	}
	quick2 := &recordingAIProvider{replies: []ai.Chunk{{Delta: aiMemoryReplyOne}}}
	main := &recordingAIProvider{replies: []ai.Chunk{aiListDirCall("c1"), {Delta: "ok"}}}
	m2, dir2 := aiMemoryModel(t, main, quick2, aiMemoryOff)
	if msg := aiMemoryMsgFrom(driveAIChatMemory(t, m2, m2.sendAIChat("list")), 300*time.Millisecond); msg != nil {
		t.Fatal("extraction while memory is off")
	}
	if len(quick.reqs) != 0 || len(quick2.reqs) != 0 {
		t.Fatalf("quick requests %d / %d", len(quick.reqs), len(quick2.reqs))
	}
	if _, err := os.Stat(aimemory.Dir(dir2)); err == nil {
		t.Error("memory folder made while off")
	}
}

// Spec: a planted secret never reaches the quick model nor lessons.md:
// the turn is redacted on the way in, a lesson quoting a secret is
// dropped on the way out.
func TestAIMemoryRedacts(t *testing.T) {
	ai.RegisterSecret("om2-planted-secret-77")
	main := &recordingAIProvider{replies: []ai.Chunk{aiListDirCall("c1"), {Delta: "The DB password is om2-planted-secret-77."}}}
	reply := "```json\n{\"lessons\":[" +
		"{\"title\":\"DB password\",\"text\":\"The password is om2-planted-secret-77.\"}," +
		"{\"title\":\"API key\",\"text\":\"Use sk-proj-ABCDEFGHIJKLMNOP1234 in tests.\"}," +
		"{\"title\":\"Safe lesson\",\"text\":\"The DB runs in compose.\"}]}\n```"
	quick := &recordingAIProvider{replies: []ai.Chunk{{Delta: reply}}}
	m, dir := aiMemoryModel(t, main, quick, aiMemoryOn)
	msg := aiMemoryMsgFrom(driveAIChatMemory(t, m, m.sendAIChat("what is the db password?")), 5*time.Second)
	if msg == nil {
		t.Fatal("no extraction")
	}
	in := lastUserContent(aiQuickReq(t, quick))
	if strings.Contains(in, "om2-planted-secret-77") || !strings.Contains(in, "[REDACTED]") {
		t.Errorf("quick request not redacted:\n%s", in)
	}
	m.handleAIMsg(*msg)
	b, _ := os.ReadFile(aimemory.Path(dir))
	if strings.Contains(string(b), "om2-planted-secret-77") || strings.Contains(string(b), "sk-proj-") || strings.Contains(string(b), "REDACTED") {
		t.Errorf("lessons.md holds a secret:\n%s", b)
	}
	if ls := aimemory.Parse(b); len(ls) != 1 || ls[0].Title != "Safe lesson" {
		t.Errorf("lessons %+v", ls)
	}
	if got := aiTranscriptText(m, 100); !strings.Contains(got, "memory: 1 lesson saved") {
		t.Errorf("transcript:\n%s", got)
	}
}

// Spec: no quick model slot: nothing breaks, one toast per session says
// lessons are not saved.
func TestAIMemoryQuickSlotMissing(t *testing.T) {
	main := &recordingAIProvider{replies: []ai.Chunk{aiListDirCall("c1"), {Delta: "one"}, aiListDirCall("c2"), {Delta: "two"}}}
	m, dir := aiMemoryModel(t, main, nil, aiMemoryOn)
	for _, text := range []string{"first", "second"} {
		if msg := aiMemoryMsgFrom(driveAIChatMemory(t, m, m.sendAIChat(text)), 300*time.Millisecond); msg != nil {
			t.Fatal("extraction without a quick slot")
		}
	}
	if n := strings.Count(aiToastText(m), "quick model"); n != 1 {
		t.Errorf("%d quick-slot toasts:\n%s", n, aiToastText(m))
	}
	if _, err := os.Stat(aimemory.Path(dir)); err == nil {
		t.Error("lessons.md written")
	}
}

// Spec: the extractor runs beside the chat: a slow quick model never
// holds the next message, and after its 30 s timeout (short here) it
// writes nothing.
func TestAIMemoryTimeoutDoesNotBlock(t *testing.T) {
	old := aiMemoryTimeout
	aiMemoryTimeout = 100 * time.Millisecond
	t.Cleanup(func() { aiMemoryTimeout = old })
	main := &recordingAIProvider{replies: []ai.Chunk{aiListDirCall("c1"), {Delta: "one"}, {Delta: "two"}}}
	m, dir := aiMemoryModel(t, main, waitAIProvider{}, aiMemoryOn)
	cmd := driveAIChatMemory(t, m, m.sendAIChat("first"))
	if m.ai.streaming {
		t.Fatal("the chat still streams while the extractor runs")
	}
	driveAIChat(t, m, m.sendAIChat("second"), nil) // not held by the extractor
	if len(main.reqs) != 3 {
		t.Fatalf("main requests %d", len(main.reqs))
	}
	msg := aiMemoryMsgFrom(cmd, 5*time.Second)
	if msg == nil || msg.err == nil {
		t.Fatalf("timeout msg %+v", msg)
	}
	m.handleAIMsg(*msg)
	if _, err := os.Stat(aimemory.Path(dir)); err == nil {
		t.Error("lessons.md written after a timeout")
	}
	if got := aiTranscriptText(m, 100); strings.Contains(got, "lesson saved") {
		t.Errorf("saved line after a timeout:\n%s", got)
	}
}
