package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/theme"
)

// ── Bottom panel framework ───────────────────────────────────────────────
//
// The bottom panel is ONE nvim split (m.terminalWinID) whose top row is
// overpainted with a Go tab bar (shell.go). Every tab has a kind:
//
//   - panelKindTerminal — a real nvim terminal buffer shown in the split
//     (terminalTabs; there can be many).
//   - every other kind (Output, Problems, Tests, Debug Console) — at most
//     one tab each, listed in m.panelTabs and rendered by Go over the
//     split's rows. nvim keeps whatever buffer is in the window (the
//     active terminal, or the placeholder scratch buffer when there is no
//     terminal tab) — it just never shows through.
//
// Bar order: non-terminal tabs first (by kind), then terminal tabs, then
// "+" (new terminal). Ctrl+Shift+PgUp/PgDn cycles the whole list.
//
// Adding a tab kind from another feature group:
//
//  1. add a panelKind constant below (or reuse a reserved one),
//  2. register its spec from an init() in your own file:
//
//     func init() {
//     	registerPanelKind(panelKindProblems, panelKindSpec{
//     		Title:  "Problems",
//     		Icon:   "⚠",
//     		Render: func(m *Model, w, h int) []string { … },
//     		Scroll: func(m *Model, delta int) { … },          // optional
//     		Click:  func(m *Model, row, col int) tea.Cmd { … }, // optional
//     		Key:    func(m *Model, k tea.KeyMsg) (tea.Cmd, bool) { … }, // optional
//     	})
//     }
//
//  3. open it with m.showPanelTab(panelKindProblems) (palette / keymap),
//     close it with m.closePanelTab(kind).
//
// Keep per-kind state behind a pointer on Model (like m.output) so copies
// of the value-type Model share it.

// panelKind identifies what a bottom-panel tab shows.
type panelKind int

const (
	// panelKindTerminal is the zero value so an uninitialised Model keeps
	// the classic terminal-only behaviour.
	panelKindTerminal panelKind = iota
	panelKindProblems
	panelKindOutput
	panelKindTests
	panelKindDebugConsole
)

// panelKindSpec describes how a non-terminal tab kind renders and reacts.
// All callbacks receive panel-content-local coordinates: row 0 is the
// first row BELOW the tab bar.
type panelKindSpec struct {
	// Title is the tab label and the bar heading while active.
	Title string
	// Icon is a 1-cell glyph drawn before the title.
	Icon string
	// Render returns the content rows for a w×h area. Rows may be shorter
	// or longer than w (they are padded / clipped) and fewer than h (the
	// rest is blank). Called from View — it must not block.
	Render func(m *Model, w, h int) []string
	// Scroll handles the mouse wheel (delta < 0 = up). Optional.
	Scroll func(m *Model, delta int)
	// Click handles a left click inside the content area. Optional.
	Click func(m *Model, row, col int) tea.Cmd
	// Key handles a key while the tab has keyboard focus (after a click
	// in its content). Return false to let the global keymap run. Esc is
	// handled by the framework (drops focus). Optional.
	Key func(m *Model, msg tea.KeyMsg) (tea.Cmd, bool)
	// OnClose runs when the tab is closed (× or closePanelTab). Optional.
	OnClose func(m *Model)
	// TitleFn, when set, overrides Title with a live label (e.g.
	// "PROBLEMS (3)"). Called from View — it must not block. Optional.
	TitleFn func(m *Model) string
}

// panelKinds holds the registered specs. Written only from init().
var panelKinds = map[panelKind]panelKindSpec{}

// registerPanelKind registers a non-terminal tab kind. Call it from an
// init() function. Panics on panelKindTerminal, a missing Render or a
// duplicate — all programming errors.
func registerPanelKind(k panelKind, spec panelKindSpec) {
	if k == panelKindTerminal {
		panic("registerPanelKind: terminal tabs are built in")
	}
	if spec.Render == nil {
		panic(fmt.Sprintf("registerPanelKind: kind %d has no Render", k))
	}
	if _, dup := panelKinds[k]; dup {
		panic(fmt.Sprintf("registerPanelKind: kind %d registered twice", k))
	}
	panelKinds[k] = spec
}

