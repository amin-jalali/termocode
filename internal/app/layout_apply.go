package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/layout"
)

// openLayout makes the IDE look like l. Parts the layout does not mention
// stay as they are. Files that do not exist are skipped, and one toast says
// how many. The result is saved like any other layout change, so the next
// start comes back the same way.
func (m *Model) openLayout(l layout.Layout) tea.Cmd {
	var cmds []tea.Cmd
	if m.zenMode { // zen hides every part; a layout shows them
		cmds = append(cmds, m.toggleZenMode())
	}
	if s := l.Sidebar; s != nil {
		if s.Width > 0 {
			m.explorerWidth = clampExplorerWidth(m, s.Width)
			m.persistExplorerWidth()
		}
		if v, ok := layoutViewFromName(s.View); ok && s.Open {
			cmds = append(cmds, m.showSidebarView(v))
		} else if ok {
			m.activity.SetActive(v)
		}
		m.showExp = s.Open
	}
	if c := l.Chat; c != nil {
		if c.Width > 0 {
			m.actionsWidth, m.actionsWidthSet = c.Width, true
		}
		switch {
		case c.Open && !m.actionsOpen:
			m.toggleActionsPanel()
		case c.Open && !m.actionsPinned:
			m.setActionsPinned(true)
		case !c.Open && m.actionsOpen:
			m.closeActionsPanel()
		default:
			m.persistActionsState()
		}
	}
	m.applyLayout()
	switch l.Focus {
	case "editor":
		m.focus = FocusEditor
	case "sidebar":
		if m.showExp {
			m.focus = FocusExplorer
		}
	}
	m.applyLayout()
	m.resizeTerminalSplit()
	return tea.Batch(cmds...)
}

// clampExplorerWidth keeps a layout's sidebar width inside the limits of
// dragging the sidebar edge.
func clampExplorerWidth(m *Model, w int) int {
	w = max(w, explorerMinWidth)
	w = min(w, explorerMaxWidth)
	if room := m.w - activity.Width - editorScrollbarWidth - 10; m.w > 0 && room > 0 && w > room {
		w = room
	}
	return w
}
