package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// CMP-25: with ai_router on, a short chat turn runs on quick, a hard one
// on main; the chat shows each pick, the model never sees the line.
func TestAIChatAutoModelPick(t *testing.T) {
	m, _ := aiReasoningModel(t)
	main := &recordingAIProvider{replies: []ai.Chunk{{Delta: "deep answer"}}}
	quick := &recordingAIProvider{replies: []ai.Chunk{{Delta: "quick answer"}, {Delta: "quick again"}}}
	m.ai.slotFake = map[string]aiSlot{
		ai.SlotMain:  {p: main, cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-opus-5-5"}},
		ai.SlotQuick: {p: quick, cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}},
	}
	m.ai.settings.Router = ai.Router{Mode: ai.RouterAuto}

	driveAIChat(t, m, m.sendAIChat("what does rebase do?"), nil)
	quick.mu.Lock()
	n := len(quick.reqs)
	quick.mu.Unlock()
	if n != 1 {
		t.Fatalf("quick got %d requests, want 1", n)
	}
	var lines []string
	for _, sm := range m.ai.conv.Messages {
		lines = append(lines, string(sm.Role)+": "+sm.Text)
	}
	all := strings.Join(lines, "\n")
	if !strings.Contains(all, aiRouteMark+"quick (gpt-5-mini) — a short, simple message") || !strings.Contains(all, "quick answer") {
		t.Fatalf("chat:\n%s", all)
	}
	if last := m.ai.conv.Messages[len(m.ai.conv.Messages)-1]; last.Model != "gpt-5-mini" {
		t.Errorf("the answer is not labelled with the picked model: %+v", last)
	}

	driveAIChat(t, m, m.sendAIChat("refactor the store"), nil)
	main.mu.Lock()
	defer main.mu.Unlock()
	if len(main.reqs) != 1 {
		t.Fatalf("main got %d requests, want 1", len(main.reqs))
	}
	for _, msg := range main.reqs[0].Messages {
		if strings.Contains(msg.Content, "Auto model:") {
			t.Errorf("the pick line was sent to the model: %q", msg.Content)
		}
	}
}

// CMP-25: off by default; other profiles keep their slot; a picked slot
// that can not run leaves the turn on main and says why.
func TestAIRouteTurnLimits(t *testing.T) {
	m, _ := aiReasoningModel(t)
	s := m.ai
	def := ai.Profile{Name: ai.ProfileDefault}
	if got, note := s.aiRouteTurn(def, "", "hi"); got != "" || note != "" {
		t.Fatalf("off router: %q %q", got, note)
	}
	s.settings.Router = ai.Router{Mode: ai.RouterAuto, Quick: "nope"}
	s.slotFake = map[string]aiSlot{ai.SlotMain: {p: &recordingAIProvider{}, cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-opus-5-5"}}}
	if got, _ := s.aiRouteTurn(ai.Profile{Name: "review"}, "", "hi"); got != "" {
		t.Fatalf("another profile was routed to %q", got)
	}
	if got, _ := s.aiRouteTurn(def, "gpt", "hi"); got != "gpt" {
		t.Fatalf("a step's own slot was routed to %q", got)
	}
	got, note := s.aiRouteTurn(def, "", "hi")
	if got != "" || !strings.HasPrefix(note, aiRouteMark+"main (claude-opus-5-5) — nope can not run") {
		t.Fatalf("broken quick: %q %q", got, note)
	}
	if !aiForUserOnly(note) {
		t.Fatal("the pick line is not for the user only")
	}
}

// CMP-25: turning it on waits for a card whose first button is Cancel;
// Cancel changes nothing, Turn On saves auto, the command again saves off.
func TestAIRouterToggleCard(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m, _ := aiReasoningModel(t)
	m.ai.slotFake = map[string]aiSlot{
		ai.SlotMain:  {p: &recordingAIProvider{}, cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-opus-5-5"}},
		ai.SlotQuick: {p: &recordingAIProvider{}, cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}},
	}
	m.toggleAIRouter()
	if !m.confirmOpen || m.confirmKind != confirmKindAIRouterOn || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no Cancel-first card: open=%v kind=%v safe=%q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	m.answerAIRouterOn("cancel")
	if m.ai.settings.Router.On() {
		t.Fatal("Cancel turned it on")
	}
	m.answerAIRouterOn("on")
	path, _ := ai.ConfigPath()
	if !m.ai.settings.Router.On() || !ai.LoadSettings(path).Router.On() {
		t.Fatal("Turn On did not save mode auto")
	}
	m.confirmOpen = false
	m.toggleAIRouter()
	if m.confirmOpen || m.ai.settings.Router.On() || ai.LoadSettings(path).Router.On() {
		t.Fatal("the command did not turn it off at once")
	}
}

// CMP-25: the palette command exists, the AI may never run it, and the
// web offers it only behind a laptop card.
func TestAIAutoModelCommandPolicy(t *testing.T) {
	found := false
	for _, it := range paletteItems() {
		found = found || it.ID == "ai-auto-model"
	}
	if !found {
		t.Fatal("ai-auto-model is not in the palette")
	}
	p, ok := aiActionPolicyFor("ai-auto-model")
	if !ok || p.Gate != aiGateNever {
		t.Fatalf("policy %+v %v", p, ok)
	}
	if card, ok := webPaletteAllow["ai-auto-model"]; !ok || !card {
		t.Fatal("the web does not ask the laptop first")
	}
}
