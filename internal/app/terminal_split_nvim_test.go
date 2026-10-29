package app

import (
	"os/exec"
	"testing"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// splitTestModel starts a real nvim of w x h cells with one terminal tab open.
func splitTestModel(t *testing.T, w, h int) *Model {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed; skipping integration test")
	}
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	c, err := nvim.New()
	if err != nil {
		t.Fatalf("nvim.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(w, h); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	m := &Model{w: w, h: h, nvim: c, terminalRows: 12, output: newOutputStore()}
	m.openTerminalPanel()
	if !m.termOpen || len(m.terminalTabs) != 1 {
		t.Fatalf("panel did not open: open=%v tabs=%d err=%q", m.termOpen, len(m.terminalTabs), m.err)
	}
	return m
}

func nvimWinCount(t *testing.T, m *Model) int {
	t.Helper()
	out, _ := m.nvim.EvalLuaString(`return tostring(#vim.api.nvim_list_wins())`)
	return parseInt(out)
}

func TestSplitRightMakesASecondShell(t *testing.T) {
	m := splitTestModel(t, 120, 30)
	before := nvimWinCount(t, m)
	m.splitTerminalPane(paneRight)
	tab := m.terminalTabs[0]
	if tab.paneCount() != 2 || tab.PaneFocus != 1 {
		t.Fatalf("panes=%d focus=%d err=%q", tab.paneCount(), tab.PaneFocus, m.err)
	}
	if nvimWinCount(t, m) != before+1 {
		t.Fatalf("windows %d -> %d, want one more", before, nvimWinCount(t, m))
	}
	if tab.Splits[0].BufID == tab.BufID || tab.Splits[0].WinID == m.terminalWinID {
		t.Fatalf("the new pane must have its own buffer and window: %+v", tab)
	}
	cur, _ := m.nvim.EvalLuaString(`return tostring(vim.api.nvim_get_current_win())`)
	if parseInt(cur) != tab.Splits[0].WinID {
		t.Errorf("focus is in window %s, want the new pane %d", cur, tab.Splits[0].WinID)
	}
	// The two panes sit side by side: same top row, new pane to the right.
	out, _ := m.nvim.EvalLuaString(`local a = vim.api.nvim_win_get_position(` + itoa(m.terminalWinID) + `)
		local b = vim.api.nvim_win_get_position(` + itoa(tab.Splits[0].WinID) + `)
		return (a[1] == b[1] and b[2] > a[2]) and 'beside' or 'other'`)
	if out != "beside" {
		t.Errorf("layout %q, want the new pane beside the first", out)
	}
}

func TestSplitDownStacksTheNewShell(t *testing.T) {
	m := splitTestModel(t, 120, 30)
	m.splitTerminalPane(paneDown)
	tab := m.terminalTabs[0]
	if tab.paneCount() != 2 || tab.Splits[0].Dir != paneDown || tab.Splits[0].Parent != 0 {
		t.Fatalf("tab = %+v err=%q", tab, m.err)
	}
	out, _ := m.nvim.EvalLuaString(`local a = vim.api.nvim_win_get_position(` + itoa(m.terminalWinID) + `)
		local b = vim.api.nvim_win_get_position(` + itoa(tab.Splits[0].WinID) + `)
		return (a[2] == b[2] and b[1] > a[1]) and 'stacked' or 'other'`)
	if out != "stacked" {
		t.Errorf("layout %q, want the new pane below the first", out)
	}
}

func TestSplitRefusedWhenTooNarrow(t *testing.T) {
	m := splitTestModel(t, 30, 30)
	before := nvimWinCount(t, m)
	cmd := m.splitTerminalPane(paneRight)
	if cmd == nil {
		t.Error("a refused split must tell the user (toast)")
	}
	if m.terminalTabs[0].paneCount() != 1 || nvimWinCount(t, m) != before {
		t.Fatalf("a refused split changed something: panes=%d wins=%d/%d", m.terminalTabs[0].paneCount(), nvimWinCount(t, m), before)
	}
}

func TestFocusFollowsPanes(t *testing.T) {
	m := splitTestModel(t, 120, 30)
	m.splitTerminalPane(paneRight)
	tab := &m.terminalTabs[0]
	curWin := func() int {
		out, _ := m.nvim.EvalLuaString(`return tostring(vim.api.nvim_get_current_win())`)
		return parseInt(out)
	}
	m.focusNextTerminalPane(1) // from pane 1 wraps to pane 0
	if tab.PaneFocus != 0 || curWin() != m.terminalWinID {
		t.Fatalf("after next: focus=%d win=%d want pane 0 / %d", tab.PaneFocus, curWin(), m.terminalWinID)
	}
	m.focusNextTerminalPane(-1) // back wraps to the last pane
	if tab.PaneFocus != 1 || curWin() != tab.Splits[0].WinID {
		t.Fatalf("after prev: focus=%d win=%d", tab.PaneFocus, curWin())
	}
	// A click makes nvim focus a window; the state message reports it.
	if !m.syncPaneFocus(m.terminalWinID) || tab.PaneFocus != 0 {
		t.Errorf("clicking pane 0: focus=%d", tab.PaneFocus)
	}
	if m.syncPaneFocus(999999) {
		t.Error("a window that is not a pane must not count as the terminal")
	}
}

func TestClosePaneKeepsTheOtherShell(t *testing.T) {
	m := splitTestModel(t, 120, 30)
	m.splitTerminalPane(paneRight)
	first := m.terminalTabs[0].BufID
	second := m.terminalTabs[0].Splits[0].BufID
	wins := nvimWinCount(t, m)

	m.closeTerminalPane(1) // the new pane
	tab := m.terminalTabs[0]
	if tab.paneCount() != 1 || tab.BufID != first || nvimWinCount(t, m) != wins-1 {
		t.Fatalf("after closing pane 1: panes=%d buf=%d wins=%d/%d", tab.paneCount(), tab.BufID, nvimWinCount(t, m), wins)
	}
	valid, _ := m.nvim.EvalLuaString(`return vim.api.nvim_buf_is_valid(` + itoa(second) + `) and 'yes' or 'no'`)
	if valid != "no" {
		t.Error("the closed pane's buffer (and so its shell) must be gone")
	}

	// Closing pane 0 of two promotes the other one.
	m.splitTerminalPane(paneDown)
	third := m.terminalTabs[0].Splits[0].BufID
	thirdWin := m.terminalTabs[0].Splits[0].WinID
	m.closeTerminalPane(0)
	tab = m.terminalTabs[0]
	if tab.paneCount() != 1 || tab.BufID != third || m.terminalWinID != thirdWin {
		t.Fatalf("after closing pane 0: panes=%d buf=%d (want %d) win=%d (want %d)", tab.paneCount(), tab.BufID, third, m.terminalWinID, thirdWin)
	}
	if !m.probeTerminalBufferLive() {
		t.Error("the panel must still be open on the remaining shell")
	}

	// The last pane closes the tab and the panel, as a tab does today.
	m.closeTerminalPane(0)
	if m.termOpen || len(m.terminalTabs) != 0 {
		t.Errorf("closing the last pane: open=%v tabs=%d", m.termOpen, len(m.terminalTabs))
	}
}
