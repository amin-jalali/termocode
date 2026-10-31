package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
)

// OMX-5: the reasoning level of the chat.

func aiReasoningModel(t *testing.T) (*Model, *recordingAIProvider) {
	t.Helper()
	p := &recordingAIProvider{}
	m, _ := aiIDETestModel(t, p)
	return m, p
}

func lastReasoning(t *testing.T, p *recordingAIProvider) string {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.reqs) == 0 {
		t.Fatal("no request")
	}
	return p.reqs[len(p.reqs)-1].Reasoning
}

// "ultrathink" in the message shows a chip before sending and raises the
// level of that turn only.
func TestAIReasoningKeywordChipRaisesOneTurn(t *testing.T) {
	m, p := aiReasoningModel(t)
	m.ai.input = []rune("ultrathink: why does the test fail?")
	row := ansi.Strip(m.aiDividerLine(60).styled)
	if !strings.Contains(row, "ultrathink · max ×") {
		t.Fatalf("no keyword chip: %q", row)
	}
	driveAIChat(t, m, m.sendAIChat(string(m.ai.input)), nil)
	if got := lastReasoning(t, p); got != ai.ReasoningMax {
		t.Errorf("turn level %q, want max", got)
	}
	driveAIChat(t, m, m.sendAIChat("and now?"), nil)
	if got := lastReasoning(t, p); got != "" {
		t.Errorf("the next turn keeps the level: %q", got)
	}
}

// The chip's × removes the keyword's effect for this message (the text
// stays as typed).
func TestAIReasoningKeywordChipRemoved(t *testing.T) {
	m, p := aiReasoningModel(t)
	m.ai.input = []rune("think about it")
	l := m.aiDividerLine(60)
	id := ""
	for _, b := range l.buttons {
		if strings.HasPrefix(b.id, "rm-think") {
			id = b.id
		}
	}
	if id == "" {
		t.Fatalf("no remove button: %+v", l.buttons)
	}
	m.onAIButton(id)
	if row := ansi.Strip(m.aiDividerLine(60).styled); strings.Contains(row, "think ·") {
		t.Fatalf("chip still shown: %q", row)
	}
	driveAIChat(t, m, m.sendAIChat("think about it"), nil)
	if got := lastReasoning(t, p); got != "" {
		t.Errorf("removed keyword still raised the level: %q", got)
	}
	m.ai.input = []rune("think again")
	if row := ansi.Strip(m.aiDividerLine(60).styled); !strings.Contains(row, "think · high") {
		t.Errorf("the next message shows the chip again: %q", row)
	}
}

// AI: Next Reasoning Level cycles the chat's level; the header chip
// shows it and a click on it cycles too; ai_reasoning is the start.
func TestAIReasoningCycleAndHeaderChip(t *testing.T) {
	m, p := aiReasoningModel(t)
	if l := m.aiReasoningChipLabel(); l != "" {
		t.Fatalf("no chip without a level: %q", l)
	}
	m.ai.settings.Reasoning = "medium"
	if l := m.aiReasoningChipLabel(); !strings.Contains(l, "medium") {
		t.Fatalf("chip %q", l)
	}
	m.cycleAIReasoning()
	if l := m.aiReasoningChipLabel(); !strings.Contains(l, "high") {
		t.Fatalf("after a cycle: %q", l)
	}
	driveAIChat(t, m, m.sendAIChat("q"), nil)
	if got := lastReasoning(t, p); got != ai.ReasoningHigh {
		t.Errorf("turn level %q", got)
	}
	for i := 0; i < 3; i++ { // xhigh, max, auto
		m.cycleAIReasoning()
	}
	if l := m.aiReasoningChipLabel(); !strings.Contains(l, "auto") {
		t.Errorf("chosen default keeps the chip: %q", l)
	}
	m.actionsOpen, m.actionsPinned = true, true
	found := false
	for _, c := range m.actionsHeaderChips(m.actionsColumnWidth()) {
		if c.id == "think" {
			found = true
		}
	}
	if !found {
		t.Error("header has no reasoning chip")
	}
	if id := m.aiPanelTargetAtChip(t, "think"); id != "ai:think" {
		t.Errorf("chip click target %q", id)
	}
}

// aiPanelTargetAtChip is the click target of header chip id.
func (m *Model) aiPanelTargetAtChip(t *testing.T, id string) string {
	t.Helper()
	x0 := m.w - m.actionsColumnWidth()
	for _, c := range m.actionsHeaderChips(m.actionsColumnWidth()) {
		if c.id == id {
			return m.aiPanelTargetAt(x0+c.x0, 0)
		}
	}
	t.Fatalf("no chip %q", id)
	return ""
}
