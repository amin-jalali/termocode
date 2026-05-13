package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/recents"
	"termocode/internal/toast"
)

// formatDocumentCmd runs the active buffer's LSP formatter via the same
// vim.lsp.buf.format() that fires on save. Surfaces a "no formatter" toast
// when no LSP/server combo can format the buffer.
func (m *Model) formatDocumentCmd() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	out, _ := m.nvim.EvalLuaString(`
		local ok = pcall(function()
			vim.lsp.buf.format({ async = false, timeout_ms = 2000 })
		end)
		return ok and 'ok' or 'no_formatter'
	`)
	var toastCmd tea.Cmd
	if out == "ok" {
		m.toast, toastCmd = m.toast.Push(toast.Info, "Formatted document")
	} else {
		m.toast, toastCmd = m.toast.Push(toast.Warn, "No formatter available for this file")
	}
	return toastCmd
}

// openRecentFilePicker shows the recent-files list in the dedicated
// command-palette-style modal (internal/recents). Selecting an entry opens
// it. Empty list ⇒ friendly toast — no point opening a modal with nothing
// in it.
func (m *Model) openRecentFilePicker() tea.Cmd {
	entries := loadRecents()
	if len(entries) == 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No recent files yet")
		return toastCmd
	}
	items := make([]recents.Entry, 0, len(entries))
	for _, e := range entries {
		items = append(items, recents.Entry{Path: e.Path, OpenedAt: e.OpenedAt})
	}
	m.recents = recents.New(items, nowFunc())
	m.recents.SetSize(m.w, m.h)
	m.recentsOpen = true
	return m.recents.Init()
}

// toggleWordWrap flips nvim's `wrap` window-local option and surfaces a
// toast with title "Word wrap" + body "enabled"/"disabled". Returns the
// toast's tick cmd so the auto-dismiss timer actually fires (the
// previous version dropped it via `_ = toastCmd` and the toast stuck
// around forever).
func (m *Model) toggleWordWrap() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	cur, _ := m.nvim.EvalLuaString(`return vim.wo.wrap and 'on' or 'off'`)
	if cur == "on" {
		_ = m.nvim.Command("setlocal nowrap")
	} else {
		_ = m.nvim.Command("setlocal wrap")
	}
	body := "disabled"
	if cur == "off" {
		body = "enabled"
	}
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Word wrap", body)
	return toastCmd
}

// trimTrailingWhitespace strips trailing spaces/tabs from every line of the
// active buffer in one substitute, preserving cursor position.
func (m *Model) trimTrailingWhitespace() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	// `:keepjumps` so the substitute doesn't pollute the jumplist; `silent!`
	// swallows the "no match" message when the buffer is already clean.
	// `winsaveview()` / `winrestview()` keeps cursor + scroll position.
	_ = m.nvim.ExecLua(`
		local view = vim.fn.winsaveview()
		vim.cmd([[silent! keepjumps %s/\s\+$//e]])
		vim.fn.winrestview(view)
	`)
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.Push(toast.Info, "Trimmed trailing whitespace")
	return toastCmd
}

// revealInFileSystem opens the OS file manager at the current file's
// directory. Works on macOS (Finder), Linux (xdg-open), and Windows (start).
// Best-effort — silent toast on unsupported platforms.
func (m *Model) revealInFileSystem() tea.Cmd {
	path := m.editor.Path()
	if path == "" {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Warn, "No file to reveal")
		return toastCmd
	}
	dir := path
	// Strip the filename: filepath.Dir
	for i := len(dir) - 1; i >= 0; i-- {
		if dir[i] == '/' {
			dir = dir[:i]
			break
		}
	}
	if err := osOpen(dir); err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Reveal failed", err.Error())
		return toastCmd
	}
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Revealed", dir)
	return toastCmd
}

// nowFunc is a level of indirection so the recent-files hint timestamps can
// be tested without freezing the system clock. Production sets it to
// time.Now via init().
var nowFunc = defaultNowFunc

// osOpen is `xdg-open` / `open` / `start` depending on platform. Set in
// quick_actions_unix.go / quick_actions_other.go via build tags.
var osOpen = defaultOpen

var _ = fmt.Sprintf
