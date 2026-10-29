package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/picker"
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

// termPaneSpawnLua defines _G.termocode_pane_spawn(win, cwd, spec): it puts a
// fresh terminal buffer into window win, starts the shell in cwd (when that
// folder exists), strips the window chrome and enters terminal mode. It
// returns the buffer id, or 0 when the shell did not start. The buffer is
// tagged as a pane, so a shell that ends stays on screen.
const termPaneSpawnLua = termExitedLua + termShellIntLua + termPaneChromeLua + `
	-- Split the current window without nvim evening out every window of the
	-- screen (it would resize the editor above the panel).
	_G.termocode_pane_split = _G.termocode_pane_split or function(down)
		local ea = vim.o.equalalways
		vim.o.equalalways = false
		pcall(vim.cmd, down and 'keepalt rightbelow split' or 'keepalt rightbelow vsplit')
		-- Turning the option back on evens out the windows at once; limit
		-- that to widths so no height (the editor's) changes.
		local ead = vim.o.eadirection
		vim.o.eadirection = 'hor'
		vim.o.equalalways = ea
		vim.o.eadirection = ead
	end
	_G.termocode_pane_spawn = _G.termocode_pane_spawn or function(win, cwd, spec)
		local buf = vim.api.nvim_create_buf(false, true)
		if buf <= 0 then return 0 end
		vim.bo[buf].buflisted = false
		vim.bo[buf].swapfile  = false
		pcall(vim.api.nvim_win_set_buf, win, buf)
		local cmd, env = _G.termocode_shellint(vim.json.decode(spec))
		local opts = {
			env = env,
			on_exit = function(_, code, _)
				vim.g.termocode_term_last_exit = code
				pcall(_G.termocode_term_exited, buf)
			end,
		}
		if vim.fn.isdirectory(cwd) == 1 then opts.cwd = cwd end
		vim.b[buf].termocode_pane = true
		local job
		vim.api.nvim_win_call(win, function() job = vim.fn.termopen(cmd, opts) end)
		if job <= 0 then
			pcall(vim.api.nvim_buf_delete, buf, { force = true })
			return 0
		end
		chrome(win)
		pcall(vim.cmd, 'startinsert')
		return buf
	end
`

// splitTerminalPane splits the focused pane of the active terminal tab: a new
// shell appears beside it (paneRight) or below it (paneDown), in the same
// folder. It returns a toast command when the split is refused.
func (m *Model) splitTerminalPane(dir paneDir) tea.Cmd {
	tab := m.activeTerminalTab()
	if tab == nil {
		return nil
	}
	from := tab.focusedPane()
	return m.splitTerminalPaneAt(from, dir, tab.paneCwd(from))
}

