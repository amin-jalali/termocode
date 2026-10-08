package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"golang.design/x/clipboard"

	"github.com/amin-jalali/termocode/internal/theme"
)

// openShellCmd suspends the TUI and runs the user's shell attached to the
// real TTY. When the shell exits (via `exit`), termocode resumes.
//
// This is the full-handoff path retained for the command palette
// ("Terminal: Open External Shell"). The day-to-day toggle is the embedded
// integrated terminal panel — see (*Model).toggleTerminalPanel.
func openShellCmd() tea.Cmd {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	c := exec.Command(shell)
	c.Env = os.Environ()
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return ErrMsg{Err: err}
		}
		return nil
	})
}

// terminalTab is a single entry in the integrated-terminal tab bar. Each
// tab owns a distinct nvim terminal buffer; switching tabs swaps the buffer
// in the single visible terminal window via nvim_win_set_buf. The PTY job
// stays alive for inactive tabs, so output keeps streaming and the user
// finds their shell exactly where they left it on switch-back.
type terminalTab struct {
	// BufID is the nvim buffer-id holding this tab's terminal job. Used as
	// the lookup key for nvim_win_set_buf on switch and for nvim_buf_delete
	// on close.
	BufID int
	// Name is the display label rendered in the tab. Initialised to the
	// basename of Cwd at spawn time; updated on cd via b:term_title polling
	// (most modern shells emit OSC 7).
	Name string
	// Cwd is the directory the tab's shell currently sits in. Refreshed
	// from b:term_title at terminalCwdRefreshTTL cadence. Used both for
	// the tab name and for the right-side cwd echo.
	Cwd string
	// Shell is the basename of the shell binary ("bash", "zsh", "fish",
	// "nu", …). Drives the per-tab icon (`$`, `%`, `>`, `λ`).
	Shell string
	// LastExit is the most recent exit status of the tab's last command,
	// fed back from termopen()'s on_exit callback. Sentinel values:
	//   -1  → no command run yet (or job still running) → green dot
	//    0  → last command exited cleanly                → green dot
	//   >0  → last command failed                        → red dot
	// Drives the right-side status-dot color in the tab bar.
	LastExit int
	// Group C — Task is the task label for tabs started by the task runner
	// (tasks_run.go): the tab keeps that name (no cwd renaming) and its
	// glyph shows ▶ while Running, then ✓ / ✘ from LastExit.
	Task    string
	Running bool
}

// integratedTerminalRows returns the live total height (in cells) of the
// embedded terminal panel when toggled open. The bar/resizer is INSIDE
// this height (it overpaints nvim's terminal split row 0), so visible
// shell content = integratedTerminalRows() - 1.
//
// Backed by Model.terminalRows, which the drag-splitter on the tab-bar row
// mutates and which persists across sessions via session.json. A zero /
// out-of-range value falls back to terminalRowsDefault so an uninitialized
// Model (tests, struct literals) still renders a sensible panel.
func (m Model) integratedTerminalRows() int {
	if m.terminalMinimized {
		return 0
	}
	if m.terminalRows < terminalRowsMin {
		return terminalRowsDefault
	}
	return m.terminalRows
}

// openIntegratedTerminalAt opens (or focuses) the integrated terminal
// panel and `cd`s the active tab's shell into `dir`. Used by the explorer's
// "Open in Terminal" / "Reveal in Terminal" right-click actions —
// previously those suspended termocode and spawned an external shell,
// which left the user in a sub-shell at a different cwd that they
// often confused with the parent shell on exit.
func (m *Model) openIntegratedTerminalAt(dir string) {
	if m.nvim == nil {
		return
	}
	// Open the panel if it isn't already. toggleTerminalPanel uses
	// nvim's actual buffer state as source of truth, so calling it
	// here is safe even when m.termOpen is stale.
	if !m.termOpen {
		m.toggleTerminalPanel()
	}
	tab := m.activeTerminalTab()
	if tab == nil {
		return
	}
	// Send `cd <dir>` + newline to the active tab's terminal job. Quoting
	// goes through Lua's %q so paths with spaces / special chars survive.
	cmd := "cd " + shellQuote(dir) + "\n"
	_ = m.nvim.ExecLua(fmt.Sprintf(`
		local buf = %d
		if vim.api.nvim_buf_is_loaded(buf) and vim.bo[buf].buftype == 'terminal' then
			local ok, chan = pcall(function() return vim.bo[buf].channel end)
			if ok and chan and chan > 0 then
				pcall(vim.fn.chansend, chan, %q)
			end
		end
	`, tab.BufID, cmd))
	// Update the tab's cwd / name immediately. b:term_title polling will
	// overwrite this once the shell catches up.
	tab.Cwd = dir
	tab.Name = terminalCwdBasename(dir)
	m.terminalCwd = dir
	m.terminalCwdAt = time.Now()
	m.focus = FocusEditor
}

// activeTerminalTab returns a pointer to the active tab, or nil when no
// tabs exist. Index out-of-range is clamped to [0, len-1] on read.
func (m *Model) activeTerminalTab() *terminalTab {
	if len(m.terminalTabs) == 0 {
		return nil
	}
	if m.terminalActiveTab < 0 || m.terminalActiveTab >= len(m.terminalTabs) {
		m.terminalActiveTab = 0
	}
	return &m.terminalTabs[m.terminalActiveTab]
}

// shellQuote wraps s in single quotes and escapes embedded single
// quotes so the resulting token is safe for POSIX-style shells.
func shellQuote(s string) string {
	return "'" + escapeSingleQuotes(s) + "'"
}

func escapeSingleQuotes(s string) string {
	out := make([]byte, 0, len(s)+8)
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			out = append(out, '\'', '\\', '\'', '\'')
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}

// detectShellBasename returns the basename of $SHELL, or "bash" as a
// fallback. Used to label the per-tab icon in the tab bar.
func detectShellBasename() string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		return "bash"
	}
	return filepath.Base(sh)
}

// shellGlyphFor returns the 1-cell icon to render to the LEFT of a tab's
// name. `$` for bash/sh, `%` for zsh, `>` for fish, `λ` for nu / unknown.
func shellGlyphFor(shell string) string {
	switch shell {
	case "bash", "sh":
		return "$"
	case "zsh":
		return "%"
	case "fish":
		return ">"
	case "nu":
		return "λ"
	}
	return "λ"
}

// toggleTerminalPanel opens or closes the embedded :terminal panel.
//
// Approach: nvim hosts a single horizontal split window at the bottom of
// its grid. Each tab in m.terminalTabs owns its own nvim terminal BUFFER;
// switching tabs calls nvim_win_set_buf to swap the visible buffer in the
// single window. PTY jobs for inactive tabs stay alive — on switch-back
// the user lands exactly where they left off.
//
// Closing the panel jobstops every tab's terminal job, force-deletes the
// buffers, and resets the tabs slice. Reopening rebuilds from scratch.
func (m *Model) toggleTerminalPanel() {
	if m.nvim == nil {
		return
	}
	// Panel open on a non-terminal tab (Output, …): Ctrl+T brings the
	// terminal forward instead of closing everything (VSCode behaviour).
	if m.termOpen && m.panelActive != panelKindTerminal {
		m.showPanelTab(panelKindTerminal)
		return
	}
	if m.probeTerminalBufferLive() {
		m.closeTerminalPanel()
		return
	}
	m.openTerminalPanel()
}

