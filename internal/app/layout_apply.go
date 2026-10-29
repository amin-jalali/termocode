package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/layout"
	"github.com/amin-jalali/termocode/internal/toast"
)

// layoutHintFor is how long the status bar shows the name of a layout after
// it was opened.
const layoutHintFor = 2 * time.Second

// layoutHintClearMsg ends the hint of generation gen.
type layoutHintClearMsg struct{ gen int }

// showLayoutHint shows name in the status bar for layoutHintFor.
func (m *Model) showLayoutHint(name string) tea.Cmd {
	m.layoutHintGen++
	m.layoutHint = name
	gen := m.layoutHintGen
	return tea.Tick(layoutHintFor, func(time.Time) tea.Msg { return layoutHintClearMsg{gen: gen} })
}

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
	missing := 0
	if e := l.Editor; e != nil {
		missing = m.openLayoutEditor(*e, root)
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
	if missing > 0 {
		var t tea.Cmd
		m.toast, t = m.toast.Push(toast.Warn, fmt.Sprintf("%d file%s not found", missing, plural(missing)))
		cmds = append(cmds, t)
	}
	if l.Name != "" {
		cmds = append(cmds, m.showLayoutHint(l.Name))
	}
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
			break
		}
	}
	m.openLayoutPanes(ts, root)
}

// openLayoutPanes gives each terminal tab that has a single shell the split
// panes its layout entry lists. Tabs that are already split stay as they are.
func (m *Model) openLayoutPanes(ts []layout.Terminal, root string) {
	active := m.terminalActiveTab
	for i, t := range ts {
		if i >= len(m.terminalTabs) || len(t.Panes) == 0 || m.terminalTabs[i].paneCount() > 1 {
			continue
		}
		m.switchTerminalTab(i)
		specs := make([]paneSpec, 0, len(t.Panes))
		for _, p := range t.Panes {
			dir := paneRight
			if p.Split == "down" {
				dir = paneDown
			}
			specs = append(specs, paneSpec{Dir: dir, Parent: p.Parent, Cwd: layoutDir(root, p.Cwd)})
		}
		m.buildTerminalPanes(specs)
	}
	if active != m.terminalActiveTab && active < len(m.terminalTabs) {
		m.switchTerminalTab(active)
	}
}

// openLayoutEditor opens the files and the splits. It returns how many
// files do not exist.
func (m *Model) openLayoutEditor(e layout.Editor, root string) (missing int) {
	if m.nvim == nil {
		return 0
	}
	exists := func(rel string) (string, bool) {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		st, err := os.Stat(abs)
		return abs, err == nil && !st.IsDir()
	}
	m.ensureEditorWindowCurrent()
	for _, rel := range e.Files {
		if abs, ok := exists(rel); ok {
			_ = m.nvim.OpenFile("edit", abs)
		} else {
			missing++
		}
	}
	m.closeExtraEditorWindows() // the layout decides the windows: one, or one per pane
	if e.Split == "" {
		if abs, ok := exists(e.Active); ok {
			_ = m.nvim.OpenFile("edit", abs)
		} else if e.Active != "" {
			missing++
		}
		return missing
	}
	// A split: one editor window per pane, the first in the current one.
	activeAbs := ""
	if abs, ok := exists(e.Active); ok && e.Active != "" {
		activeAbs = abs
	}
	cmd := map[string]string{"vertical": "rightbelow vsplit", "horizontal": "rightbelow split"}[e.Split]
	first := true
	for _, rel := range e.Panes {
		abs, ok := exists(rel)
		if !ok {
			if rel != "" {
				missing++
			}
			continue
		}
		if first {
			_ = m.nvim.OpenFile("edit", abs)
			first = false
			continue
		}
		_ = m.nvim.OpenFile(cmd, abs)
	}
	if first { // no pane file could open: still make the split
		_ = m.nvim.Command(cmd)
	}
	// The window that shows the active file has the keyboard (else the first).
	_ = m.nvim.ExecLuaArgs(`local want = ...
		local first, hit
		for _, w in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
			local b = vim.api.nvim_win_get_buf(w)
			if vim.api.nvim_win_get_config(w).relative == '' and not vim.w[w].termocode_panel
				and vim.bo[b].buftype == '' then
				first = first or w
				if want ~= '' and vim.api.nvim_buf_get_name(b) == want then hit = hit or w end
			end
		end
		local w = hit or first
		if w then pcall(vim.api.nvim_set_current_win, w) end`, activeAbs)
	return missing
}

// closeExtraEditorWindows closes every editor window but one; the panel's
// window and floating windows stay.
func (m *Model) closeExtraEditorWindows() {
	_ = m.nvim.ExecLua(`
		local keep
		for _, w in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
			local b = vim.api.nvim_win_get_buf(w)
			if vim.api.nvim_win_get_config(w).relative == '' and not vim.w[w].termocode_panel
				and vim.bo[b].buftype ~= 'terminal' then
				if keep then pcall(vim.api.nvim_win_close, w, false) else keep = w end
			end
		end
		if keep then pcall(vim.api.nvim_set_current_win, keep) end`)
}

// resetLayout is Layout: Reset to Default: the built-in default layout, and
// the sizes a first start has (sidebar width, panel height, chat width).
// Open files stay open.
func (m *Model) resetLayout() tea.Cmd {
	var def layout.Layout
	for _, e := range layout.Builtins() {
		if e.Name == "default" {
			def = e.Layout
		}
	}
	cmd := m.openLayout(def)
	m.explorerWidth = defaultExplorerWidth
	m.terminalRows = terminalRowsDefault
	m.actionsWidth, m.actionsWidthSet = defaultActionsWidth, false
	m.persistExplorerWidth()
	m.persistTerminalRows()
	m.persistActionsState()
	m.applyLayout()
	m.resizeTerminalSplit()
	return cmd
}
