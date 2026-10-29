package app

import (
	"os"
	"path/filepath"

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
	root, _ := m.rootDir()

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
	if p := l.Panel; p != nil {
		m.openLayoutPanel(*p, root)
	}
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

// layoutDir is the folder of a layout's relative path, "" if it is not a
// folder.
func layoutDir(root, rel string) string {
	if root == "" || rel == "" {
		return ""
	}
	p := filepath.Join(root, filepath.FromSlash(rel))
	if st, err := os.Stat(p); err != nil || !st.IsDir() {
		return ""
	}
	return p
}

// openLayoutPanel opens or closes the bottom panel and its tabs.
func (m *Model) openLayoutPanel(p layout.Panel, root string) {
	if !p.Open {
		if m.termOpen {
			m.hideTerminalPanel()
		}
		return
	}
	if m.nvim == nil {
		return
	}
	if p.Rows > 0 {
		m.terminalRows = max(p.Rows, terminalRowsMin)
		if hi := m.h - 8; hi >= terminalRowsMin && m.terminalRows > hi {
			m.terminalRows = hi
		}
		m.terminalMinimized = false
		m.persistTerminalRows()
	}
	if n := len(p.Terminals); n > 0 {
		m.openLayoutTerminals(p.Terminals, root)
	} else if !m.termOpen {
		m.showPanelTab(panelKindTerminal)
	}
	for _, name := range p.Tabs {
		if k, ok := layoutPanelFromName(name); ok && k != panelKindTerminal {
			m.showPanelTab(k)
		}
	}
	if k, ok := layoutPanelFromName(p.Active); ok {
		m.showPanelTab(k)
	}
}

// openLayoutTerminals makes sure the panel has at least len(ts) shells; a
// new one starts in the folder its entry names.
func (m *Model) openLayoutTerminals(ts []layout.Terminal, root string) {
	if !m.termOpen {
		if len(m.terminalTabs) == 0 && len(m.hiddenTerminalTabs) == 0 {
			if !m.ensurePanelHost(true) { // an empty panel: each shell then picks its folder
				return
			}
		} else {
			m.openTerminalPanel()
		}
	}
	for len(m.terminalTabs) < len(ts) {
		before := len(m.terminalTabs)
		m.newTerminalTabIn(layoutDir(root, ts[before].Cwd))
		if len(m.terminalTabs) == before {
			return
		}
	}
}
