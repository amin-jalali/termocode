package app

import (
	"os/exec"
	"strconv"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// TestExitInOneTerminalKeepsOthers: with two shell tabs open, `exit` in
// the shown one ends only that shell. The panel stays open on the other
// tab and its shell keeps running. It used to close the panel window
// with the dead buffer, so termocode saw no terminal on screen and
// dropped every tab.
func TestExitInOneTerminalKeepsOthers(t *testing.T) {
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

	m.openTerminalPanel()
	m.newTerminalTab()
	if len(m.terminalTabs) != 2 {
		t.Fatalf("want 2 terminal tabs, got %d", len(m.terminalTabs))
	}
	keep, gone := m.terminalTabs[0].BufID, m.terminalTabs[1].BufID
	lua := func(s string) string { out, _ := c.EvalLuaString(s); return out }
	b := func(n int) string { return strconv.Itoa(n) }

	// Type `exit` into the shown (second) shell.
	lua(`vim.fn.chansend(vim.bo[` + b(gone) + `].channel, "exit\n") return ''`)
	deadline := time.Now().Add(5 * time.Second)
	for lua(`return vim.api.nvim_buf_is_valid(`+b(gone)+`) and '1' or ''`) == "1" {
		if time.Now().After(deadline) {
			t.Fatal("the exited shell's buffer was never closed")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// What the next render does.
	m.invalidateTerminalProbeCache()
	m.syncTerminalState()
	m.terminalCwdAt = time.Time{}
	m.refreshTerminalCwd()

	if !m.termOpen {
		t.Fatal("panel closed: the other shell tab was dropped")
	}
	if len(m.terminalTabs) != 1 || m.terminalTabs[0].BufID != keep {
		t.Fatalf("tabs = %+v, want only buf %d", m.terminalTabs, keep)
	}
	if lua(`return vim.fn.jobwait({vim.bo[`+b(keep)+`].channel}, 0)[1] == -1 and '1' or ''`) != "1" {
		t.Error("the other shell is no longer running")
	}
	if lua(`return vim.fn.bufwinid(`+b(keep)+`) ~= -1 and '1' or ''`) != "1" {
		t.Error("the other shell is not on screen")
	}
}

// TestPanelDragBarWaitsForNvim: dragging the bottom panel's bar moves it
// only together with nvim's split. The bar used to jump to the pointer
// at once while nvim's content followed a frame later, so for a moment
// the bar sat over the wrong rows (editor lines below it).
func TestPanelDragBarWaitsForNvim(t *testing.T) {
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
	m.openTerminalPanel()
	bar := m.terminalTabBarRowAbsolute()

	m.dragKind = dragTerminalTop
	next, _ := m.handleMouse(tea.MouseMsg{X: 50, Y: bar - 3, Type: tea.MouseMotion})
	m = next
	if m.integratedTerminalRows() != 11 {
		t.Fatalf("drag did not grow the panel: rows=%d", m.integratedTerminalRows())
	}
	if got := m.terminalTabBarRowAbsolute(); got != bar {
		t.Errorf("bar moved to row %d before nvim redrew; want it at %d", got, bar)
	}

	next, _, _ = m.handleNvimMsg(nvim.RedrawMsg{Events: [][]any{{"flush"}}})
	m = next
	if got := m.terminalTabBarRowAbsolute(); got != bar-3 {
		t.Errorf("after nvim redrew: bar at row %d, want %d", got, bar-3)
	}
}
