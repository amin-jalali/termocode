package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
)

// hoverModel is a 160×45 window on the welcome screen (no file open).
func hoverModel(tb testing.TB) Model {
	tb.Helper()
	m0, _ := newTestModel(tb).Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	m := m0.(Model)
	_ = m.View() // warm the render caches, as the real program does
	return m
}

// openAIPanel opens the pinned AI chat panel on a long conversation.
func openAIPanel(tb testing.TB, m *Model) {
	d := dragTestModel(tb)
	m.actionsOpen, m.actionsPinned = true, true
	m.actionsWidth, m.actionsWidthSet = 40, true
	*m.aiS() = *d.aiS()
	m.applyLayout()
}

func hoverAt(m Model, x, y int) Model {
	nm, _ := m.Update(tea.MouseMsg{X: x, Y: y, Type: tea.MouseMotion, Action: tea.MouseActionMotion})
	return nm.(Model)
}

// BenchmarkHoverUpdate measures one mouse-motion Update (no View) over
// the parts of the screen a pointer moves across most.
func BenchmarkHoverUpdate(b *testing.B) {
	cases := []struct {
		name  string
		setup func(tb testing.TB, m *Model)
		pos   func(m Model) (int, int)
	}{
		{"welcome-card", nil, func(m Model) (int, int) {
			pane := m.editorPaneLayout()
			h := welcomeQuickActionHits(pane.W, pane.H)[1]
			return pane.X + h.ColStart + 1, pane.Top + h.Row
		}},
		{"welcome-empty", nil, func(m Model) (int, int) {
			pane := m.editorPaneLayout()
			return pane.X + 1, pane.Top + 1
		}},
		{"sidebar-files", nil, func(m Model) (int, int) { return activity.Width + 3, 4 }},
		{"sidebar-scm", func(_ testing.TB, m *Model) { m.activity.SetActive(activity.ViewGit) },
			func(m Model) (int, int) { return activity.Width + 3, 6 }},
		{"activity-bar", nil, func(m Model) (int, int) { return 1, 3 }},
		{"tabs", nil, func(m Model) (int, int) { return m.editorPaneLayout().X + 4, m.tabBarRow() }},
		{"status-bar", nil, func(m Model) (int, int) { return m.w - 10, m.h - 1 }},
		{"ai-panel", openAIPanel, func(m Model) (int, int) { return m.w - 10, 20 }},
		{"welcome-card-ai-open", openAIPanel, func(m Model) (int, int) {
			pane := m.editorPaneLayout()
			h := welcomeQuickActionHits(pane.W, pane.H)[1]
			return pane.X + h.ColStart + 1, pane.Top + h.Row
		}},
		{"overflow-menu", func(_ testing.TB, m *Model) { m.openOverflowMenu() }, func(m Model) (int, int) {
			x, y, _, _, _ := m.overflowMenuRect()
			return x + 2, y + 2
		}},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.Setenv("XDG_CONFIG_HOME", b.TempDir())
			m := hoverModel(b)
			if c.setup != nil {
				c.setup(b, &m)
				_ = m.View()
			}
			x, y := c.pos(m)
			m = hoverAt(m, x, y) // warm
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m = hoverAt(m, x, y)
			}
		})
	}
}
