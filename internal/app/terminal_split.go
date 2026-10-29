package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
)

// paneWin returns the nvim window of pane i of tab t (0 = the panel window).
func (m Model) paneWin(t terminalTab, i int) int {
	if i == 0 {
		return m.terminalWinID
	}
	if i > 0 && i <= len(t.Splits) {
		return t.Splits[i-1].WinID
	}
	return 0
}

// termPaneChromeLua strips the editor chrome from a shell window (same as
// the tab's own window).
const termPaneChromeLua = `
	local function chrome(w)
		pcall(function() vim.wo[w].number = false end)
		pcall(function() vim.wo[w].relativenumber = false end)
		pcall(function() vim.wo[w].signcolumn = 'no' end)
		pcall(function() vim.wo[w].foldcolumn = '0' end)
		pcall(function() vim.wo[w].statuscolumn = '' end)
		pcall(function() vim.wo[w].cursorline = false end)
	end
`

// splitTerminalPane splits the focused pane of the active terminal tab: a new
// shell appears beside it (paneRight) or below it (paneDown), in the same
// folder. It returns a toast command when the split is refused.
func (m *Model) splitTerminalPane(dir paneDir) tea.Cmd {
	tab := m.activeTerminalTab()
	if m.nvim == nil || !m.termOpen || tab == nil || m.panelActive != panelKindTerminal {
		return nil
	}
	from := tab.focusedPane()
	fromWin := m.paneWin(*tab, from)
	size, err := m.nvim.EvalLuaString(fmt.Sprintf(`
		local w = %d
		if not vim.api.nvim_win_is_valid(w) then return '0:0' end
		return vim.api.nvim_win_get_width(w) .. ':' .. vim.api.nvim_win_get_height(w)`, fromWin))
	if err != nil {
		return nil
	}
	winW, winH := parseWinBufPair(size)
	if winW <= 0 {
		return nil
	}
	if why := splitRefusal(dir, winW, winH, tab.paneCount()); why != "" {
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Info, why)
		return c
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	cwd := tab.paneCwd(from)
	out, err := m.nvim.EvalLuaString(fmt.Sprintf(termExitedLua+termShellIntLua+termPaneChromeLua+`
		local fromWin, down, cwd, bufs = %d, %t, %q, {%s}
		if not vim.api.nvim_win_is_valid(fromWin) then return '0:0' end
		-- A tab with panes keeps a shell that ended on screen (termExitedLua).
		for _, b in ipairs(bufs) do
			if vim.api.nvim_buf_is_valid(b) then vim.b[b].termocode_pane = true end
		end
		vim.api.nvim_set_current_win(fromWin)
		vim.cmd(down and 'keepalt rightbelow split' or 'keepalt rightbelow vsplit')
		local win = vim.api.nvim_get_current_win()
		local buf = vim.api.nvim_create_buf(false, true)
		vim.bo[buf].buflisted = false
		vim.bo[buf].swapfile  = false
		pcall(vim.api.nvim_win_set_buf, win, buf)
		vim.w[win].termocode_panel = true
		local cmd, env = _G.termocode_shellint(vim.json.decode(%q))
		local opts = {
			env = env,
			on_exit = function(_, code, _)
				vim.g.termocode_term_last_exit = code
				pcall(_G.termocode_term_exited, buf)
			end,
		}
		if vim.fn.isdirectory(cwd) == 1 then opts.cwd = cwd end
		local job = vim.fn.termopen(cmd, opts)
		if job <= 0 then
			pcall(vim.api.nvim_win_close, win, true)
			return '0:0'
		end
		vim.b[buf].termocode_pane = true
		chrome(win)
		pcall(vim.cmd, 'startinsert')
		return tostring(win) .. ':' .. tostring(buf)
	`, fromWin, dir == paneDown, cwd, intList(tab.paneBufs()), termShellSpecJSON(shell)))
	if err != nil {
		m.err = "terminal: " + err.Error()
		return nil
	}
	win, buf := parseWinBufPair(out)
	if win <= 0 || buf <= 0 {
		m.err = "terminal: failed to spawn shell"
		return nil
	}
	tab.Splits = append(tab.Splits, termPane{BufID: buf, WinID: win, Cwd: cwd, Dir: dir, Parent: from})
	tab.PaneFocus = len(tab.Splits)
	m.focus = FocusEditor
	m.terminalCwdAt = time.Now()
	m.invalidateTerminalProbeCache()
	return nil
}