// openTerminalPanel opens a fresh terminal split + first tab. Idempotent
// when called with the panel already open: returns without touching nvim.
// When the split is already open with only non-terminal tabs, it adds the
// first terminal tab into that split instead.
func (m *Model) openTerminalPanel() {
	if m.nvim == nil {
		return
	}
	if m.termOpen {
		if len(m.terminalTabs) == 0 {
			m.newTerminalTab()
		}
		return
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	// Single Lua chunk so the buffer/window/job state changes happen
	// atomically — splitting them across multiple nvim_command calls left
	// a race where keystrokes typed during the gap could land in the wrong
	// window. The chunk:
	//   1. opens an empty N-row split at the bottom (botright new)
	//   2. spawns the user's $SHELL via termopen() in that buffer
	//   3. captures the win-id + buf-id so Go-side can reuse them across
	//      tab switches and resize calls
	//   4. defers startinsert via vim.schedule so the prompt is interactive
	//      from the user's first keystroke
	luaOpen := fmt.Sprintf(`
		vim.cmd('keepalt botright %dnew')
		local buf = vim.api.nvim_get_current_buf()
		vim.bo[buf].buflisted = false
		vim.bo[buf].swapfile  = false
		local termWin = vim.api.nvim_get_current_win()
		-- Tag the panel split so ensureEditorWindowCurrent never picks it
		-- (it may later hold the non-terminal placeholder buffer).
		vim.w[termWin].termocode_panel = true
		local job = vim.fn.termopen(%q, {
			on_exit = function(_, code, _)
				vim.g.termocode_term_last_exit = code
				pcall(function()
					if vim.api.nvim_buf_is_valid(buf) then
						vim.api.nvim_buf_delete(buf, { force = true })
					end
				end)
			end,
		})
		if job <= 0 then
			vim.cmd('close')
			return '0:0'
		end
		-- Strip editor chrome from the terminal window so the shell pane
		-- looks like a clean shell (no line-number gutter, signcolumn,
		-- foldcolumn, custom statuscolumn, or cursorline). These are
		-- window-local so the editor windows keep their gutter.
		pcall(function() vim.wo[termWin].number = false end)
		pcall(function() vim.wo[termWin].relativenumber = false end)
		pcall(function() vim.wo[termWin].signcolumn = 'no' end)
		pcall(function() vim.wo[termWin].foldcolumn = '0' end)
		pcall(function() vim.wo[termWin].statuscolumn = '' end)
		pcall(function() vim.wo[termWin].cursorline = false end)
		-- Land in terminal-insert mode immediately so the prompt is live
		-- from the user's first keystroke. The global BufEnter/WinEnter
		-- autocmd (model.go) handles re-focus, and the global ModeChanged
		-- autocmd handles the click-drops-out-of-terminal-insert case.
		if vim.api.nvim_buf_is_valid(buf) and vim.bo[buf].buftype == 'terminal' then
			pcall(vim.cmd, 'startinsert')
		end
		return tostring(termWin) .. ':' .. tostring(buf)
	`, m.integratedTerminalRows(), shell)
	out, err := m.nvim.EvalLuaString(luaOpen)
	if err != nil {
		m.err = "terminal: " + err.Error()
		return
	}
	winID, bufID := parseWinBufPair(out)
	if winID <= 0 || bufID <= 0 {
		m.err = "terminal: failed to spawn shell"
		return
	}
	m.terminalWinID = winID
	cwd, _ := os.Getwd()
	tab := terminalTab{
		BufID:    bufID,
		Cwd:      cwd,
		Name:     terminalCwdBasename(cwd),
		Shell:    detectShellBasename(),
		LastExit: -1,
	}
	if tab.Name == "" {
		tab.Name = "shell"
	}
	m.terminalTabs = []terminalTab{tab}
	m.terminalActiveTab = 0
	m.panelActive = panelKindTerminal
	m.terminalMinimized = false
	m.focus = FocusEditor
	m.termOpen = true
	m.terminalCwd = cwd
	m.terminalCwdAt = time.Now()
	m.invalidateTerminalProbeCache()
}

// closeTerminalPanel jobstops every tab's terminal job, deletes the
// underlying buffers, and resets all per-panel state. Safe to call when
// the panel is already closed.
func (m *Model) closeTerminalPanel() {
	if m.nvim == nil {
		return
	}
	_ = m.nvim.ExecLua(`
		for _, b in ipairs(vim.api.nvim_list_bufs()) do
			if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buftype == 'terminal' then
				local ok, chan = pcall(function() return vim.bo[b].channel end)
				if ok and chan and chan > 0 then pcall(vim.fn.jobstop, chan) end
				pcall(vim.api.nvim_buf_delete, b, { force = true })
			end
		end
		-- Non-terminal placeholder (panel.go): deleting it closes the split
		-- when it is the buffer on display.
		local pb = vim.g.termocode_panel_buf
		if pb and vim.api.nvim_buf_is_valid(pb) then
			pcall(vim.api.nvim_buf_delete, pb, { force = true })
		end
		vim.g.termocode_panel_buf = nil
	`)
	m.resetPanelState()
	m.termOpen = false
	m.terminalCwd = ""
	m.terminalCwdAt = time.Time{}
	m.terminalRowsLastSent = 0
	m.terminalTabs = nil
	m.terminalActiveTab = 0
	m.terminalWinID = 0
	m.terminalMinimized = false
	m.invalidateTerminalProbeCache()
}

// newTerminalTab spawns a brand-new terminal buffer and adds it as the
// active tab. The freshly-spawned buffer is set as the visible buffer in
// the existing terminal window via nvim_win_set_buf. When the panel isn't
// open yet, this just delegates to openTerminalPanel (which creates the
// first tab from scratch).
func (m *Model) newTerminalTab() {
	if m.nvim == nil {
		return
	}
	if !m.termOpen {
		m.openTerminalPanel()
		return
	}
	if m.terminalWinID <= 0 {
		m.terminalWinID = m.probeTerminalWindowID()
		if m.terminalWinID <= 0 {
			// Lost the window — fall back to a full reopen so the user
			// still gets a working tab.
			m.closeTerminalPanel()
			m.openTerminalPanel()
			return
		}
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	luaNew := fmt.Sprintf(`
		local termWin = %d
		if not vim.api.nvim_win_is_valid(termWin) then return '0' end
		local buf = vim.api.nvim_create_buf(false, true)
		if buf <= 0 then return '0' end
		vim.bo[buf].buflisted = false
		vim.bo[buf].swapfile  = false
		pcall(vim.api.nvim_win_set_buf, termWin, buf)
		pcall(vim.api.nvim_set_current_win, termWin)
		local job = vim.fn.termopen(%q, {
			on_exit = function(_, code, _)
				vim.g.termocode_term_last_exit = code
				pcall(function()
					if vim.api.nvim_buf_is_valid(buf) then
						vim.api.nvim_buf_delete(buf, { force = true })
					end
				end)
			end,
		})
		if job <= 0 then return '0' end
		-- Strip editor chrome from the terminal window so the shell pane
		-- looks like a clean shell (no line-number gutter, signcolumn,
		-- foldcolumn, custom statuscolumn, or cursorline). Window-local
		-- so editor windows keep their gutter.
		pcall(function() vim.wo[termWin].number = false end)
		pcall(function() vim.wo[termWin].relativenumber = false end)
		pcall(function() vim.wo[termWin].signcolumn = 'no' end)
		pcall(function() vim.wo[termWin].foldcolumn = '0' end)
		pcall(function() vim.wo[termWin].statuscolumn = '' end)
		pcall(function() vim.wo[termWin].cursorline = false end)
		-- Same as openTerminalPanel: immediate startinsert; global autocmds
		-- in model.go cover BufEnter/WinEnter re-focus AND the click-out
		-- case via ModeChanged *:nt.
		if vim.api.nvim_buf_is_valid(buf) and vim.bo[buf].buftype == 'terminal' then
			pcall(vim.cmd, 'startinsert')
		end
		return tostring(buf)
	`, m.terminalWinID, shell)
	out, err := m.nvim.EvalLuaString(luaNew)
	if err != nil {
		m.err = "terminal: " + err.Error()
		return
	}
	bufID := parseInt(out)
	if bufID <= 0 {
		m.err = "terminal: failed to spawn shell"
		return
	}
	cwd, _ := os.Getwd()
	tab := terminalTab{
		BufID:    bufID,
		Cwd:      cwd,
		Name:     terminalCwdBasename(cwd),
		Shell:    detectShellBasename(),
		LastExit: -1,
	}
	if tab.Name == "" {
		tab.Name = "shell"
	}
	m.terminalTabs = append(m.terminalTabs, tab)
	m.terminalActiveTab = len(m.terminalTabs) - 1
	m.panelActive = panelKindTerminal
	m.panelFocused = false
	m.terminalCwd = cwd
	m.terminalCwdAt = time.Now()
	m.terminalMinimized = false
}

// closeTerminalTab closes the tab at idx. If it's the last tab, the entire
// panel closes. Otherwise the active index shifts to the previous tab
// (clamped to [0, len-1]).
//
// Order of operations matters: when the closed tab IS the visible buffer
// in the terminal window, deleting its buffer first causes nvim to fall
// back to the alternate (the editor buffer, since terminal buffers are
// buflisted=false), which paints the editor through the panel area for
// one frame — the "editor leaks through on tab close" regression. We
// avoid this by swapping the visible buffer to the new active tab FIRST,
// then deleting the closed tab's buffer, so the terminal window always
// holds a valid terminal buffer.
func (m *Model) closeTerminalTab(idx int) {
	if idx < 0 || idx >= len(m.terminalTabs) {
		return
	}
	if len(m.terminalTabs) == 1 {
		if len(m.panelTabs) == 0 {
			m.closeTerminalPanel()
			return
		}
		// Non-terminal tabs remain: keep the split, park the placeholder
		// buffer in it, then drop the last shell.
		m.closeLastTerminalKeepPanel()
		return
	}
	// Restore editor focus afterwards if a non-terminal tab is on show —
	// the buffer swap below focuses the terminal window.
	defer func() {
		if m.panelActive != panelKindTerminal {
			m.ensureEditorWindowCurrent()
		}
	}()
	closedTab := m.terminalTabs[idx]
	wasActive := idx == m.terminalActiveTab

	// Compute the post-removal active index, mirroring the original
	// fall-through clamp + shift-down rules:
	//   active > idx         → shift down by 1 (same tab, new index)
	//   active == idx        → keep index (now points at the old next-tab)
	//   active < idx         → unchanged
	//   clamped to len-2 if it would otherwise overflow the new slice
	newActive := m.terminalActiveTab
	if newActive > idx {
		newActive--
	}
	if newActive >= len(m.terminalTabs)-1 {
		// Closing the last tab while it was active: drop to the new last
		// index (which is len-2 in the pre-removal slice).
		newActive = len(m.terminalTabs) - 2
	}
	if newActive < 0 {
		newActive = 0
	}

	// Resolve the bufID of the new active tab BEFORE we mutate the slice.
	// `newActive` is into the post-removal slice; map it back to the
	// pre-removal slice by skipping over `idx`.
	newActiveBuf := 0
	{
		preIdx := newActive
		if preIdx >= idx {
			preIdx++
		}
		if preIdx >= 0 && preIdx < len(m.terminalTabs) {
			newActiveBuf = m.terminalTabs[preIdx].BufID
		}
	}

	if m.nvim != nil {
		// Single Lua chunk so the swap+delete pair is atomic from nvim's
		// perspective — no in-between frame where the window holds the
		// to-be-deleted buffer with no replacement queued.
		_ = m.nvim.ExecLua(fmt.Sprintf(`
			local termWin = %d
			local newBuf  = %d
			local oldBuf  = %d
			local wasActive = %t
			-- 1. If the closed tab was visible, swap the new active tab's
			--    buffer into the terminal window FIRST so nvim never has to
			--    fall back to the editor buffer.
			if wasActive
				and vim.api.nvim_win_is_valid(termWin)
				and vim.api.nvim_buf_is_loaded(newBuf) then
				pcall(vim.api.nvim_win_set_buf, termWin, newBuf)
				pcall(vim.api.nvim_set_current_win, termWin)
			end
			-- 2. Now it's safe to jobstop + delete the closed buffer.
			if vim.api.nvim_buf_is_loaded(oldBuf) then
				local ok, chan = pcall(function() return vim.bo[oldBuf].channel end)
				if ok and chan and chan > 0 then pcall(vim.fn.jobstop, chan) end
				pcall(vim.api.nvim_buf_delete, oldBuf, { force = true })
			end
		`, m.terminalWinID, newActiveBuf, closedTab.BufID, wasActive))
	}

	// Update Go-side state to match the new nvim state.
	m.terminalTabs = append(m.terminalTabs[:idx], m.terminalTabs[idx+1:]...)
	m.terminalActiveTab = newActive
	if wasActive && newActive >= 0 && newActive < len(m.terminalTabs) {
		// Mirror switchTerminalTab's cwd bookkeeping for the new active tab.
		// Skip the actual buffer swap — already done atomically above.
		m.terminalCwd = m.terminalTabs[newActive].Cwd
		m.terminalCwdAt = time.Now()
	}
}

// closeLastTerminalKeepPanel closes the only terminal tab while non-terminal
// tabs stay open: the placeholder goes into the split FIRST (so nvim never
// closes the window or shows the editor buffer there), then the shell is
// stopped and its buffer deleted.
func (m *Model) closeLastTerminalKeepPanel() {
	if len(m.terminalTabs) == 0 {
		return
	}
	buf := m.terminalTabs[0].BufID
	m.ensurePanelHost(true)
	if m.nvim != nil {
		_ = m.nvim.ExecLua(fmt.Sprintf(`
			local b = %d
			if vim.api.nvim_buf_is_loaded(b) then
				local ok, chan = pcall(function() return vim.bo[b].channel end)
				if ok and chan and chan > 0 then pcall(vim.fn.jobstop, chan) end
				pcall(vim.api.nvim_buf_delete, b, { force = true })
			end
		`, buf))
	}
	m.terminalTabs = nil
	m.terminalActiveTab = 0
	m.terminalCwd = ""
	if m.panelActive == panelKindTerminal && len(m.panelTabs) > 0 {
		m.panelActive = m.panelTabs[len(m.panelTabs)-1]
	}
	m.inTerminal = false
	m.ensureEditorWindowCurrent()
	m.invalidateTerminalProbeCache()
}

// switchTerminalTab sets the active tab to idx and swaps the visible buffer
// in the terminal window. No-op when idx is out of range. Also makes the
// terminal kind the visible panel tab.
func (m *Model) switchTerminalTab(idx int) {
	if idx < 0 || idx >= len(m.terminalTabs) {
		return
	}
	m.terminalActiveTab = idx
	m.panelActive = panelKindTerminal
	m.panelFocused = false
	if m.nvim == nil || m.terminalWinID <= 0 {
		return
	}
	tab := m.terminalTabs[idx]
	_ = m.nvim.ExecLua(fmt.Sprintf(`
		local termWin = %d
		local buf = %d
		if vim.api.nvim_win_is_valid(termWin) and vim.api.nvim_buf_is_loaded(buf) then
			pcall(vim.api.nvim_win_set_buf, termWin, buf)
			pcall(vim.api.nvim_set_current_win, termWin)
			vim.schedule(function() pcall(vim.cmd, 'startinsert') end)
		end
	`, m.terminalWinID, tab.BufID))
	m.terminalCwd = tab.Cwd
	m.terminalCwdAt = time.Now()
}

// cycleTerminalTab moves the active tab by `step` (negative cycles back),
// wrapping around. Cycles ALL bottom-panel tabs — terminals and Output /
// Problems / … alike (panel.go). No-op when fewer than 2 tabs exist.
func (m *Model) cycleTerminalTab(step int) {
	m.cyclePanelTabs(step)
}

// minimizeTerminalPanel collapses the panel to just the tab-bar row.
// nvim's terminal split is shrunk to 0 rows visually; the PTY job stays
// alive so the user finds their shell intact on un-minimize.
func (m *Model) minimizeTerminalPanel() {
	m.terminalMinimized = true
	m.applyLayout()
	m.resizeTerminalSplit()
}

// maximizeTerminalPanel grows the panel to fill (most of) the editor area.
// Caps to leave at least 5 rows for the editor above (matches the drag
// upper bound).
func (m *Model) maximizeTerminalPanel() {
	m.terminalMinimized = false
	max := m.h - 8
	if max < terminalRowsMin {
		max = terminalRowsMin
	}
	m.terminalRows = max
	m.applyLayout()
	m.resizeTerminalSplit()
	m.persistTerminalRows()
}

// parseWinBufPair parses "winID:bufID" into (winID, bufID). Returns
// (0, 0) on any parse error so callers can fall through gracefully.
func parseWinBufPair(s string) (int, int) {
	colon := strings.IndexByte(s, ':')
	if colon < 0 {
		return 0, 0
	}
	return parseInt(s[:colon]), parseInt(s[colon+1:])
}

func parseInt(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// resizeTerminalSplit pushes the current m.terminalRows into the live nvim
// terminal split via nvim_win_set_height. Cheap no-op when the panel isn't
// open or when the value hasn't changed since the last call (so a fast
// drag firing >20 MouseMotion events per second doesn't spam nvim with
// redundant resize RPCs).
//
// terminalRows = visible shell rows (NOT panel total). The panel is
// terminalRows + 1 rows on screen — the extra +1 is the tab-bar/resizer
// row that overpaints nvim's split-separator (the row that would
// otherwise show the editor buffer's filename).
func (m *Model) resizeTerminalSplit() {
	if !m.termOpen || m.nvim == nil {
		return
	}
	rows := m.integratedTerminalRows()
	if rows == m.terminalRowsLastSent {
		return
	}
	m.terminalRowsLastSent = rows
	if rows <= 0 {
		// Minimized — squash the split to 1 row (nvim won't allow 0).
		rows = 1
	}
	// Prefer the known panel window (it may hold the non-terminal
	// placeholder buffer); fall back to the first terminal window.
	_ = m.nvim.ExecLua(fmt.Sprintf(`
		local rows, panelWin = %d, %d
		if panelWin > 0 and vim.api.nvim_win_is_valid(panelWin) then
			pcall(vim.api.nvim_win_set_height, panelWin, rows)
			return
		end
		for _, w in ipairs(vim.api.nvim_list_wins()) do
			local b = vim.api.nvim_win_get_buf(w)
			if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buftype == 'terminal' then
				pcall(vim.api.nvim_win_set_height, w, rows)
				break
			end
		end
	`, rows, m.terminalWinID))
}

// persistTerminalRows writes the current m.terminalRows into the session
// file so the user's drag-set panel height survives a relaunch. Called
// once at the end of a drag (on release / cancel) — issuing a write per
// MouseMotion would burn disk I/O for no user-visible benefit.
func (m *Model) persistTerminalRows() {
	existing := loadSession()
	existing.TerminalRows = m.terminalRows
	saveSession(existing)
}

// integratedTerminalHeight returns the total number of rows the integrated
// terminal panel occupies when open. The bar/resizer is INSIDE this height
// (it overpaints nvim's terminal split row 0), so the total = the panel's
// drag-set rows (or 1 when minimized — just the bar). Returns 0 when the
// panel is closed so callers can fold the value into chrome math without
// an extra branch.
func (m Model) integratedTerminalHeight() int {
	if !m.termOpen {
		return 0
	}
	if m.terminalMinimized {
		return 1
	}
	return m.integratedTerminalRows()
}

// terminalTabBarRowAbsolute returns the screen-space Y coordinate of the
// terminal tab-bar row, or -1 when the panel isn't open. Used by the mouse
// router to hit-test tab clicks, "+", and the right-side controls.
//
// Position: status bar at m.h-1; nvim's terminal split (terminalRows of
// shell content) at rows [m.h-1-terminalRows .. m.h-2]; nvim's split
// separator (which we overpaint with the bar) at row m.h-2-terminalRows.
// So the bar's screen Y is m.h-2-terminalRows.
func (m Model) terminalTabBarRowAbsolute() int {
	if !m.termOpen {
		return -1
	}
	return m.h - 2 - m.integratedTerminalRows()
}

// editorPaneWidth duplicates the small bit of math from view.go::renderBase
// that derives the editor pane's width from the global geometry. Pulled out
// here so the terminal tab bar / mouse hit-test can share the same source
// of truth without re-deriving it inline in three places.
func (m Model) editorPaneWidth() int {
	w := m.w - 4 - 0 // activity.Width is 4, scrollbar is 0
	if m.showExp {
		w -= m.explorerWidth
	}
	if w < 1 {
		w = 1
	}
	return w
}

// ── Tab-bar render ────────────────────────────────────────────────────────
//
// Layout, left to right:
//
//   "Terminal" │ ≡ Output × │ <icon> name × │ <icon> name × │ + ……… ● ↑ − ×
//
// The leading label is the active tab's kind title ("Terminal", "Output",
// …). Non-terminal tabs (panel.go) come first, then terminal tabs; "+"
// always adds a terminal. One row, full editor-pane width. Active tab
// punches through to the editor body bg (BgEditor). Inactive tabs inherit
// the tab-bar bg (BgPanel). All styling pulls from the active theme; no
// raw hex literals.

const (
	terminalTabMaxNameW   = 16 // truncate tab name to this many cells with "…"
	terminalTabBarMinTabW = 4  // smallest cell-width a single tab occupies
)

// terminalTabSlot is the rendered geometry of one tab segment in the bar,
// returned alongside the rendered string so the mouse router can hit-test
// click targets without re-deriving the column layout.
type terminalTabSlot struct {
	// Kind of the tab. Index points into m.terminalTabs for
	// panelKindTerminal and into m.panelTabs for every other kind.
	Kind  panelKind
	Index int
	// Active tab gets the editor-bg punch-through.
	Active bool
	// Half-open editor-pane-LOCAL column range [Start, End) for the whole
	// tab segment (used for click-to-activate hit-testing).
	Start, End int
	// Half-open column range for the close-x button inside the tab.
	CloseStart, CloseEnd int
}

// terminalTabBarLayout is the precomputed hit-map for the tab bar, used
// by both the renderer and the mouse router so click targets land exactly
// on what's drawn.
type terminalTabBarLayout struct {
	Tabs []terminalTabSlot
	// New-tab "+" button.
	NewStart, NewEnd int
	// Right-side controls (status dot is decorative — no click). Each
	// control button is 1 cell wide.
	MaxStart   int // ↑
	MinStart   int // −
	CloseStart int // ×  (closes the entire panel)
	// Width of the bar (= editor pane width).
	Width int
}

// computeTerminalTabBarLayout returns the column-precise layout of the tab
// bar at the given pane width. Pure function of (m.terminalTabs, width) so
// it's safe to call from both the renderer and the mouse router.
func (m Model) computeTerminalTabBarLayout(width int) terminalTabBarLayout {
	layout := terminalTabBarLayout{Width: width}
	if width <= 0 {
		return layout
	}
	// Reserve space for the right-side controls. From the right edge:
	//   1 col right pad
	//   1 col × (close panel)
	//   1 col − (minimize)
	//   1 col ↑ (maximize)
	//   1 col gap
	//   1 col ● (status dot)
	const rightWidth = 6 // pad + × + − + ↑ + gap + ●
	rightStart := width - rightWidth
	if rightStart < 0 {
		rightStart = 0
	}
	layout.MaxStart = rightStart + 2     // dot(1) + gap(1) → ↑
	layout.MinStart = layout.MaxStart + 1 // ↑(1) → −
	layout.CloseStart = layout.MinStart + 1

	// Left segment: " <Label> │ " (1 pad + label + 1 gap + 1 sep + 1 gap).
	// Build the layout incrementally so it stays correct if widths shift.
	col := 0
	col++                                                       // " " (left pad)
	col += runewidth.StringWidth(m.panelTitle(m.panelActive))   // "Terminal" / "Output" / …
	col++                                                       // " " (gap before separator)
	col++                                                       // "│" (separator)
	col++                                                       // " " (gap after separator)
	tabsStart := col

	// Tabs: each rendered as " icon name × ", in panelBarEntries order.
	entries := m.panelBarEntries()
	for i, e := range entries {
		tabName := m.panelEntryName(e)
		// Visible cell-width budget for the tab segment.
		nameW := runewidth.StringWidth(tabName)
		if nameW > terminalTabMaxNameW {
			nameW = terminalTabMaxNameW
		}
		// Layout per tab: " <icon> <name> <×> "
		//                 1 + 1 +   1   + nameW + 1 + 1 + 1 = 6 + nameW
		segW := 6 + nameW
		if segW < terminalTabBarMinTabW {
			segW = terminalTabBarMinTabW
		}
		// Bail out gracefully if we'd overflow into the right controls
		// region (or the "+" / right-margin reservations).
		// Need to leave at least: 1 col gap + 1 col "+" + 1 col gap + right.
		if col+segW+3 > rightStart {
			break
		}
		slot := terminalTabSlot{
			Kind:       e.Kind,
			Index:      e.Index,
			Active:     m.panelEntryActive(e),
			Start:      col,
			End:        col + segW,
			CloseStart: col + segW - 2, // " ×"
			CloseEnd:   col + segW - 1, // half-open; just the ×
		}
		layout.Tabs = append(layout.Tabs, slot)
		col += segW
		// Inter-tab separator " │ " (3 cells), unless this is the last one
		// or either neighbour is the active tab (spec).
		if i < len(entries)-1 {
			nextIsActive := m.panelEntryActive(entries[i+1])
			thisIsActive := slot.Active
			if !nextIsActive && !thisIsActive {
				col += 3
			} else {
				col += 1 // single col gap when adjacent to the active tab
			}
			if col+segW+3 > rightStart {
				// Out of room — stop adding tabs.
				break
			}
		}
	}
	_ = tabsStart

	// "+" button after the last visible tab. 1 col gap each side.
	if col+3 <= rightStart {
		layout.NewStart = col + 1
		layout.NewEnd = layout.NewStart + 1
	} else {
		layout.NewStart = -1
		layout.NewEnd = -1
	}
	return layout
}

// renderTerminalTabBar emits the 1-row tab-bar that sits above the nvim
// terminal split. Layout matches computeTerminalTabBarLayout.
func (m Model) renderTerminalTabBar(width int) string {
	if width <= 0 {
		return ""
	}
	layout := m.computeTerminalTabBarLayout(width)

	bg := theme.Bg(theme.BgPanel)
	editorBg := theme.Bg(theme.BgEditor)
	label := theme.FgBg(theme.TextPrimary, theme.BgPanel)
	sepStyle := theme.FgBg(theme.BorderDefault, theme.BgPanel)
	muted := theme.FgBg(theme.TextSecondary, theme.BgPanel)
	mutedDim := theme.FgBg(theme.TextMuted, theme.BgPanel)
	primary := theme.FgBg(theme.TextPrimary, theme.BgEditor)
	accent := theme.FgBg(theme.AccentLavender, theme.BgEditor).Bold(true)
	mutedShellOnPanel := theme.FgBg(theme.TextMuted, theme.BgPanel)
	closeOnPanel := theme.FgBg(theme.TextMuted, theme.BgPanel)
	closeOnEditor := theme.FgBg(theme.TextMuted, theme.BgEditor)

	var out strings.Builder

	// Left segment: " <active kind title> │ "
	out.WriteString(bg.Render(" "))
	out.WriteString(label.Render(m.panelTitle(m.panelActive)))
	out.WriteString(bg.Render(" "))
	out.WriteString(sepStyle.Render("│"))
	out.WriteString(bg.Render(" "))

	// Tabs.
	for i, slot := range layout.Tabs {
		e := panelBarEntry{Kind: slot.Kind, Index: slot.Index}
		name := m.panelEntryName(e)
		if runewidth.StringWidth(name) > terminalTabMaxNameW {
			name = runewidth.Truncate(name, terminalTabMaxNameW, "…")
		}
		glyph := m.panelEntryGlyph(e)
		if slot.Active {
			out.WriteString(editorBg.Render(" "))
			out.WriteString(accent.Render(glyph))
			out.WriteString(editorBg.Render(" "))
			out.WriteString(primary.Render(name))
			out.WriteString(editorBg.Render(" "))
			out.WriteString(closeOnEditor.Render("×"))
			out.WriteString(editorBg.Render(" "))
		} else {
			out.WriteString(bg.Render(" "))
			out.WriteString(mutedShellOnPanel.Render(glyph))
			out.WriteString(bg.Render(" "))
			out.WriteString(muted.Render(name))
			out.WriteString(bg.Render(" "))
			out.WriteString(closeOnPanel.Render("×"))
			out.WriteString(bg.Render(" "))
		}
		// Inter-tab separator. Skip when adjacent to the active tab
		// (matches computeTerminalTabBarLayout's column math).
		if i < len(layout.Tabs)-1 {
			nextActive := layout.Tabs[i+1].Active
			thisActive := slot.Active
			if !nextActive && !thisActive {
				out.WriteString(bg.Render(" "))
				out.WriteString(sepStyle.Render("│"))
				out.WriteString(bg.Render(" "))
			} else {
				out.WriteString(bg.Render(" "))
			}
		}
	}

	// "+" button.
	if layout.NewStart >= 0 {
		out.WriteString(bg.Render(" "))
		out.WriteString(mutedDim.Render("+"))
	}

	// Pad with bg up to the right-controls region.
	have := lipgloss.Width(out.String())
	rightStart := width - 6 // see computeTerminalTabBarLayout
	if rightStart < 0 {
		rightStart = 0
	}
	if have < rightStart {
		out.WriteString(bg.Render(strings.Repeat(" ", rightStart-have)))
	}

	// Right-side controls: status dot, gap, ↑, −, ×, right pad. The dot
	// reports the active shell, so it is blank while a non-terminal tab
	// is on show.
	if m.panelActive == panelKindTerminal {
		dotStyle := terminalStatusDotStyle(m.activeTerminalLastExit())
		out.WriteString(dotStyle.Render("●"))
	} else {
		out.WriteString(bg.Render(" "))
	}
	out.WriteString(bg.Render(" "))
	out.WriteString(mutedDim.Render("↑"))
	out.WriteString(mutedDim.Render("−"))
	out.WriteString(mutedDim.Render("×"))
	out.WriteString(bg.Render(" "))

	return padTabBarToWidth(out.String(), width, bg)
}

// panelEntryName returns the tab label for a bar entry: the shell cwd
// basename for terminals, the kind title otherwise.
func (m Model) panelEntryName(e panelBarEntry) string {
	if e.Kind != panelKindTerminal {
		return m.panelTitle(e.Kind)
	}
	if e.Index < 0 || e.Index >= len(m.terminalTabs) {
		return "shell"
	}
	if name := m.terminalTabs[e.Index].Name; name != "" {
		return name
	}
	return "shell"
}

// panelEntryGlyph returns the 1-cell icon for a bar entry.
func (m Model) panelEntryGlyph(e panelBarEntry) string {
	if e.Kind != panelKindTerminal {
		return panelKindIcon(e.Kind)
	}
	if e.Index < 0 || e.Index >= len(m.terminalTabs) {
		return shellGlyphFor("")
	}
	if t := m.terminalTabs[e.Index]; t.Task != "" { // Group C
		return taskTabGlyph(t)
	}
	return shellGlyphFor(m.terminalTabs[e.Index].Shell)
}

// terminalStatusDotStyle returns the lipgloss style for the right-side
// status dot. Green = idle / last-cmd-ok, red = last-cmd-failed, gray =
// running (we don't have a "running" signal yet — palette uses gray when
// a job is mid-flight in a future enhancement).
func terminalStatusDotStyle(lastExit int) lipgloss.Style {
	switch {
	case lastExit < 0:
		// No command run yet OR no active tab → green dot (idle).
		return theme.FgBg(theme.AccentGreen, theme.BgPanel)
	case lastExit == 0:
		return theme.FgBg(theme.AccentGreen, theme.BgPanel)
	default:
		return theme.FgBg(theme.AccentRedCoral, theme.BgPanel)
	}
}

// activeTerminalLastExit returns the active tab's last-exit code, or -1
// when no tabs exist.
func (m Model) activeTerminalLastExit() int {
	if len(m.terminalTabs) == 0 {
		return -1
	}
	idx := m.terminalActiveTab
	if idx < 0 || idx >= len(m.terminalTabs) {
		idx = 0
	}
	return m.terminalTabs[idx].LastExit
}

// padTabBarToWidth ensures the rendered bar is exactly `w` cells wide,
// padding with the panel bg style so the splice into renderBase doesn't
// leak terminal default bg under the bar. Trims from the right if the row
// went over (defensive — shouldn't happen, but cheap insurance).
func padTabBarToWidth(s string, w int, bgStyle lipgloss.Style) string {
	have := lipgloss.Width(s)
	if have == w {
		return s
	}
	if have < w {
		return s + bgStyle.Render(strings.Repeat(" ", w-have))
	}
	return truncRightVisual(s, w)
}

// spliceTerminalTabBar overpaints nvim's split-separator row with the
// termocode-painted tab bar. The separator is the 1-row band nvim draws
// between two horizontally-split windows — without this overpaint it
// would show the editor buffer's filename leaking just above our panel.
//
// Layout produced (always editorH rows tall):
//
//	rows[0..barIdx-1]  → editor body (nvim's editor split)
//	rows[barIdx]       → tab bar / resizer  ← OVERPAINTED, was nvim's separator
//	rows[barIdx+1..]   → shell content (nvim's terminal split, terminalRows rows)
//
// where barIdx = editorH - terminalRows - 1. The bar sits one row ABOVE
// nvim's terminal split (on the separator), so the panel from the bar
// down to the bottom-most terminal row is terminalRows + 1 rows tall.
//
// Minimized mode: terminalRows = 0, so barIdx = editorH - 1 → bar sits
// one row above the status bar; nvim's terminal is squashed to 1 row.
func (m Model) spliceTerminalTabBar(editorContent string, paneW, editorH int) string {
	rows := strings.Split(editorContent, "\n")
	bar := m.renderTerminalTabBar(paneW)
	bgPad := theme.Bg(theme.BgEditor).Render(strings.Repeat(" ", paneW))

	// Pad/clip rows to exactly editorH so the overpaint index is always valid.
	for len(rows) < editorH {
		rows = append(rows, bgPad)
	}
	if len(rows) > editorH {
		rows = rows[:editorH]
	}

	// barIdx = the separator row, one row above where nvim's terminal split begins.
	barIdx := editorH - m.integratedTerminalRows() - 1
	if barIdx >= editorH {
		barIdx = editorH - 1
	}
	if barIdx < 0 {
		barIdx = 0
	}
	rows[barIdx] = bar

	// Non-terminal tab on show (Output, Problems, …): Go paints its
	// content over nvim's split rows below the bar (panel.go).
	if m.panelActive != panelKindTerminal && barIdx+1 < editorH {
		copy(rows[barIdx+1:], m.renderPanelContent(paneW, editorH-barIdx-1))
	}

	return strings.Join(rows, "\n")
}

// terminalCwdBasename returns the basename of `dir`, or "" when `dir` is
// empty. Used in the tab name so paths don't shove the close button off
// the edge on long absolute paths.
func terminalCwdBasename(dir string) string {
	if dir == "" {
		return ""
	}
	dir = strings.TrimRight(dir, "/")
	if dir == "" {
		return "/"
	}
	base := filepath.Base(dir)
	if base == "." || base == "" {
		return dir
	}
	return base
}

// ── Mouse hit-tests for the tab bar ───────────────────────────────────────

// terminalTabBarHit identifies what (if anything) on the tab-bar row was
// clicked. Returned by terminalTabBarHitTest so the mouse router can
// dispatch without re-deriving the layout.
type terminalTabBarHit int

const (
	terminalHitNone terminalTabBarHit = iota
	terminalHitTabClose
	terminalHitTabActivate
	terminalHitNewTab
	terminalHitMaximize
	terminalHitMinimize
	terminalHitClosePanel
	// terminalHitDrag means the click landed on the bar but not on any
	// button — start a vertical drag-resize.
	terminalHitDrag
	// panelHitTabClose / panelHitTabActivate are the non-terminal tab
	// counterparts (Output, Problems, …); the index is into m.panelTabs.
	panelHitTabClose
	panelHitTabActivate
)

// terminalTabBarHitTest maps an absolute screen coordinate to a tab-bar
// hit. Returns (terminalHitNone, -1) when the coord doesn't fall on the
// tab-bar row at all. Tab hits carry an index into m.terminalTabs
// (terminalHit*) or m.panelTabs (panelHit*).
func (m Model) terminalTabBarHitTest(absX, absY int) (terminalTabBarHit, int) {
	if !m.termOpen {
		return terminalHitNone, -1
	}
	y := m.terminalTabBarRowAbsolute()
	if y < 0 || absY != y {
		return terminalHitNone, -1
	}
	edPaneW := m.editorPaneWidth()
	editorStart := m.w - edPaneW
	localX := absX - editorStart
	if localX < 0 || localX >= edPaneW {
		return terminalHitNone, -1
	}
	layout := m.computeTerminalTabBarLayout(edPaneW)
	// Right-side controls (1 cell each).
	if localX == layout.MaxStart {
		return terminalHitMaximize, -1
	}
	if localX == layout.MinStart {
		return terminalHitMinimize, -1
	}
	if localX == layout.CloseStart {
		return terminalHitClosePanel, -1
	}
	// "+"
	if layout.NewStart >= 0 && localX >= layout.NewStart && localX < layout.NewEnd {
		return terminalHitNewTab, -1
	}
	// Per-tab close-x.
	for _, slot := range layout.Tabs {
		if localX >= slot.CloseStart && localX < slot.CloseEnd {
			if slot.Kind != panelKindTerminal {
				return panelHitTabClose, slot.Index
			}
			return terminalHitTabClose, slot.Index
		}
	}
	// Per-tab activate.
	for _, slot := range layout.Tabs {
		if localX >= slot.Start && localX < slot.End {
			if slot.Kind != panelKindTerminal {
				return panelHitTabActivate, slot.Index
			}
			return terminalHitTabActivate, slot.Index
		}
	}
	// Anywhere else on the row → drag.
	return terminalHitDrag, -1
}

// ── Probe / sync helpers ─────────────────────────────────────────────────

const terminalProbeCacheTTL = 250 * time.Millisecond

var (
	terminalProbeMu  sync.Mutex
	terminalProbeAt  time.Time
	terminalProbeVal bool
)

// invalidateTerminalProbeCache clears the cached probe result so the next
// syncTerminalState call hits nvim. Used after toggleTerminalPanel mutates
// the buffer state.
func (m *Model) invalidateTerminalProbeCache() {
	terminalProbeMu.Lock()
	terminalProbeAt = time.Time{}
	terminalProbeMu.Unlock()
}

// ensureEditorWindowCurrent makes nvim's current window a non-terminal
// (editor) window, so a subsequent `:edit`/`:buffer` lands in the editor
// area instead of the integrated-terminal split.
//
// Why this exists: opening a file loads the buffer into nvim's CURRENT
// window. When the terminal panel is focused, the terminal split IS that
// window — so the file would displace the running shell and render in the
// middle of the terminal panel. Every file-open path (picker, recent files,
// search, explorer, tab switch) must call this first.
//
// No-op (and cheap) when the panel is closed or no terminal window exists:
// in that case the current window is already an editor window. If somehow
// only terminal windows exist, this leaves the current window unchanged and
// the caller's edit falls back to nvim's default behaviour.
func (m *Model) ensureEditorWindowCurrent() {
	if m.nvim == nil || !m.termOpen {
		return
	}
	// Skip the panel split too (vim.w.termocode_panel): it may hold the
	// non-terminal placeholder buffer, which is not a terminal.
	_ = m.nvim.ExecLua(`
		for _, w in ipairs(vim.api.nvim_list_wins()) do
			local b = vim.api.nvim_win_get_buf(w)
			if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buftype ~= 'terminal'
				and not vim.w[w].termocode_panel then
				pcall(vim.api.nvim_set_current_win, w)
				break
			end
		end
	`)
}

// probeTerminalBufferLive does a fresh, uncached check of nvim for any
// loaded terminal buffer. Used by toggleTerminalPanel (where we need
// authoritative state) and by syncTerminalState after the cache TTL
// expires.
func (m Model) probeTerminalBufferLive() bool {
	if m.nvim == nil {
		return false
	}
	// The panel also counts as live while its non-terminal placeholder
	// buffer (panel.go) is on display in a window.
	out, _ := m.nvim.EvalLuaString(`
		for _, b in ipairs(vim.api.nvim_list_bufs()) do
			if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buftype == 'terminal' then
				return '1'
			end
		end
		local pb = vim.g.termocode_panel_buf
		if pb and vim.api.nvim_buf_is_valid(pb) and vim.fn.bufwinid(pb) ~= -1 then
			return '1'
		end
		return ''
	`)
	return out == "1"
}

// probeTerminalWindowID returns the win-id of the (first) live terminal
// window, or 0 when none exists. Used to recover terminalWinID after a
// stale-state event (e.g. nvim's on_exit nuked the buffer/window pair on
// the user's `exit` keystroke).
func (m Model) probeTerminalWindowID() int {
	if m.nvim == nil {
		return 0
	}
	out, _ := m.nvim.EvalLuaString(`
		for _, w in ipairs(vim.api.nvim_list_wins()) do
			local b = vim.api.nvim_win_get_buf(w)
			if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buftype == 'terminal' then
				return tostring(w)
			end
		end
		return '0'
	`)
	return parseInt(out)
}

// syncTerminalState reconciles m.termOpen with nvim's actual buffer state.
// Called from view.go on each render frame (cheap — cached). When the
// shell exits on its own (Ctrl+D / `exit`), nvim's on_exit callback nukes
// the terminal buffer but our flag stays true; this catches that drift on
// the next render and flips the flag back.
func (m *Model) syncTerminalState() (changed bool) {
	if m.nvim == nil {
		return false
	}
	terminalProbeMu.Lock()
	now := time.Now()
	stale := terminalProbeAt.IsZero() || now.Sub(terminalProbeAt) > terminalProbeCacheTTL
	if stale {
		terminalProbeMu.Unlock()
		live := m.probeTerminalBufferLive()
		terminalProbeMu.Lock()
		terminalProbeVal = live
		terminalProbeAt = now
	}
	live := terminalProbeVal
	terminalProbeMu.Unlock()
	if m.termOpen && !live {
		m.resetPanelState()
		m.termOpen = false
		m.terminalCwd = ""
		m.terminalCwdAt = time.Time{}
		m.terminalRowsLastSent = 0
		m.terminalTabs = nil
		m.terminalActiveTab = 0
		m.terminalWinID = 0
		m.terminalMinimized = false
		return true
	}
	return false
}

// refreshTerminalCwd polls nvim for every active tab's `b:term_title`,
// which most modern shells update from OSC 7 / OSC 0. Throttled to
// terminalCwdRefreshTTL so the per-render cost stays negligible.
const terminalCwdRefreshTTL = 250 * time.Millisecond

func (m *Model) refreshTerminalCwd() {
	if !m.termOpen || m.nvim == nil || len(m.terminalTabs) == 0 {
		return
	}
	if !m.terminalCwdAt.IsZero() && time.Since(m.terminalCwdAt) < terminalCwdRefreshTTL {
		return
	}
	m.terminalCwdAt = time.Now()
	// Filter out tabs whose buffers got nuked (user ran `exit` inside the
	// tab, on_exit callback fired nvim_buf_delete). Keeps the tab bar in
	// sync with reality without forcing the user to click the close-x.
	alive := m.terminalTabs[:0]
	for i := range m.terminalTabs {
		buf := m.terminalTabs[i].BufID
		out, err := m.nvim.EvalLuaString(fmt.Sprintf(`
			local b = %d
			if not vim.api.nvim_buf_is_loaded(b) then return '__DEAD__' end
			local ok, t = pcall(vim.api.nvim_buf_get_var, b, 'term_title')
			if ok and type(t) == 'string' and t ~= '' then return t end
			return ''
		`, buf))
		if out == "__DEAD__" {
			// Skip this tab — it's gone.
			continue
		}
		alive = append(alive, m.terminalTabs[i])
		if err != nil || out == "" || m.terminalTabs[i].Task != "" { // Group C: task tabs keep their name
			continue
		}
		cwd := strings.TrimSpace(out)
		if j := strings.LastIndex(cwd, ": "); j >= 0 {
			cwd = strings.TrimSpace(cwd[j+2:])
		}
		if cwd == "" {
			continue
		}
		idx := len(alive) - 1
		alive[idx].Cwd = cwd
		alive[idx].Name = terminalCwdBasename(cwd)
		if alive[idx].Name == "" {
			alive[idx].Name = "shell"
		}
	}
	m.terminalTabs = alive
	if m.terminalActiveTab >= len(m.terminalTabs) {
		m.terminalActiveTab = len(m.terminalTabs) - 1
	}
	if m.terminalActiveTab < 0 {
		m.terminalActiveTab = 0
	}
	if len(m.terminalTabs) == 0 && len(m.panelTabs) > 0 {
		// Every shell exited but Output / Problems / … tabs remain: keep
		// (or reopen) the split with the placeholder buffer in it.
		m.terminalCwd = ""
		m.terminalActiveTab = 0
		if m.panelActive == panelKindTerminal {
			m.panelActive = m.panelTabs[len(m.panelTabs)-1]
		}
		m.ensurePanelHost(true)
		m.inTerminal = false
		m.ensureEditorWindowCurrent()
		m.invalidateTerminalProbeCache()
		return
	}
	if len(m.terminalTabs) == 0 {
		// No live tabs → close the panel state.
		m.resetPanelState()
		m.termOpen = false
		m.terminalCwd = ""
		m.terminalRowsLastSent = 0
		m.terminalWinID = 0
		m.terminalMinimized = false
		m.invalidateTerminalProbeCache()
		return
	}
	if m.terminalActiveTab >= 0 && m.terminalActiveTab < len(m.terminalTabs) {
		m.terminalCwd = m.terminalTabs[m.terminalActiveTab].Cwd
	}
}

// ── Terminal pass-through key gating ─────────────────────────────────────
//
// When the user is actively typing into the integrated terminal (m.inTerminal
// == true; see model.go field doc + the StateMsg handler), termocode's
// global keymap normally swallows shortcuts like Ctrl+C (= ActionCopy).
// That breaks every shell convention: a user pressing Ctrl+C inside the
// terminal expects to send SIGINT to the running command, not copy the
// selection.
//
// shouldPassToTerminal reports whether `msg` is a terminal-meaningful
// shortcut that must reach the PTY when the terminal pane has focus. The
// caller (handleGlobalKey) skips m.keys.Match / Action dispatch entirely
// for these keys and relies on the editor's normal nvim_input forward
// path (via dispatchToFocus → editor.Update → translateKey → client.Input)
// to land them in nvim — which is already in terminal-insert mode and
// thus passes them straight to the shell's stdin.
//
// The pass-through set:
//   - Ctrl+letter (Ctrl+A through Ctrl+Z) — every shell uses these for
//     line editing (Ctrl+A start-of-line, Ctrl+E end-of-line, Ctrl+U
//     clear-line, Ctrl+W delete-word, …), job control (Ctrl+C SIGINT,
//     Ctrl+Z SIGTSTP), search (Ctrl+R reverse-i-search), history
//     (Ctrl+P/N), screen control (Ctrl+L clear).
//   - Tab, Backspace, Delete, Enter — line editing & completion.
//   - Up / Down — shell history.
//   - Left / Right, Home / End, PageUp / PageDown — line navigation /
//     scrollback.
//   - Ctrl+Left / Ctrl+Right — word-wise navigation.
//   - Plain printable runes — typing.
//
// Explicitly NOT passed through (so termocode chrome still works from
// inside the terminal):
//   - Ctrl+Shift+anything — these are termocode-level (palette,
//     new-terminal-tab, …) and don't conflict with shell shortcuts.
//   - Alt-letter combos — termocode menu navigation.
//   - F-keys (F1-F12) — palette / find / debug / etc.
//   - Ctrl+\ — bound to ActionSplitVertical; we still need it reachable.
//   - The terminal-panel actions themselves (ActionToggleTerminal /
//     ActionTerminalNewTab / ActionTerminalCloseTab / ActionTerminalNext
//     / ActionTerminalPrevTab) — without these the user can't manage
//     the panel from inside it. Ctrl+T (toggle) is the user's escape
//     hatch back to the editor; it's intercepted instead of passed.
func shouldPassToTerminal(msg tea.KeyMsg) bool {
	s := msg.String()

	// Termocode-managed terminal-panel actions: the user must be able
	// to toggle / close / cycle the panel from inside it, otherwise
	// they're stuck once focus lands inside. These bindings live in
	// keymap.go::Default(); keep the literal key names in sync if
	// they change there.
	switch s {
	case "ctrl+t", "ctrl+`", "alt+`",
		"ctrl+shift+`", "ctrl+shift+w",
		"ctrl+shift+pgup", "ctrl+shift+pgdown":
		return false
	}

	// Plain F-keys (F1-F20) — termocode chrome (palette, find, debug
	// step keys). A shell that genuinely uses an F-key is rare; the
	// chrome priority wins.
	if len(s) >= 2 && s[0] == 'f' {
		allDigits := true
		for i := 1; i < len(s); i++ {
			if s[i] < '0' || s[i] > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return false
		}
	}
	// Modifier+F-key chord (shift+f4, ctrl+f12, …). Detect by checking
	// whether the segment after the last "+" is "f<digits>".
	if idx := strings.LastIndex(s, "+"); idx >= 0 && idx+1 < len(s) {
		tail := s[idx+1:]
		if len(tail) >= 2 && tail[0] == 'f' {
			allDigits := true
			for i := 1; i < len(tail); i++ {
				if tail[i] < '0' || tail[i] > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				return false
			}
		}
	}

	// Ctrl+Shift+anything — termocode-level (palette, …).
	if strings.HasPrefix(s, "ctrl+shift+") {
		return false
	}

	// Alt+letter (and alt+shift+letter) — termocode menu / chord
	// bindings (alt+a actions panel, alt+z zen, alt+b git blame, …).
	// nvim's terminal-insert does treat <M-x> as escape sequences some
	// shells use, but the conflict tilts toward termocode — power users
	// who rely on shell <M-b>/<M-f> word jumps can still use
	// Ctrl+Left / Ctrl+Right.
	if strings.HasPrefix(s, "alt+") {
		return false
	}

	// Ctrl+\ — bound to ActionSplitVertical. The shell rarely uses it
	// (Ctrl+\ sends SIGQUIT but most users prefer Ctrl+C); keeping
	// vertical-split reachable is more useful.
	if s == "ctrl+\\" {
		return false
	}

	// Everything below this point is a pass-through candidate. ─────────

	// Plain Ctrl+letter (Ctrl+A through Ctrl+Z) — pass through. The
	// headline case: Ctrl+C → SIGINT, Ctrl+D → EOF, Ctrl+R → reverse
	// history search, Ctrl+L → clear, Ctrl+U → kill-line, etc.
	if strings.HasPrefix(s, "ctrl+") && len(s) == len("ctrl+")+1 {
		return true
	}

	// Single-key chords / motion / typing.
	switch msg.Type {
	case tea.KeyTab, tea.KeyShiftTab,
		tea.KeyBackspace, tea.KeyDelete,
		tea.KeyEnter, tea.KeySpace,
		tea.KeyUp, tea.KeyDown, tea.KeyLeft, tea.KeyRight,
		tea.KeyShiftUp, tea.KeyShiftDown, tea.KeyShiftLeft, tea.KeyShiftRight,
		tea.KeyHome, tea.KeyEnd, tea.KeyPgUp, tea.KeyPgDown,
		tea.KeyEsc:
		return true
	case tea.KeyRunes:
		// Plain printable typing.
		return true
	}

	// Ctrl+arrow / Ctrl+Home / Ctrl+End — word-wise / start-end-of-line
	// navigation; shells with readline forward these to the line editor.
	switch s {
	case "ctrl+left", "ctrl+right",
		"ctrl+up", "ctrl+down",
		"ctrl+home", "ctrl+end",
		"ctrl+pgup", "ctrl+pgdown",
		"shift+left", "shift+right",
		"shift+up", "shift+down",
		"shift+home", "shift+end",
		"shift+pgup", "shift+pgdown":
		return true
	}

	return false
}

// ── Terminal clipboard shortcuts (Ctrl+Shift+C / Ctrl+Shift+V) ───────────
//
// Standard terminal-emulator shortcuts that users expect to work the same
// way termocode's host terminal does. The shell itself ignores these chords
// (Ctrl+Shift+* are emulator-level), so we implement them at the termocode
// layer when m.inTerminal is true. See handleGlobalKey for the dispatch.
//
// IMPORTANT: these helpers run only on the inTerminal path; the editor's
// existing Ctrl+Shift+V / Ctrl+Shift+C bindings (clipboard-history picker,
// no-op) keep working when the terminal pane isn't focused.

// terminalPasteFromClipboard reads the host OS clipboard and chansends its
// contents into the active terminal tab's PTY. The shell sees the bytes
// as if the user had typed them, so multi-line content (e.g. a copied
// `git clone <url>` line) lands on the prompt and Enter executes it.
//
// No-ops when nvim is gone, no terminal tab exists, or the clipboard is
// empty (don't send empty bytes to the PTY).
func (m Model) terminalPasteFromClipboard() {
	if m.nvim == nil {
		return
	}
	tab := m.activeTerminalTab()
	if tab == nil {
		return
	}
	text := readClipboardText()
	if text == "" {
		return
	}
	// Lua %q safely quotes the payload — embedded newlines, quotes, and
	// non-printables are escaped into a Lua string literal, then chansend
	// emits the original bytes to the PTY.
	_ = m.nvim.ExecLua(fmt.Sprintf(`
		local buf = %d
		if vim.api.nvim_buf_is_loaded(buf) and vim.bo[buf].buftype == 'terminal' then
			local ok, chan = pcall(function() return vim.bo[buf].channel end)
			if ok and chan and chan > 0 then
				pcall(vim.fn.chansend, chan, %q)
			end
		end
	`, tab.BufID, text))
}

// terminalCopyToClipboard reads the active terminal buffer's lines and
// pushes them to the host OS clipboard. Returns the line count copied
// so the caller can build a feedback toast. Returns 0 when there's
// nothing to copy (no nvim, no tab, buffer empty).
//
// Termocode's terminals don't have a vim-style visual selection (clicks
// drop into terminal-insert immediately via the ModeChanged *:nt
// autocmd in model.go), so the practical "copy" is the entire current
// buffer content. Trailing empty lines are stripped so the user doesn't
// paste a wad of trailing newlines.
func (m Model) terminalCopyToClipboard() int {
	if m.nvim == nil {
		return 0
	}
	tab := m.activeTerminalTab()
	if tab == nil {
		return 0
	}
	out, err := m.nvim.EvalLuaString(fmt.Sprintf(`
		local buf = %d
		if not vim.api.nvim_buf_is_loaded(buf) then return '' end
		if vim.bo[buf].buftype ~= 'terminal' then return '' end
		local lines = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
		-- Strip trailing empty lines so the clipboard payload doesn't
		-- carry a wad of blank tail-rows the terminal pads with.
		while #lines > 0 and lines[#lines] == '' do
			table.remove(lines)
		end
		return table.concat(lines, '\n')
	`, tab.BufID))
	if err != nil || out == "" {
		return 0
	}
	safeClipboardWrite(out)
	// Also forward via OSC 52 so the user's local clipboard receives the
	// payload when termocode is running over SSH (mirrors editorCopy).
	emitOSC52(out)
	return strings.Count(out, "\n") + 1
}

// readClipboardText returns the host OS clipboard as a string. Wraps
// clipboard.Read in the same defer-recover guard as safeClipboardWrite —
// the library hard-panics on CGO_ENABLED=0 builds. Returns "" on panic
// or when the clipboard is empty / non-text.
func readClipboardText() string {
	var out string
	defer func() { _ = recover() }()
	buf := clipboard.Read(clipboard.FmtText)
	if len(buf) == 0 {
		return ""
	}
	out = string(buf)
	return out
}
