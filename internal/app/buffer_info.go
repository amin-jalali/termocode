package app

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/preview"
	"termocode/internal/toast"
)

// openBufferInfo shows a non-interactive preview overlay describing the
// active buffer: absolute path, on-disk size, line count, detected
// filetype, encoding, indent settings, and whether an LSP server is
// attached. Mirrors `:set` / `:filetype?` style output but condensed
// into a single read-only panel so it stays palette-friendly.
//
// Returns a toast cmd if no buffer is open (so the user knows why nothing
// happened); otherwise returns nil and the preview replaces the screen.
func (m *Model) openBufferInfo() tea.Cmd {
	path := m.editor.Path()
	if path == "" {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No active buffer")
		return toastCmd
	}

	// File size: read via os.Stat. Best-effort — an in-memory scratch
	// buffer with no on-disk counterpart still has a path (set via :file)
	// but Stat will return ENOENT. Surface that as "—" instead of an
	// alarming error.
	sizeStr := "—"
	if info, err := os.Stat(path); err == nil {
		sizeStr = fmt.Sprintf("%s (%d bytes)", humanBytes(info.Size()), info.Size())
	}

	// Line count comes from nvim — `vim.api.nvim_buf_line_count(0)` is
	// the source of truth for the in-memory buffer (handles unsaved
	// edits correctly, unlike counting newlines in the on-disk file).
	lineCount := 0
	if m.nvim != nil {
		if s, err := m.nvim.EvalLuaString(`return tostring(vim.api.nvim_buf_line_count(0))`); err == nil {
			_, _ = fmt.Sscanf(s, "%d", &lineCount)
		}
	}

	// LSP attachment: query vim.lsp.get_clients (Neovim 0.10+) and fall
	// back to vim.lsp.buf_get_clients on older versions. Empty list means
	// no server is running for this buffer. We render a comma-joined name
	// list so multi-server setups (e.g. pyright + ruff) report both.
	lspLine := "no"
	if m.nvim != nil {
		const luaQuery = `
local bufnr = vim.api.nvim_get_current_buf()
local clients
if vim.lsp.get_clients then
  clients = vim.lsp.get_clients({ bufnr = bufnr })
else
  clients = vim.lsp.buf_get_clients(bufnr)
end
local names = {}
for _, c in ipairs(clients) do
  table.insert(names, c.name)
end
return table.concat(names, ', ')
`
		if names, err := m.nvim.EvalLuaString(luaQuery); err == nil && names != "" {
			lspLine = "yes (" + names + ")"
		}
	}

	lang := m.editor.Lang()
	if lang == "" {
		lang = "—"
	}

	// Encoding + indent currently live as UI-only constants in the status
	// bar (UTF-8 / Spaces:4). Keeping the same values here avoids any
	// confusion about a divergent display until the editor surfaces real
	// per-buffer values.
	body := strings.Join([]string{
		"Path        " + path,
		"Size        " + sizeStr,
		"Lines       " + fmt.Sprintf("%d", lineCount),
		"Filetype    " + lang,
		"Encoding    UTF-8",
		"Indent      Spaces: 4",
		"LSP         " + lspLine,
	}, "\n")

	m.preview = preview.New(" Buffer Info ", body)
	m.preview.SetSize(m.w, m.h)
	m.previewOpen = true
	return nil
}

// humanBytes returns a short, human-friendly string for a file size,
// e.g. "1.2 KB" / "3.4 MB". Mirrors the units used elsewhere in the
// status bar so users see consistent figures across the UI.
func humanBytes(n int64) string {
	const (
		_  = iota
		KB = 1 << (10 * iota)
		MB
		GB
	)
	switch {
	case n >= GB:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(GB))
	case n >= MB:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(MB))
	case n >= KB:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(KB))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