// panelKindTitle returns the display title for a kind.
func panelKindTitle(k panelKind) string {
	if k == panelKindTerminal {
		return "Terminal"
	}
	if s, ok := panelKinds[k]; ok && s.Title != "" {
		return s.Title
	}
	return "Panel"
}

// panelTitle is panelKindTitle plus the kind's live TitleFn, if any.
func (m Model) panelTitle(k panelKind) string {
	if s, ok := panelKinds[k]; ok && s.TitleFn != nil {
		if t := s.TitleFn(&m); t != "" {
			return t
		}
	}
	return panelKindTitle(k)
}

// panelKindIcon returns the 1-cell glyph for a non-terminal kind.
func panelKindIcon(k panelKind) string {
	if s, ok := panelKinds[k]; ok && s.Icon != "" {
		return s.Icon
	}
	return "≡"
}

// panelBarEntry is one tab in bar order. Index points into m.panelTabs for
// non-terminal kinds and into m.terminalTabs for terminals.
type panelBarEntry struct {
	Kind  panelKind
	Index int
}

// panelBarEntries returns every tab in bar order: non-terminal tabs, then
// terminal tabs.
func (m Model) panelBarEntries() []panelBarEntry {
	out := make([]panelBarEntry, 0, len(m.panelTabs)+len(m.terminalTabs))
	for i, k := range m.panelTabs {
		out = append(out, panelBarEntry{Kind: k, Index: i})
	}
	for i := range m.terminalTabs {
		out = append(out, panelBarEntry{Kind: panelKindTerminal, Index: i})
	}
	return out
}

// panelEntryActive reports whether e is the visible tab.
func (m Model) panelEntryActive(e panelBarEntry) bool {
	if e.Kind != m.panelActive {
		return false
	}
	if e.Kind == panelKindTerminal {
		return e.Index == m.terminalActiveTab
	}
	return true
}

// hasPanelTab reports whether a non-terminal tab of kind k is open.
func (m Model) hasPanelTab(k panelKind) bool {
	for _, t := range m.panelTabs {
		if t == k {
			return true
		}
	}
	return false
}

// insertPanelTab adds k to tabs keeping kind order (Problems, Output,
// Tests, Debug Console). No-op when already present. Pure for tests.
func insertPanelTab(tabs []panelKind, k panelKind) []panelKind {
	for i, t := range tabs {
		if t == k {
			return tabs
		}
		if t > k {
			out := make([]panelKind, 0, len(tabs)+1)
			out = append(out, tabs[:i]...)
			out = append(out, k)
			return append(out, tabs[i:]...)
		}
	}
	return append(tabs, k)
}

// cycleIndex moves cur by step inside [0, n), wrapping. Pure for tests.
func cycleIndex(cur, step, n int) int {
	if n <= 0 {
		return 0
	}
	idx := (cur + step) % n
	if idx < 0 {
		idx += n
	}
	return idx
}

// showPanelTab opens the bottom panel (if needed) and makes a tab of kind
// k visible — the one entry point features use ("View: Output", "View:
// Problems", …). For panelKindTerminal it focuses the active terminal tab
// or spawns one.
func (m *Model) showPanelTab(k panelKind) {
	if m.nvim == nil {
		return
	}
	if k == panelKindTerminal {
		if m.termOpen && len(m.terminalTabs) > 0 {
			m.switchTerminalTab(m.terminalActiveTab)
		} else {
			m.newTerminalTab()
		}
		m.terminalMinimized = false
		m.applyLayout()
		m.resizeTerminalSplit()
		return
	}
	if _, ok := panelKinds[k]; !ok {
		return
	}
	if !m.termOpen {
		if !m.ensurePanelHost(true) {
			return
		}
	}
	m.panelTabs = insertPanelTab(m.panelTabs, k)
	m.activateNonTerminalTab(k)
	m.terminalMinimized = false
	m.applyLayout()
	m.resizeTerminalSplit()
}

// activateNonTerminalTab makes k the visible tab and pulls nvim focus out
// of the panel split so typing lands in the editor, not a hidden shell.
func (m *Model) activateNonTerminalTab(k panelKind) {
	m.panelActive = k
	m.inTerminal = false
	m.ensureEditorWindowCurrent()
}

