package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Spec: the chat header has a "usage" chip after the profile and MCP
// chips; a click opens the session-wide AI: Usage tables.
func TestAIHeaderUsageChipOpensUsage(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	m.w, m.h = 200, 40
	w := m.actionsColumnWidth()
	head := ansi.Strip(m.renderActionsHeader(w))
	i := strings.Index(head, "usage")
	if i < 0 {
		t.Fatalf("no usage chip in header %q", head)
	}
	x := m.w - w + ansi.StringWidth(head[:i]) + 1
	if id := m.hitActionsButton(x, 0); id != "usage" {
		t.Fatalf("hit at chip = %q", id)
	}
	if id := m.aiPanelTargetAt(x, 0); id != "ai:usage" {
		t.Fatalf("target at chip = %q", id)
	}
	m.handleAIPanelMouse(tea.MouseMsg{X: x, Y: 0, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !m.agentsShowing() || !m.ai.agents.usage {
		t.Fatal("a click on the chip did not open the usage view")
	}
}

// Spec: chips drop from the right when the panel is narrow (usage
// first); the header never exceeds the panel width.
func TestAIHeaderUsageChipNarrow(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	for w := 20; w <= 80; w++ {
		head := ansi.Strip(m.renderActionsHeader(w))
		if got := ansi.StringWidth(head); got != w {
			t.Fatalf("width %d: header is %d cells: %q", w, got, head)
		}
		if strings.Contains(head, "usage") && !strings.Contains(head, "default") {
			t.Fatalf("width %d: usage chip without the profile chip: %q", w, head)
		}
		chips := m.actionsHeaderChips(w)
		end := 0
		for _, c := range chips {
			if c.x0 < end {
				t.Fatalf("width %d: chips overlap: %v", w, chips)
			}
			end = c.x0 + ansi.StringWidth(c.label)
		}
		if len(chips) > 0 && end+1+actionsHeaderButtonsW > w {
			t.Fatalf("width %d: chips run into the buttons: %v", w, chips)
		}
	}
	if head := ansi.Strip(m.renderActionsHeader(36)); strings.Contains(head, "usage") {
		t.Errorf("usage chip should drop first at 36: %q", head)
	}
}
