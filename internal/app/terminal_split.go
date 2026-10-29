package app

import (
	"fmt"
	"os"
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