// splitTerminalPaneAt splits pane from of the active terminal tab in
// direction dir; the new shell starts in cwd. The new pane gets the focus.
// It returns a toast command when the split is refused.
func (m *Model) splitTerminalPaneAt(from int, dir paneDir, cwd string) tea.Cmd {
	tab := m.activeTerminalTab()
	if m.nvim == nil || !m.termOpen || tab == nil || m.panelActive != panelKindTerminal ||
		from < 0 || from >= tab.paneCount() {
		return nil
	}
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
	out, err := m.nvim.EvalLuaString(fmt.Sprintf(termPaneSpawnLua+`
		local fromWin, down, cwd, bufs = %d, %t, %q, {%s}
		if not vim.api.nvim_win_is_valid(fromWin) then return '0:0' end
		-- A tab with panes keeps a shell that ended on screen (termExitedLua).
		for _, b in ipairs(bufs) do
			if vim.api.nvim_buf_is_valid(b) then vim.b[b].termocode_pane = true end
		end
		vim.api.nvim_set_current_win(fromWin)
		_G.termocode_pane_split(down)
		local win = vim.api.nvim_get_current_win()
		vim.w[win].termocode_panel = true
		local buf = _G.termocode_pane_spawn(win, cwd, %q)
		if buf <= 0 then
			pcall(vim.api.nvim_win_close, win, true)
			return '0:0'
		end
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
	m.paintPaneTitles()
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
	m.paintPaneTitles()
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
	out, err := m.nvim.EvalLuaString(fmt.Sprintf(termPaneSpawnLua+`
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
						_G.termocode_pane_split(down)
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
	m.paintPaneTitles()
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

// paneTitle is the text of pane i's title bar: "zsh · folder".
func paneTitle(t terminalTab, i int) string {
	name := terminalCwdBasename(t.paneCwd(i))
	switch {
	case t.Shell != "" && name != "":
		return t.Shell + " · " + name
	case t.Shell != "":
		return t.Shell
	}
	return name
}

// paintPaneTitles gives every pane of the active tab a one-row title bar (an
// nvim winbar) in the theme's focus colour while focused and a dim one while
// not; nvim switches between them itself as focus moves. A tab with a single
// pane has no title bar. A pane whose shell ended shows the exit hint.
func (m *Model) paintPaneTitles() {
	tab := m.activeTerminalTab()
	if m.nvim == nil || tab == nil {
		return
	}
	multi := tab.paneCount() > 1
	items := ""
	for i := 0; i < tab.paneCount(); i++ {
		if w := m.paneWin(*tab, i); w > 0 {
			// A restored pane's note shows even on a one-pane tab.
			title, force := paneTitle(*tab, i), false
			if h, ok := m.termHints[tab.paneBuf(i)]; ok {
				title, force = h.Text, true
			}
			items += fmt.Sprintf("{%d,%d,%q,%t},", w, tab.paneBuf(i), title, force)
		}
	}
	_ = m.nvim.ExecLua(fmt.Sprintf(`
		local multi = %t
		for _, e in ipairs({%s}) do
			local win, buf, title, force = e[1], e[2], e[3], e[4]
			if vim.api.nvim_win_is_valid(win) then
				if vim.b[buf] and vim.b[buf].termocode_exited then
					title = %q
				end
				local bar = ''
				if multi or force or title == %q then bar = ' ' .. title:gsub('%%%%', '%%%%%%%%') end
				vim.wo[win].winbar = bar
				vim.wo[win].winhighlight = 'WinBar:TermocodePaneTitle,WinBarNC:TermocodePaneTitleNC'
			end
		end`, multi, items, paneExitedHint, paneExitedHint))
}

// paneExitedHint replaces a pane's title when its shell has ended.
const paneExitedHint = "[exited · Enter to restart · x to close]"

// refreshPaneExits notes which panes' shells have ended (the Lua exit handler
// marks the buffer). Cheap: it asks nvim only when a tab has extra panes.
func (m *Model) refreshPaneExits() {
	if m.nvim == nil {
		return
	}
	bufs := []int{}
	for _, t := range m.terminalTabs {
		if len(t.Splits) > 0 {
			bufs = append(bufs, t.paneBufs()...)
		}
	}
	if len(bufs) == 0 {
		return
	}
	out, err := m.nvim.EvalLuaString(fmt.Sprintf(`local out = {}
		for _, b in ipairs({%s}) do
			if vim.api.nvim_buf_is_valid(b) and vim.b[b].termocode_exited then out[#out + 1] = tostring(b) end
		end
		return table.concat(out, ',')`, intList(bufs)))
	if err != nil {
		return
	}
	exited := map[int]bool{}
	for _, f := range strings.Split(out, ",") {
		exited[parseInt(f)] = true
	}
	for i := range m.terminalTabs {
		for k := range m.terminalTabs[i].Splits {
			m.terminalTabs[i].Splits[k].Exited = exited[m.terminalTabs[i].Splits[k].BufID]
		}
		m.terminalTabs[i].Exited0 = exited[m.terminalTabs[i].BufID]
	}
}

// paneExited reports whether pane i of tab t has a finished shell.
func (t terminalTab) paneExited(i int) bool {
	if i == 0 {
		return t.Exited0
	}
	if i > 0 && i <= len(t.Splits) {
		return t.Splits[i-1].Exited
	}
	return false
}

// restartTerminalPane starts a new shell in pane i, in the same window and
// folder, in place of the one that ended.
func (m *Model) restartTerminalPane(i int) {
	tab := m.activeTerminalTab()
	if m.nvim == nil || tab == nil || i < 0 || i >= tab.paneCount() {
		return
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	win, old := m.paneWin(*tab, i), tab.paneBuf(i)
	out, err := m.nvim.EvalLuaString(fmt.Sprintf(termPaneSpawnLua+`
		local win, old = %d, %d
		if not vim.api.nvim_win_is_valid(win) then return '0' end
		vim.api.nvim_set_current_win(win)
		local buf = _G.termocode_pane_spawn(win, %q, %q)
		if buf > 0 and vim.api.nvim_buf_is_valid(old) then
			pcall(vim.api.nvim_buf_delete, old, { force = true })
		end
		return tostring(buf)`, win, old, tab.paneCwd(i), termShellSpecJSON(shell)))
	buf := parseInt(out)
	if err != nil || buf <= 0 {
		m.err = "terminal: failed to restart the shell"
		return
	}
	if i == 0 {
		tab.BufID, tab.Exited0 = buf, false
	} else {
		tab.Splits[i-1].BufID, tab.Splits[i-1].Exited = buf, false
	}
	tab.PaneFocus = i
	m.paintPaneTitles()
	m.invalidateTerminalProbeCache()
}

// exitedPaneKey handles Enter (restart) and x (close) while the focused pane's
// shell has ended. It reports whether it used the key.
func (m *Model) exitedPaneKey(msg tea.KeyMsg) bool {
	if !m.inPaneWin || !m.termOpen || m.panelActive != panelKindTerminal {
		return false
	}
	tab := m.activeTerminalTab()
	if tab == nil {
		return false
	}
	i := tab.focusedPane()
	if !tab.paneExited(i) {
		return false
	}
	switch msg.String() {
	case "enter":
		m.restartTerminalPane(i)
		return true
	case "x":
		m.closeTerminalPane(i)
		m.applyLayout()
		m.resizeTerminalSplit()
		return true
	}
	return false
}

// resizeTerminalPanes sets the panel to rows shell rows when the active tab
// has several panes, and reports whether it did. Panes can be stacked, so
// the height of one window is not the height of the panel: the editor window
// above it is resized instead and the space moves to or from the panes.
func (m *Model) resizeTerminalPanes(rows int) bool {
	tab := m.activeTerminalTab()
	if tab == nil || tab.paneCount() < 2 || m.panelActive != panelKindTerminal {
		return false
	}
	wins := []int{}
	for i := 0; i < tab.paneCount(); i++ {
		if w := m.paneWin(*tab, i); w > 0 {
			wins = append(wins, w)
		}
	}
	_ = m.nvim.ExecLua(fmt.Sprintf(`
		local rows, wins = %d, {%s}
		local bottom = 0
		for _, w in ipairs(wins) do
			if vim.api.nvim_win_is_valid(w) then
				bottom = math.max(bottom, vim.api.nvim_win_get_position(w)[1] + vim.api.nvim_win_get_height(w) + 1)
			end
		end
		local ed, edBottom = nil, -1
		for _, w in ipairs(vim.api.nvim_list_wins()) do
			if not vim.w[w].termocode_panel and vim.api.nvim_win_get_config(w).relative == '' then
				local b = vim.api.nvim_win_get_position(w)[1] + vim.api.nvim_win_get_height(w)
				if b > edBottom then ed, edBottom = w, b end
			end
		end
		if not ed then return end
		-- Everything below the editor's status line, down to the last pane's
		-- status line, is the panel: it must be rows + 1 tall.
		local delta = (bottom - (edBottom + 1)) - (rows + 1)
		if delta ~= 0 then
			pcall(vim.api.nvim_win_set_height, ed, vim.api.nvim_win_get_height(ed) + delta)
		end`, rows, intList(wins)))
	return true
}

// resizeTerminalPane grows (delta > 0) or shrinks the focused pane by delta
// cells: wider when another pane sits beside it, taller when it is stacked.
// A pane never gets smaller than termPaneMinCols x termPaneMinRows. It
// reports whether a pane was resized.
func (m *Model) resizeTerminalPane(delta int) bool {
	tab := m.activeTerminalTab()
	if m.nvim == nil || tab == nil || tab.paneCount() < 2 {
		return false
	}
	wins := []int{}
	for i := 0; i < tab.paneCount(); i++ {
		if w := m.paneWin(*tab, i); w > 0 {
			wins = append(wins, w)
		}
	}
	win := m.paneWin(*tab, tab.focusedPane())
	if win <= 0 {
		return false
	}
	out, err := m.nvim.EvalLuaString(fmt.Sprintf(`
		local win, delta, wins = %d, %d, {%s}
		if not vim.api.nvim_win_is_valid(win) then return '' end
		local row = vim.api.nvim_win_get_position(win)[1]
		local beside = false
		for _, w in ipairs(wins) do
			if w ~= win and vim.api.nvim_win_is_valid(w) and vim.api.nvim_win_get_position(w)[1] == row then beside = true end
		end
		if beside then
			pcall(vim.api.nvim_win_set_width, win, math.max(vim.api.nvim_win_get_width(win) + delta, %d))
		else
			local bar = vim.wo[win].winbar ~= '' and 1 or 0
			pcall(vim.api.nvim_win_set_height, win, math.max(vim.api.nvim_win_get_height(win) + delta, %d + bar))
		end
		return 'ok'`, win, delta, intList(wins), termPaneMinCols, termPaneMinRows))
	if err != nil || out != "ok" {
		return false
	}
	// Growing a stacked pane can take rows from the editor: put the panel back.
	m.terminalRowsLastSent = 0
	m.resizeTerminalSplit()
	return true
}

// termPanePaletteItems are the palette commands for terminal panes.
func termPanePaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "terminal-split-right", Title: "Terminal: Split Right"},
		{ID: "terminal-split-down", Title: "Terminal: Split Down"},
		{ID: "terminal-close-pane", Title: "Terminal: Close Pane"},
		{ID: "terminal-focus-next-pane", Title: "Terminal: Focus Next Pane"},
		{ID: "terminal-focus-prev-pane", Title: "Terminal: Focus Previous Pane"},
		{ID: "terminal-grow-pane", Title: "Terminal: Grow Pane"},
		{ID: "terminal-shrink-pane", Title: "Terminal: Shrink Pane"},
	}
}

// termPaneStep is how many cells Grow / Shrink Pane move the divider.
const termPaneStep = 4

// runTermPaneAction runs a terminal-pane palette command. ok is false when id
// is not one of them.
func (m *Model) runTermPaneAction(id string) (cmd tea.Cmd, ok bool) {
	switch id {
	case "terminal-split-right", "terminal-split-down":
		dir := paneRight
		if id == "terminal-split-down" {
			dir = paneDown
		}
		if !m.termOpen {
			m.openTerminalPanel()
			m.applyLayout()
			m.resizeTerminalSplit()
		}
		cmd = m.splitTerminalPane(dir)
		m.applyLayout()
	case "terminal-close-pane":
		if tab := m.activeTerminalTab(); tab != nil && m.termOpen {
			m.closeTerminalPane(tab.focusedPane())
			m.applyLayout()
			m.resizeTerminalSplit()
		}
	case "terminal-focus-next-pane":
		m.focusNextTerminalPane(1)
	case "terminal-focus-prev-pane":
		m.focusNextTerminalPane(-1)
	case "terminal-grow-pane":
		m.resizeTerminalPane(termPaneStep)
	case "terminal-shrink-pane":
		m.resizeTerminalPane(-termPaneStep)
	default:
		return nil, false
	}
	return cmd, true
}

// paneSpec is one extra pane to make: pane Parent (0 = the tab's own
// shell) is split in direction Dir and the new shell starts in Cwd ("" =
// the parent's folder).
type paneSpec struct {
	Dir    paneDir
	Parent int
	Cwd    string
}

// buildTerminalPanes makes the extra panes of the active tab, in order. A
// pane that cannot be made (no room, the shell did not start) is left out,
// and panes split off it are split off its parent instead. It returns the
// live pane index of each spec's pane (pane 0 first; a pane left out maps
// to its parent) and the refusal toasts.
func (m *Model) buildTerminalPanes(specs []paneSpec) ([]int, tea.Cmd) {
	tab := m.activeTerminalTab()
	if tab == nil {
		return nil, nil
	}
	live := []int{0} // saved pane index → live pane index
	var cmds []tea.Cmd
	for _, s := range specs {
		parent := 0
		if s.Parent >= 0 && s.Parent < len(live) {
			parent = live[s.Parent]
		}
		cwd := s.Cwd
		if cwd == "" {
			cwd = m.activeTerminalTab().paneCwd(parent)
		}
		before := m.activeTerminalTab().paneCount()
		cmds = append(cmds, m.splitTerminalPaneAt(parent, s.Dir, cwd))
		if n := m.activeTerminalTab().paneCount(); n > before {
			live = append(live, n-1)
		} else {
			live = append(live, parent)
		}
	}
	return live, tea.Batch(cmds...)
}
