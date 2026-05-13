package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/picker"
	"termocode/internal/toast"
)

// Bookmarks ride on top of nvim's built-in global marks (A-Z), which already
// persist across buffers within a session and naturally survive buffer
// reload. The benefits over a custom side-table are:
//
//   - free file/line restoration when the buffer reloads (autoread)
//   - free `'A` jump syntax for users who know it
//   - free shareability with anyone using nvim directly
//
// We use uppercase-mark slots A..Z so each bookmark is a global mark; lowercase
// marks (a..z) are buffer-local and would defeat the cross-file behaviour.

// BookmarksMsg is the fetched list of currently-set global marks, ready for
// the picker.
type BookmarksMsg struct {
	Items []picker.Item
	// Index maps picker IDs back to {file, line, col} so jumpToBookmark can
	// edit + cursor without re-querying nvim.
	Index map[string]workspaceSymbolTarget
}

// toggleBookmark sets the lowest unused global mark on the current line
// when called somewhere without an existing bookmark, OR removes the
// existing bookmark when one is already on the cursor's line. This gives
// a single command both "add" and "remove" semantics — VSCode-style.
func (m *Model) toggleBookmark() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	out, err := m.nvim.EvalLuaString(`
		local row = vim.api.nvim_win_get_cursor(0)[1]
		local file = vim.fn.expand('%:p')
		-- Look for an existing global mark on this exact line+file. If
		-- found, delete it (toggle off).
		for code = 65, 90 do
			local letter = string.char(code)
			local pos = vim.api.nvim_get_mark(letter, {})
			-- nvim_get_mark returns {row, col, buf, file}; an unset mark
			-- has row == 0.
			if pos[1] == row and pos[3] == vim.api.nvim_get_current_buf() then
				vim.api.nvim_del_mark(letter)
				return 'removed:' .. letter
			end
		end
		-- Otherwise pick the lowest unused letter and set it on the
		-- current cursor.
		for code = 65, 90 do
			local letter = string.char(code)
			local pos = vim.api.nvim_get_mark(letter, {})
			if pos[1] == 0 then
				vim.cmd('normal! m' .. letter)
				return 'added:' .. letter .. ':' .. file .. ':' .. row
			end
		end
		return 'full'
	`)
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	switch {
	case out == "full":
		m.toast, toastCmd = m.toast.Push(toast.Warn, "Bookmarks full (A-Z all set)")
	case strings.HasPrefix(out, "added:"):
		// added:A:/path:42
		parts := strings.SplitN(out, ":", 4)
		if len(parts) >= 2 {
			m.toast, toastCmd = m.toast.Push(toast.Info, "Bookmark "+parts[1]+" set")
		}
	case strings.HasPrefix(out, "removed:"):
		letter := strings.TrimPrefix(out, "removed:")
		m.toast, toastCmd = m.toast.Push(toast.Info, "Bookmark "+letter+" cleared")
	}
	return toastCmd
}

// fetchBookmarksCmd returns a tea.Cmd that produces a BookmarksMsg containing
// every currently-set global mark with its file path + line.
func (m Model) fetchBookmarksCmd() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	c := m.nvim
	return func() tea.Msg {
		entries, err := c.Bookmarks()
		if err != nil {
			return ErrMsg{Err: err}
		}
		items := make([]picker.Item, 0, len(entries))
		index := make(map[string]workspaceSymbolTarget, len(entries))
		for _, e := range entries {
			id := "bm-" + e.Letter
			rel := e.File
			if cwd, err := filepath.Abs("."); err == nil {
				if r, err2 := filepath.Rel(cwd, e.File); err2 == nil && !strings.HasPrefix(r, "..") {
					rel = r
				}
			}
			items = append(items, picker.Item{
				ID:    id,
				Title: fmt.Sprintf("%s   %s", e.Letter, rel),
				Hint:  fmt.Sprintf("L%d", e.Line),
			})
			index[id] = workspaceSymbolTarget{File: e.File, Line: e.Line, Col: e.Col}
		}
		return BookmarksMsg{Items: items, Index: index}
	}
}

// applyBookmarksMsg installs the picker. Empty list ⇒ helpful toast.
func (m *Model) applyBookmarksMsg(msg BookmarksMsg) tea.Cmd {
	if len(msg.Items) == 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No bookmarks set yet (Toggle Bookmark to add)")
		return toastCmd
	}
	m.workspaceSymIndex = msg.Index // reuse the same target shape + jump path
	m.picker = picker.NewItems(" Bookmarks ", msg.Items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindWorkspaceSymbols
	return nil
}

// clearAllBookmarks deletes every set global mark A-Z.
func (m *Model) clearAllBookmarks() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	_ = m.nvim.ExecLua(`
		for code = 65, 90 do
			local letter = string.char(code)
			pcall(vim.api.nvim_del_mark, letter)
		end
	`)
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.Push(toast.Info, "All bookmarks cleared")
	return toastCmd
}

