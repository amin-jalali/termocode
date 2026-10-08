package app

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// TestPanelHostLifecycleNvim drives the panel split against a real nvim:
// Output opens a placeholder split, a terminal tab reuses it, closing the
// last terminal keeps the split for Output, closing Output closes it.
func TestPanelHostLifecycleNvim(t *testing.T) {
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
	winCount := func() int {
		out, _ := c.EvalLuaString(`return tostring(#vim.api.nvim_list_wins())`)
		return parseInt(out)
	}
	curIsPanel := func() bool {
		out, _ := c.EvalLuaString(`return vim.w.termocode_panel and '1' or ''`)
		return out == "1"
	}

	m.showPanelTab(panelKindOutput)
	if !m.termOpen || m.terminalWinID <= 0 || m.panelActive != panelKindOutput {
		t.Fatalf("Output did not open the panel: open=%v win=%d active=%d", m.termOpen, m.terminalWinID, m.panelActive)
	}
	if winCount() != 2 || curIsPanel() {
		t.Fatalf("want 2 windows with focus on the editor; wins=%d curIsPanel=%v", winCount(), curIsPanel())
	}
	if !m.probeTerminalBufferLive() {
		t.Fatal("placeholder split should count as a live panel")
	}
	// Calling again must not stack a second split.
	m.showPanelTab(panelKindOutput)
	if winCount() != 2 {
		t.Fatalf("second showPanelTab opened another split: wins=%d", winCount())
	}

	// "+" → a terminal tab lands in the same split.
	win := m.terminalWinID
	m.newTerminalTab()
	if len(m.terminalTabs) != 1 || m.panelActive != panelKindTerminal || m.terminalWinID != win || winCount() != 2 {
		t.Fatalf("terminal tab: tabs=%d active=%d win=%d/%d wins=%d", len(m.terminalTabs), m.panelActive, m.terminalWinID, win, winCount())
	}

	// Back to Output: focus leaves the shell window.
	m.cyclePanelTabs(1)
	if m.panelActive != panelKindOutput || curIsPanel() {
		t.Fatalf("cycle to Output: active=%d curIsPanel=%v", m.panelActive, curIsPanel())
	}

	// Closing the only terminal keeps the split for Output.
	m.closeTerminalTab(0)
	if !m.termOpen || len(m.terminalTabs) != 0 || winCount() != 2 {
		t.Fatalf("after closing last shell: open=%v tabs=%d wins=%d", m.termOpen, len(m.terminalTabs), winCount())
	}
	ft, _ := c.EvalLuaString(`return vim.bo[vim.api.nvim_win_get_buf(` + intToLua(m.terminalWinID) + `)].filetype`)
	if strings.TrimSpace(ft) != "termocode_panel" {
		t.Errorf("split should hold the placeholder, filetype=%q", ft)
	}

	// Closing Output (last tab) closes the panel.
	m.closePanelTab(panelKindOutput)
	if m.termOpen || winCount() != 1 {
		t.Fatalf("after closing Output: open=%v wins=%d", m.termOpen, winCount())
	}
}
