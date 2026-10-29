package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Hiding the terminal panel (Ctrl+T) keeps the shells running, as VS Code
// does: the panel window closes but the terminal buffers and their jobs
// stay, and the next Ctrl+T shows the same tabs again — a dev server or a
// long build is not killed by hiding the panel. Closing the last tab (or
// "Close Panel") still ends the shells.

// hideTerminalPanel closes the panel window and remembers the terminal
// tabs for showTerminalPanel.
func (m *Model) hideTerminalPanel() {
	if m.nvim == nil || len(m.terminalTabs) == 0 {
		m.closeTerminalPanel()
		return
	}
	_ = m.nvim.ExecLua(`
		for _, w in ipairs(vim.api.nvim_list_wins()) do
			local b = vim.api.nvim_win_get_buf(w)
			if vim.w[w].termocode_panel or vim.bo[b].buftype == 'terminal' then
				pcall(vim.api.nvim_win_close, w, true)
			end
		end
		local pb = vim.g.termocode_panel_buf
		if pb and vim.api.nvim_buf_is_valid(pb) then
			pcall(vim.api.nvim_buf_delete, pb, { force = true })
		end
		vim.g.termocode_panel_buf = nil
	`)
	m.hiddenTerminalTabs = m.terminalTabs
	m.hiddenTerminalActive = m.terminalActiveTab
	m.resetTerminalState()
}

// showHiddenTerminals reopens the panel with the tabs hideTerminalPanel
// kept, minus shells that exited meanwhile. It reports false when none
// is left (the caller then starts a fresh shell).
func (m *Model) showHiddenTerminals() bool {
	tabs, active := m.hiddenTerminalTabs, m.hiddenTerminalActive
	m.hiddenTerminalTabs = nil
	if m.nvim == nil || len(tabs) == 0 {
		return false
	}
	ids := make([]string, len(tabs))
	for i, t := range tabs {
		ids[i] = strconv.Itoa(t.BufID)
	}
	alive, _ := m.nvim.EvalLuaString(`local out = {}
		for _, b in ipairs({` + strings.Join(ids, ",") + `}) do
			if vim.api.nvim_buf_is_valid(b) and vim.bo[b].buftype == 'terminal' then
				table.insert(out, tostring(b))
			end
		end
		return table.concat(out, ',')`)
	live := map[int]bool{}
	for _, s := range strings.Split(alive, ",") {
		live[parseInt(s)] = true
	}
	var kept []terminalTab
	activeBuf := 0
	if active >= 0 && active < len(tabs) {
		activeBuf = tabs[active].BufID
	}
	newActive := 0
	for _, t := range tabs {
		if live[t.BufID] {
			if t.BufID == activeBuf {
				newActive = len(kept)
			}
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		return false
	}
	for i := range kept {
		for k := range kept[i].Splits {
			kept[i].Splits[k].WinID = 0 // every window was closed when hiding
		}
	}
	out, err := m.nvim.EvalLuaString(fmt.Sprintf(`
		local rows, buf = %d, %d
		vim.cmd('keepalt botright ' .. rows .. 'new')
		local scratch = vim.api.nvim_get_current_buf()
		local win = vim.api.nvim_get_current_win()
		vim.w[win].termocode_panel = true
		pcall(vim.api.nvim_win_set_buf, win, buf)
		if scratch ~= buf then pcall(vim.api.nvim_buf_delete, scratch, { force = true }) end
		pcall(function() vim.wo[win].number = false end)
		pcall(function() vim.wo[win].relativenumber = false end)
		pcall(function() vim.wo[win].signcolumn = 'no' end)
		pcall(function() vim.wo[win].foldcolumn = '0' end)
		pcall(function() vim.wo[win].statuscolumn = '' end)
		pcall(function() vim.wo[win].cursorline = false end)
		pcall(vim.cmd, 'startinsert')
		return tostring(win)
	`, m.integratedTerminalRows(), kept[newActive].BufID))
	if err != nil || parseInt(out) <= 0 {
		return false
	}
	m.terminalWinID = parseInt(out)
	m.terminalTabs = kept
	m.terminalActiveTab = newActive
	m.panelActive = panelKindTerminal
	m.terminalMinimized = false
	m.focus = FocusEditor
	m.termOpen = true
	m.terminalCwd = kept[newActive].Cwd
	m.terminalCwdAt = time.Now()
	m.invalidateTerminalProbeCache()
	return true
}
