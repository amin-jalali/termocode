package app

import (
	"regexp"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/toast"
)

// gotoNextHunk / gotoPrevHunk move the cursor to the next/prev line that
// has a TermocodeGit* sign placed on it (i.e. an added/changed/deleted
// line per `git_signs_lua.go`). Falls back to a "no changes" toast when
// the buffer has no signs.
//
// Implementation lives in Lua so it can introspect nvim's sign state in
// one round-trip rather than fetching all signs into Go and walking them.
func (m *Model) gotoNextHunk() tea.Cmd {
	return m.jumpHunk(1)
}

func (m *Model) gotoPrevHunk() tea.Cmd {
	return m.jumpHunk(-1)
}

func (m *Model) jumpHunk(direction int) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	out, _ := m.nvim.EvalLuaString(`
		local placed = vim.fn.sign_getplaced(0, { group = 'TermocodeGitSigns' })[1]
		if not placed or #placed.signs == 0 then return 'none' end
		local lines = {}
		for _, s in ipairs(placed.signs) do
			-- Collapse runs of consecutive signs into a single hunk anchor
			-- (the first line of each hunk). Sort by line.
			table.insert(lines, s.lnum)
		end
		table.sort(lines)
		local hunks = {}
		for i, ln in ipairs(lines) do
			if i == 1 or ln ~= lines[i-1] + 1 then
				table.insert(hunks, ln)
			end
		end
		if #hunks == 0 then return 'none' end
		local cursor = vim.api.nvim_win_get_cursor(0)[1]
		local target
		if ` + boolStr(direction == 1) + ` then
			-- next: smallest hunk strictly > cursor; wrap to first.
			for _, ln in ipairs(hunks) do
				if ln > cursor then target = ln; break end
			end
			if not target then target = hunks[1] end
		else
			-- prev: largest hunk strictly < cursor; wrap to last.
			for i = #hunks, 1, -1 do
				if hunks[i] < cursor then target = hunks[i]; break end
			end
			if not target then target = hunks[#hunks] end
		end
		vim.fn.cursor(target, 1)
		return tostring(target)
	`)
	// Silently no-op when there are no hunks. The previous toast was
	// noisy because some terminals emit escape sequences during scroll
	// that Bubble Tea parses as Alt+] / Alt+[ and accidentally trigger
	// hunk-nav on every scroll tick.
	_ = out
	return nil
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// reloadBuffer triggers `:checktime` which forces nvim to compare the
// buffer with disk and reload if it changed. autoread is on, so this
// is rarely needed — but useful when the file changed in another tool
// and you don't want to wait for the next focus event.
func (m *Model) reloadBuffer() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	_ = m.nvim.Command("silent! checktime")
	_ = m.nvim.Command("silent! edit")
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.Push(toast.Info, "Buffer reloaded from disk")
	return toastCmd
}

// urlPattern matches http(s) and ssh-style URLs anywhere in a line.
// We deliberately keep it simple — anything more elaborate would have
// to handle gnarly edge cases (markdown link syntax, parens, …) and
// the false-negative rate of this regex is acceptable for a "click to
// open" feature.
var urlPattern = regexp.MustCompile(`https?://[^\s)\]'""\x60>]+`)

// openURLUnderCursor reads the current line, finds the first URL on it,
// and opens it in the system browser via the same xdg-open / open / start
// dispatcher as `revealInFileSystem`. No URL on the line ⇒ a "no URL"
// toast.
func (m *Model) openURLUnderCursor() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	line, _ := m.nvim.EvalLuaString(`return vim.api.nvim_get_current_line()`)
	url := urlPattern.FindString(line)
	if url == "" {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No URL on this line")
		return toastCmd
	}
	if err := osOpen(url); err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Open failed", err.Error())
		return toastCmd
	}
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Opened", url)
	return toastCmd
}
