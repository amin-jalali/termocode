package app

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// Spec (benchmarks 2026-10-15): the sticky-scroll lookup ("which func am
// I in?") looks back at most stickyLookback lines, so in a 1-million-line
// log it no longer blocks nvim for ~0.3 s every time the cursor moves. A
// func header within reach is still found, and a huge matching line comes
// back short.
func TestStickyContextBoundedOnBigFiles(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	withConfigDir(t)
	c, err := nvim.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(100, 30); err != nil {
		t.Fatal(err)
	}
	if err := c.ExecLua(`
		local lines = {}
		for i = 1, 1000000 do lines[i] = 'INFO request ' .. i .. ' handled path=/api status=200' end
		vim.api.nvim_buf_set_lines(0, 0, -1, false, lines)
		vim.cmd('normal! G')`); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	s, err := c.EvalLuaString(stickyContextLua)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 80*time.Millisecond || s != "" {
		t.Errorf("1M-line log: %v, %q; want < 80ms and no signature", d, s)
	}

	if err := c.ExecLua(`
		vim.api.nvim_buf_set_lines(0, 0, -1, false, { 'func Big() { ' .. string.rep('x', 5000), '  body', '  more' })
		vim.cmd('normal! G')`); err != nil {
		t.Fatal(err)
	}
	s, err = c.EvalLuaString(stickyContextLua)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s, "func Big()") || len(s) > 400 {
		t.Errorf("signature %d bytes, starts %q; want the func line, ≤ 400 bytes", len(s), s[:min(len(s), 20)])
	}
}