// activatePanelEntry switches to the tab e (bar click / cycling).
func (m *Model) activatePanelEntry(e panelBarEntry) {
	if e.Kind == panelKindTerminal {
		m.switchTerminalTab(e.Index)
		return
	}
	m.panelFocused = false
	m.activateNonTerminalTab(e.Kind)
}

// cyclePanelTabs moves the active tab by step across ALL tabs (terminal
// and non-terminal), wrapping. No-op with fewer than 2 tabs.
func (m *Model) cyclePanelTabs(step int) {
	entries := m.panelBarEntries()
	if len(entries) < 2 {
		return
	}
	cur := 0
	for i, e := range entries {
		if m.panelEntryActive(e) {
			cur = i
			break
		}
	}
	m.activatePanelEntry(entries[cycleIndex(cur, step, len(entries))])
}

// closePanelTab closes the non-terminal tab of kind k. When it was the
// visible tab, its bar neighbour takes over. The whole panel closes when
// no tab of any kind remains.
func (m *Model) closePanelTab(k panelKind) {
	idx := -1
	for i, t := range m.panelTabs {
		if t == k {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	if spec, ok := panelKinds[k]; ok && spec.OnClose != nil {
		spec.OnClose(m)
	}
	m.panelTabs = append(m.panelTabs[:idx:idx], m.panelTabs[idx+1:]...)
	if len(m.panelTabs) == 0 && len(m.terminalTabs) == 0 {
		m.closeTerminalPanel()
		m.applyLayout()
		return
	}
	if m.panelActive != k {
		return
	}
	m.panelFocused = false
	switch {
	case idx < len(m.panelTabs):
		m.activateNonTerminalTab(m.panelTabs[idx])
	case len(m.terminalTabs) > 0:
		m.switchTerminalTab(m.terminalActiveTab)
	default:
		m.activateNonTerminalTab(m.panelTabs[len(m.panelTabs)-1])
	}
}

// closeActivePanelTab closes whichever tab is visible (Ctrl+Shift+W /
// "Terminal: Close Active Tab"). No-op when the panel is closed.
func (m *Model) closeActivePanelTab() {
	if !m.termOpen {
		return
	}
	if m.panelActive != panelKindTerminal {
		m.closePanelTab(m.panelActive)
	} else if len(m.terminalTabs) > 0 {
		m.closeTerminalTab(m.terminalActiveTab)
	}
	m.applyLayout()
	m.resizeTerminalSplit()
}

// resetPanelState clears the non-terminal tab state when the panel split
// goes away. Output channel contents survive (they live in m.output).
func (m *Model) resetPanelState() {
	m.panelTabs = nil
	m.panelActive = panelKindTerminal
	m.panelFocused = false
	m.panelHostBuf = 0
}

// ensurePanelHost makes sure the panel split exists. When the split is
// missing it opens a fresh bottom split; with showPlaceholder it also puts
// the placeholder scratch buffer into the window (used when no terminal
// tab is left to show). Focus stays on the editor window. Returns false
// when nvim refused.
func (m *Model) ensurePanelHost(showPlaceholder bool) bool {
	if m.nvim == nil {
		return false
	}
	out, err := m.nvim.EvalLuaString(fmt.Sprintf(`
		local rows, win, show = %d, %d, %t
		local buf = vim.g.termocode_panel_buf
		if not (buf and vim.api.nvim_buf_is_valid(buf)) then
			buf = vim.api.nvim_create_buf(false, true)
			if buf <= 0 then return '0:0' end
			vim.bo[buf].bufhidden  = 'hide'
			vim.bo[buf].modifiable = false
			vim.bo[buf].filetype   = 'termocode_panel'
			vim.g.termocode_panel_buf = buf
		end
		local prev = vim.api.nvim_get_current_win()
		if not (win > 0 and vim.api.nvim_win_is_valid(win)) then
			vim.cmd('noautocmd keepalt botright ' .. rows .. 'split')
			win = vim.api.nvim_get_current_win()
			show = true
			if prev == win then prev = nil end
		end
		if show then pcall(vim.api.nvim_win_set_buf, win, buf) end
		vim.w[win].termocode_panel = true
		pcall(function() vim.wo[win].number = false end)
		pcall(function() vim.wo[win].relativenumber = false end)
		pcall(function() vim.wo[win].signcolumn = 'no' end)
		pcall(function() vim.wo[win].foldcolumn = '0' end)
		pcall(function() vim.wo[win].statuscolumn = '' end)
		pcall(function() vim.wo[win].cursorline = false end)
		if prev and vim.api.nvim_win_is_valid(prev) and prev ~= win then
			pcall(vim.api.nvim_set_current_win, prev)
		end
		return tostring(win) .. ':' .. tostring(buf)
	`, m.integratedTerminalRows(), m.terminalWinID, showPlaceholder))
	if err != nil {
		m.err = "panel: " + err.Error()
		return false
	}
	win, buf := parseWinBufPair(out)
	if win <= 0 || buf <= 0 {
		m.err = "panel: failed to open split"
		return false
	}
	m.terminalWinID = win
	m.panelHostBuf = buf
	if !m.termOpen {
		m.termOpen = true
		m.terminalRowsLastSent = 0
		m.invalidateTerminalProbeCache()
	}
	return true
}

// ── Rendering & input for non-terminal tabs ──────────────────────────────

// renderPanelContent returns exactly h rows of w cells for the active
// non-terminal tab, on the editor background (the active tab punches
// through to BgEditor, so the body matches it).
func (m Model) renderPanelContent(w, h int) []string {
	if h <= 0 || w <= 0 {
		return nil
	}
	bg := theme.Bg(theme.BgEditor)
	var rows []string
	if spec, ok := panelKinds[m.panelActive]; ok {
		rows = spec.Render(&m, w, h)
	}
	out := make([]string, h)
	for i := 0; i < h; i++ {
		row := ""
		if i < len(rows) {
			row = rows[i]
		}
		out[i] = padTabBarToWidth(row, w, bg)
	}
	return out
}

// panelContentRect returns the screen rect of the content area of the
// active non-terminal tab (below the tab bar, above the status bar), or
// ok=false when no such tab is visible.
func (m Model) panelContentRect() (x, y, w, h int, ok bool) {
	if !m.termOpen || m.panelActive == panelKindTerminal {
		return 0, 0, 0, 0, false
	}
	bar := m.terminalTabBarRowAbsolute()
	if bar < 0 {
		return 0, 0, 0, 0, false
	}
	w = m.editorPaneWidth()
	x = m.w - w
	y = bar + 1
	h = (m.h - 1) - y // status bar sits on the last row
	if h <= 0 {
		return 0, 0, 0, 0, false
	}
	return x, y, w, h, true
}

// routePanelContentMouse claims mouse events over the content of an active
// non-terminal tab so they never reach the hidden nvim split underneath.
// A left click elsewhere drops panel keyboard focus (without consuming the
// click).
func (m *Model) routePanelContentMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	x, y, w, h, ok := m.panelContentRect()
	inside := ok && msg.X >= x && msg.X < x+w && msg.Y >= y && msg.Y < y+h
	if !inside {
		if msg.Type == tea.MouseLeft {
			m.panelFocused = false
		}
		return nil, false
	}
	spec := panelKinds[m.panelActive]
	switch msg.Type {
	case tea.MouseWheelUp, tea.MouseWheelDown:
		if spec.Scroll != nil {
			delta := 3
			if msg.Type == tea.MouseWheelUp {
				delta = -3
			}
			spec.Scroll(m, delta)
		}
	case tea.MouseLeft:
		m.panelFocused = true
		m.focus = FocusEditor
		if spec.Click != nil {
			return spec.Click(m, msg.Y-y, msg.X-x), true
		}
	}
	return nil, true
}

// routePanelKey gives a focused non-terminal tab first pick of a key.
func (m *Model) routePanelKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if !m.termOpen || m.panelActive == panelKindTerminal {
		m.panelFocused = false
		return nil, false
	}
	if msg.String() == "esc" {
		m.panelFocused = false
		return nil, true
	}
	if spec, ok := panelKinds[m.panelActive]; ok && spec.Key != nil {
		return spec.Key(m, msg)
	}
	return nil, false
}
