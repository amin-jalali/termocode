package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// Ctrl+C / Ctrl+X / Ctrl+V must work without an nvim clipboard provider
// (no xclip / wl-copy): copy goes through the unnamed register and Go,
// paste reads the OS clipboard or falls back to the unnamed register.
func TestEditorCopyCutPasteNvim(t *testing.T) {
	c, err := nvim.New()
	if err != nil {
		t.Skipf("nvim not available: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(80, 24); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	m := Model{nvim: c}
	lines := func() string {
		out, _ := c.EvalLuaString(`return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), '|')`)
		return out
	}

	// Whole-line copy (no selection) pastes as a new line above the cursor.
	_ = c.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, -1, false, {'a1b1c', 'beta'}); vim.api.nvim_win_set_cursor(0, {1, 0})`)
	m.editorCopy()
	if m.lastLineCopy != "a1b1c\n" {
		t.Fatalf("lastLineCopy = %q", m.lastLineCopy)
	}
	_ = c.ExecLua(`vim.cmd('stopinsert'); vim.api.nvim_win_set_cursor(0, {2, 2})`)
	m.editorPaste()
	if got := lines(); got != "a1b1c|a1b1c|beta" {
		t.Errorf("line paste: %q", got)
	}

	// Cut a visual selection, then paste it inside a line.
	_ = c.ExecLua(`vim.cmd('stopinsert'); vim.api.nvim_buf_set_lines(0, 0, -1, false, {'hello world'}); vim.api.nvim_win_set_cursor(0, {1, 0})`)
	_ = c.Input("v4l")
	_, _ = c.EvalLuaString(`return vim.api.nvim_get_mode().mode`) // let the input land
	m.editorCut()
	if got := lines(); got != " world" {
		t.Fatalf("after cut: %q", got)
	}
	if m.lastLineCopy != "" {
		t.Errorf("selection cut should clear lastLineCopy, got %q", m.lastLineCopy)
	}
	_ = c.ExecLua(`vim.cmd('stopinsert'); vim.api.nvim_win_set_cursor(0, {1, 6})`)
	m.editorPaste()
	if got := lines(); got != " worldhello" && got != " worlhellod" {
		t.Errorf("char paste: %q", got)
	}
}