// intList renders ids as the inside of a Lua table: "3,5,9".
func intList(ids []int) string {
	s := ""
	for i, id := range ids {
		if i > 0 {
			s += ","
		}
		s += itoa(id)
	}
	return s
}

// syncPaneFocus reports whether nvim's current window win is a shell window
// of the active terminal tab, and if so records which pane it is. A click on
// a pane makes nvim focus its window; this is how termocode learns of it.
func (m *Model) syncPaneFocus(win int) bool {
	tab := m.activeTerminalTab()
	if tab == nil || win <= 0 {
		return false
	}
	for i := 0; i < tab.paneCount(); i++ {
		if m.paneWin(*tab, i) == win {
			tab.PaneFocus = i
			return true
		}
	}
	return false
}

// focusTerminalPane moves the keyboard to pane i of the active tab.
func (m *Model) focusTerminalPane(i int) {
	tab := m.activeTerminalTab()
	if m.nvim == nil || tab == nil || i < 0 || i >= tab.paneCount() {
		return
	}
	win := m.paneWin(*tab, i)
	if win <= 0 {
		return
	}
	tab.PaneFocus = i
	_ = m.nvim.ExecLua(fmt.Sprintf(`
		local w = %d
		if vim.api.nvim_win_is_valid(w) then
			pcall(vim.api.nvim_set_current_win, w)
			vim.schedule(function() pcall(vim.cmd, 'startinsert') end)
		end`, win))
	m.focus = FocusEditor
}

// focusNextTerminalPane moves the keyboard to the next pane, wrapping
// around (step -1 goes back).
func (m *Model) focusNextTerminalPane(step int) {
	tab := m.activeTerminalTab()
	if tab == nil || tab.paneCount() < 2 {
		return
	}
	n := tab.paneCount()
	m.focusTerminalPane(((tab.focusedPane()+step)%n + n) % n)
}

// closeTerminalPane closes pane i of the active tab: its shell stops and its
// window goes; the others share the room. The last pane closes the tab, as
// before.
func (m *Model) closeTerminalPane(i int) {
	tab := m.activeTerminalTab()
	if tab == nil || i < 0 || i >= tab.paneCount() {
		return
	}
	if tab.paneCount() == 1 {
		m.closeTerminalTab(m.terminalActiveTab)
		return
	}
	win, buf := m.paneWin(*tab, i), tab.paneBuf(i)
	next := tab.withoutPane(i)
	if m.nvim != nil {
		// Close the window first, then drop the buffer, so nvim never has to
		// pick a fallback buffer for the window (the editor would leak in).
		_ = m.nvim.ExecLua(fmt.Sprintf(`
			local win, buf = %d, %d
			if vim.api.nvim_win_is_valid(win) then pcall(vim.api.nvim_win_close, win, true) end
			if vim.api.nvim_buf_is_valid(buf) then
				local ok, chan = pcall(function() return vim.bo[buf].channel end)
				if ok and chan and chan > 0 then pcall(vim.fn.jobstop, chan) end
				pcall(vim.api.nvim_buf_delete, buf, { force = true })
			end`, win, buf))
	}
	if i == 0 {
		// The next pane takes over the panel window.
		m.terminalWinID = tab.Splits[0].WinID
	}
	*tab = next
	m.focusTerminalPane(tab.PaneFocus)
	m.invalidateTerminalProbeCache()
	m.pruneAgentMCPFiles()
}

