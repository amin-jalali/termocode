package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/picker"
	"termocode/internal/toast"
)

// copyFileLineRef puts "path:line" (or "path:line:col") on the system
// clipboard so the user can paste a shareable reference into a chat /
// commit message / etc. Uses the existing `+` register for parity with
// the rest of the editor's copy paths.
func (m *Model) copyFileLineRef() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	path := m.editor.Path()
	if path == "" {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Warn, "No file to reference")
		return toastCmd
	}
	cur := m.editor.Cursor()
	ref := fmt.Sprintf("%s:%d:%d", path, cur.Line+1, cur.Col+1)
	// `let @+ = ...` writes directly to the + register (system clipboard
	// when nvim is built with clipboard support, which is the standard).
	_ = m.nvim.Command(fmt.Sprintf("let @+ = %q", ref))
	// OSC 52 forwards the reference to the user's *local* clipboard
	// even over SSH — without this the "Copy: File:Line" palette
	// action only populates the remote server's clipboard, which is
	// useless for pasting into a local chat / commit message.
	emitOSC52(ref)
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Copied", ref)
	return toastCmd
}

// openSnippetPicker shows every snippet defined for the current filetype
// in a fuzzy picker. Selection inserts the snippet body (via
// vim.snippet.expand) at the cursor, so users who don't remember a
// trigger can browse instead.
func (m *Model) openSnippetPicker() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	// Pull the current filetype's snippet table from the Lua side so we
	// don't have to keep the Go and Lua copies in sync — _snippets is a
	// global table (defined in snippets_lua.go), keyed by ft.
	out, err := m.nvim.EvalLuaString(`
		local ft = vim.bo.filetype
		local alias = { javascriptreact = 'javascript', typescriptreact = 'typescript', py = 'python' }
		ft = alias[ft] or ft
		local table_ = _snippets and _snippets[ft]
		if not table_ then return '' end
		local parts = {}
		for trigger, body in pairs(table_) do
			-- Encode each trigger and base64-ish escape the body so the
			-- "|" delimiter doesn't collide with any character in the
			-- body. Simplest-safe: replace "|" with "\\u007c", "\\n" with
			-- "\\u000a", before joining with "|".
			local escaped = body:gsub('\\', '\\\\'):gsub('|', '\\|'):gsub('\n', '\\n')
			table.insert(parts, trigger .. '|' .. escaped)
		end
		return table.concat(parts, '\n')
	`)
	if err != nil || out == "" {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No snippets for this filetype")
		return toastCmd
	}
	items := make([]picker.Item, 0, 16)
	idx := make(map[string]string, 16)
	for _, line := range splitLines(out) {
		trigger, body := splitOnUnescapedBar(line)
		if trigger == "" {
			continue
		}
		body = unescapeSnippetBody(body)
		preview := firstLine(body)
		if len(preview) > 50 {
			preview = preview[:50] + "…"
		}
		id := "snip-" + trigger
		items = append(items, picker.Item{
			ID:    id,
			Title: trigger,
			Hint:  preview,
		})
		idx[id] = body
	}
	if len(items) == 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No snippets for this filetype")
		return toastCmd
	}
	m.snippetPickerIndex = idx
	m.picker = picker.NewItems(" Snippets (Enter to insert) ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindSnippet
	return nil
}

// insertSnippetBody is the picker's selection handler — feed the body to
// vim.snippet.expand at the current cursor.
func (m *Model) insertSnippetBody(id string) tea.Cmd {
	body, ok := m.snippetPickerIndex[id]
	if !ok || m.nvim == nil {
		return nil
	}
	// Pass the body through Lua to call vim.snippet.expand. We can't
	// directly nvim.Input the body because tabstop expansion needs the
	// snippet API.
	luaSafe := luaEscape(body)
	_ = m.nvim.ExecLua(fmt.Sprintf(`
		if vim.snippet and vim.snippet.expand then
			vim.snippet.expand('%s')
		end
	`, luaSafe))
	m.focus = FocusEditor
	return nil
}

// tabsToSpaces / spacesToTabs convert leading whitespace across the buffer.
// The user picks the indent width via the existing :set tabstop value
// (default 4). Both operations preserve cursor position via winsaveview.
func (m *Model) tabsToSpaces() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	_ = m.nvim.ExecLua(`
		local view = vim.fn.winsaveview()
		vim.cmd('set expandtab')
		vim.cmd([[silent! retab]])
		vim.fn.winrestview(view)
	`)
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.Push(toast.Info, "Converted tabs → spaces")
	return toastCmd
}

func (m *Model) spacesToTabs() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	_ = m.nvim.ExecLua(`
		local view = vim.fn.winsaveview()
		vim.cmd('set noexpandtab')
		vim.cmd([[silent! retab!]])
		vim.fn.winrestview(view)
	`)
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.Push(toast.Info, "Converted spaces → tabs")
	return toastCmd
}

// trimBlankLines collapses runs of >1 consecutive blank lines down to a
// single blank line, buffer-wide. Useful after deleting code that leaves
// gaping vertical holes behind.
func (m *Model) trimBlankLines() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	_ = m.nvim.ExecLua(`
		local view = vim.fn.winsaveview()
		vim.cmd([[silent! %s/\(\n\s*\n\)\(\s*\n\)\+/\1/g]])
		vim.fn.winrestview(view)
	`)
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.Push(toast.Info, "Collapsed extra blank lines")
	return toastCmd
}

// Helpers for parsing the snippet table dump. Kept tiny (no external deps).

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// splitOnUnescapedBar splits a "key|value" pair where literal | in the value
// has been escaped as \|. We walk the string once, splitting at the first
// non-escaped pipe.
func splitOnUnescapedBar(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '|' && (i == 0 || s[i-1] != '\\') {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

// unescapeSnippetBody reverses the gsub escaping done in openSnippetPicker:
// \\n → newline, \\| → |, \\\\ → \\.
func unescapeSnippetBody(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				out = append(out, '\n')
				i++
				continue
			case '|':
				out = append(out, '|')
				i++
				continue
			case '\\':
				out = append(out, '\\')
				i++
				continue
			}
		}
		out = append(out, s[i])
	}
	return string(out)
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
