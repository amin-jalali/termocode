package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

// dragTestModel is a 200×50 window with the AI chat panel open on a long
// conversation (20 messages, Markdown + code blocks).
func dragTestModel(tb testing.TB) Model {
	tb.Helper()
	m0, _ := New().Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	m := m0.(Model)
	m.actionsOpen, m.actionsPinned = true, true
	m.actionsWidth = 40
	s := m.aiS()
	s.provider = fakeAIProvider{}
	for i := 0; i < 10; i++ {
		s.conv.Messages = append(s.conv.Messages,
			ai.StoredMessage{Role: ai.StoredUser, Text: fmt.Sprintf("Question %d: why is this slow when I drag the splitter quickly?", i)},
			ai.StoredMessage{Role: ai.StoredAssistant, Text: "## Answer\n\nThe **resize** happens on every `MouseMotion` event, so *nvim* redraws.\n\n" +
				"1. first point with `code`\n2. second point that is long enough to wrap twice in a narrow panel\n\n" +
				"```go\nfunc (m *Model) applyLayout() {\n\tfor i := 0; i < len(m.bufs); i++ {\n\t\tm.editor.SetSize(m.w-m.explorerWidth, m.h)\n\t}\n}\n```\n\n> tip: debounce it"})
	}
	m.applyLayout()
	return m
}

// dragStep runs one splitter motion event (Update + View).
func dragStep(m Model, x int) Model {
	nm, _ := m.Update(tea.MouseMsg{X: x, Y: 10, Type: tea.MouseMotion, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	m = nm.(Model)
	_ = m.View()
	return m
}

func startDrag(m Model) Model {
	edge := m.w - m.actionsColumnWidth()
	nm, _ := m.Update(tea.MouseMsg{X: edge, Y: 10, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	return nm.(Model)
}

// BenchmarkAISplitterDrag measures one drag step (Update + View) while the
// chat panel grows and shrinks over a 200-step drag.
func BenchmarkAISplitterDrag(b *testing.B) {
	b.Setenv("XDG_CONFIG_HOME", b.TempDir())
	m := startDrag(dragTestModel(b))
	edge := m.w - m.actionsColumnWidth()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := i % 200
		if k >= 100 {
			k = 199 - k
		}
		m = dragStep(m, edge-k/2)
	}
}

// TestAISplitterDragIsCheap asserts the per-step cost of a 200-step drag.
func TestAISplitterDragIsCheap(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("timing test")
	}
	withConfigDir(t)
	m := startDrag(dragTestModel(t))
	edge := m.w - m.actionsColumnWidth()
	m = dragStep(m, edge) // warm caches
	start := time.Now()
	const steps = 200
	for i := 0; i < steps; i++ {
		k := i
		if k >= 100 {
			k = 199 - k
		}
		m = dragStep(m, edge-k/2)
	}
	per := time.Since(start) / steps
	t.Logf("AI splitter drag: %v per step (Update + View)", per)
	if per > 4*time.Millisecond {
		t.Errorf("drag step costs %v, want < 4ms", per)
	}
	if !strings.Contains(m.View(), "Answer") {
		t.Error("chat not rendered")
	}
}