// reconcileTerminalPanes makes the nvim windows match the active tab: the
// panes of every other tab are hidden (their shells keep running), and the
// active tab's panes get windows again if they have none (after a tab
// switch or after the panel was hidden). Cheap when nothing changes: it
// only looks at Go state until a window has to be made or closed.
func (m *Model) reconcileTerminalPanes() {
	if m.nvim == nil {
		return
	}
	active := -1
	if m.termOpen && m.panelActive == panelKindTerminal && len(m.terminalTabs) > 0 {
		active = min(max(m.terminalActiveTab, 0), len(m.terminalTabs)-1)
	}
	var hide []int
	for i := range m.terminalTabs {
		if i == active {
			continue
		}
		for k := range m.terminalTabs[i].Splits {
			if w := m.terminalTabs[i].Splits[k].WinID; w > 0 {
				hide = append(hide, w)
				m.terminalTabs[i].Splits[k].WinID = 0
			}
		}
	}
	if len(hide) > 0 {
		_ = m.nvim.ExecLua(fmt.Sprintf(`for _, w in ipairs({%s}) do
			if vim.api.nvim_win_is_valid(w) then pcall(vim.api.nvim_win_hide, w) end
		end`, intList(hide)))
	}
	if active < 0 {
		return
	}
	tab := &m.terminalTabs[active]
	missing := false
	for _, s := range tab.Splits {
		if s.WinID <= 0 {
			missing = true
		}
	}
	if !missing || m.terminalWinID <= 0 {
		return
	}
	rows := ""
	for _, s := range tab.Splits {
		rows += fmt.Sprintf("{%d,%d,%t,%d},", s.BufID, s.Parent, s.Dir == paneDown, s.WinID)
	}
	out, err := m.nvim.EvalLuaString(fmt.Sprintf(termPaneChromeLua+`
		local rows, wins, out = {%s}, { [0] = %d }, {}
		for k, r in ipairs(rows) do
			local buf, parent, down, win = r[1], r[2], r[3], r[4]
			if not vim.api.nvim_buf_is_valid(buf) then
				out[#out + 1] = k .. ':-1'
			else
				if win > 0 and vim.api.nvim_win_is_valid(win) then
					wins[k] = win
				else
					local pw = wins[parent]
					if pw and vim.api.nvim_win_is_valid(pw) then
						vim.api.nvim_set_current_win(pw)
						vim.cmd(down and 'keepalt rightbelow split' or 'keepalt rightbelow vsplit')
						local nw = vim.api.nvim_get_current_win()
						pcall(vim.api.nvim_win_set_buf, nw, buf)
						vim.w[nw].termocode_panel = true
						chrome(nw)
						wins[k] = nw
					end
				end
				out[#out + 1] = k .. ':' .. (wins[k] or 0)
			end
		end
		return table.concat(out, ',')`, rows, m.terminalWinID))
	if err != nil {
		return
	}
	dead := []int{}
	for _, part := range strings.Split(out, ",") {
		k, w := splitColonInt(part)
		if k < 1 || k > len(tab.Splits) {
			continue
		}
		if w < 0 {
			dead = append(dead, k)
			continue
		}
		tab.Splits[k-1].WinID = w
	}
	// A pane whose shell is gone (its buffer was deleted while the panel was
	// hidden) leaves the tab. Highest index first so the others keep theirs.
	for j := len(dead) - 1; j >= 0; j-- {
		*tab = tab.withoutPane(dead[j])
	}
}

// splitColonInt parses "k:v" where v may be negative.
func splitColonInt(s string) (int, int) {
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return 0, 0
	}
	v := s[i+1:]
	if strings.HasPrefix(v, "-") {
		return parseInt(s[:i]), -parseInt(v[1:])
	}
	return parseInt(s[:i]), parseInt(v)
}

// dropTabSplits stops the shells of a tab's extra panes and closes their
// windows (the tab is going away).
func (m *Model) dropTabSplits(t terminalTab) {
	if m.nvim == nil || len(t.Splits) == 0 {
		return
	}
	var bufs []int
	for _, s := range t.Splits {
		bufs = append(bufs, s.BufID)
	}
	_ = m.nvim.ExecLua(fmt.Sprintf(`for _, buf in ipairs({%s}) do
		for _, w in ipairs(vim.fn.win_findbuf(buf)) do pcall(vim.api.nvim_win_close, w, true) end
		if vim.api.nvim_buf_is_valid(buf) then
			local ok, chan = pcall(function() return vim.bo[buf].channel end)
			if ok and chan and chan > 0 then pcall(vim.fn.jobstop, chan) end
			pcall(vim.api.nvim_buf_delete, buf, { force = true })
		end
	end`, intList(bufs)))
}
