package app

import (
	"os/exec"
	"testing"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// After the grid shrinks below the panel height, nvim squeezes the panel
// window and does not give the rows back when the grid grows again. The
// panel height must be set again on every grid resize, or the shell ends
// up drawn above the panel's tab bar (found by the e2e resize test).
func TestTerminalSplitHeightAfterShrinkAndGrowNvim(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed; skipping integration test")
	}
	t.Setenv("SHELL", "/bin/sh")
	c, err := nvim.New()
	if err != nil {
		t.Fatalf("nvim.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(100, 30); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	m := Model{w: 100, h: 30, nvim: c, terminalRows: 8, output: newOutputStore()}
	m.showPanelTab(panelKindOutput)
	if !m.termOpen || m.terminalWinID <= 0 {
		t.Fatal("panel did not open")
	}
	height := func() string {
		s, _ := c.EvalLuaStringArgs(`return tostring(vim.api.nvim_win_get_height(...))`, m.terminalWinID)
		return s
	}
	m.reapplyTerminalSplit()
	if h := height(); h != "8" {
		t.Fatalf("panel height %s, want 8", h)
	}
	// Shrink: nvim squeezes the panel.
	_ = c.Resize(40, 6)
	m.reapplyTerminalSplit()
	// Grow back: the panel must be 8 rows again.
	_ = c.Resize(100, 30)
	m.reapplyTerminalSplit()
	if h := height(); h != "8" {
		t.Fatalf("panel height after shrink+grow = %s, want 8", h)
	}
}
