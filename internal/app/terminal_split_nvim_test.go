package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"os/exec"
	"strings"
	"testing"
	"time"

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

func TestPanesFollowTheActiveTab(t *testing.T) {
	m := splitTestModel(t, 120, 30)
	base := nvimWinCount(t, m)
	m.splitTerminalPane(paneRight)
	splitBuf := m.terminalTabs[0].Splits[0].BufID
	if nvimWinCount(t, m) != base+1 {
		t.Fatalf("split: wins=%d want %d", nvimWinCount(t, m), base+1)
	}

	m.newTerminalTab() // a second tab: the first tab's extra pane is hidden
	if nvimWinCount(t, m) != base || len(m.terminalTabs) != 2 {
		t.Fatalf("new tab: wins=%d want %d tabs=%d", nvimWinCount(t, m), base, len(m.terminalTabs))
	}
	if m.terminalTabs[0].Splits[0].WinID != 0 {
		t.Error("a hidden pane must not keep a window id")
	}

	m.switchTerminalTab(0) // back: the pane is on screen again, same shell
	tab := m.terminalTabs[0]
	if nvimWinCount(t, m) != base+1 || tab.Splits[0].WinID <= 0 || tab.Splits[0].BufID != splitBuf {
		t.Fatalf("switch back: wins=%d pane=%+v", nvimWinCount(t, m), tab.Splits[0])
	}
	shown, _ := m.nvim.EvalLuaString(`return tostring(vim.api.nvim_win_get_buf(` + itoa(tab.Splits[0].WinID) + `))`)
	if parseInt(shown) != splitBuf {
		t.Errorf("the pane window shows buffer %s, want %d", shown, splitBuf)
	}

	m.closeTerminalTab(0) // closing the tab ends every one of its shells
	valid, _ := m.nvim.EvalLuaString(`return vim.api.nvim_buf_is_valid(` + itoa(splitBuf) + `) and 'yes' or 'no'`)
	if valid != "no" || nvimWinCount(t, m) != base || len(m.terminalTabs) != 1 {
		t.Errorf("close tab: split buf valid=%s wins=%d tabs=%d", valid, nvimWinCount(t, m), len(m.terminalTabs))
	}
}

func TestHideAndShowKeepsPanes(t *testing.T) {
	m := splitTestModel(t, 120, 30)
	m.splitTerminalPane(paneDown)
	splitBuf := m.terminalTabs[0].Splits[0].BufID
	base := nvimWinCount(t, m)

	m.hideTerminalPanel()
	if m.termOpen || nvimWinCount(t, m) != 1 {
		t.Fatalf("hidden: open=%v wins=%d", m.termOpen, nvimWinCount(t, m))
	}
	m.openTerminalPanel() // shows the hidden shells again
	m.reconcileTerminalPanes()
	tab := m.terminalTabs[0]
	if !m.termOpen || tab.paneCount() != 2 || tab.Splits[0].BufID != splitBuf || tab.Splits[0].WinID <= 0 {
		t.Fatalf("shown again: open=%v tab=%+v", m.termOpen, tab)
	}
	if nvimWinCount(t, m) != base {
		t.Errorf("wins = %d, want %d", nvimWinCount(t, m), base)
	}
}

func paneWinbar(t *testing.T, m *Model, win int) string {
	t.Helper()
	out, _ := m.nvim.EvalLuaString(`return vim.wo[` + itoa(win) + `].winbar`)
	return out
}

func TestPanesGetTitleBars(t *testing.T) {
	m := splitTestModel(t, 120, 30)
	if bar := paneWinbar(t, m, m.terminalWinID); bar != "" {
		t.Fatalf("a single pane has no title bar, got %q", bar)
	}
	m.splitTerminalPane(paneRight)
	tab := m.terminalTabs[0]
	for i, w := range []int{m.terminalWinID, tab.Splits[0].WinID} {
		want := " " + paneTitle(tab, i)
		if bar := paneWinbar(t, m, w); bar != want {
			t.Errorf("pane %d title %q, want %q", i, bar, want)
		}
	}
	hl, _ := m.nvim.EvalLuaString(`return vim.wo[` + itoa(m.terminalWinID) + `].winhighlight`)
	if hl == "" {
		t.Error("panes need a focus and a dim title colour (winhighlight)")
	}
	m.closeTerminalPane(1)
	if bar := paneWinbar(t, m, m.terminalWinID); bar != "" {
		t.Errorf("back to one pane the title bar goes away, got %q", bar)
	}
}

// waitNvim polls an nvim Lua expression until it returns "1" or the deadline.
func waitNvim(t *testing.T, m *Model, expr string) bool {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if out, _ := m.nvim.EvalLuaString(`return (` + expr + `) and '1' or ''`); out == "1" {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func TestExitedPaneStaysWithHint(t *testing.T) {
	m := splitTestModel(t, 120, 30)
	m.splitTerminalPane(paneRight)
	tab := m.terminalTabs[0]
	buf, win := tab.Splits[0].BufID, tab.Splits[0].WinID
	_ = m.nvim.ExecLua(`vim.fn.chansend(vim.bo[` + itoa(buf) + `].channel, "exit\n")`)
	if !waitNvim(t, m, `vim.api.nvim_buf_is_valid(`+itoa(buf)+`) and vim.b[`+itoa(buf)+`].termocode_exited`) {
		t.Fatal("the shell did not end, or its pane vanished")
	}
	if bar := paneWinbar(t, m, win); !strings.Contains(bar, "Enter to restart") || !strings.Contains(bar, "x to close") {
		t.Errorf("hint missing: winbar %q", bar)
	}
	m.refreshPaneExits()
	if !m.terminalTabs[0].paneExited(1) || m.terminalTabs[0].paneExited(0) {
		t.Errorf("exited flags: pane0=%v pane1=%v", m.terminalTabs[0].paneExited(0), m.terminalTabs[0].paneExited(1))
	}
	if nvimWinCount(t, m) != 3 {
		t.Errorf("the pane must stay on screen, wins=%d", nvimWinCount(t, m))
	}
	// The hint survives a repaint of the titles.
	m.paintPaneTitles()
	if bar := paneWinbar(t, m, win); !strings.Contains(bar, "Enter to restart") {
		t.Errorf("repaint lost the hint: %q", bar)
	}
}

func TestExitedPaneEnterRestartsAndXCloses(t *testing.T) {
	m := splitTestModel(t, 120, 30)
	m.splitTerminalPane(paneRight)
	endShell := func() (buf int) {
		tab := m.terminalTabs[0]
		buf = tab.Splits[0].BufID
		_ = m.nvim.ExecLua(`vim.fn.chansend(vim.bo[` + itoa(buf) + `].channel, "exit\n")`)
		if !waitNvim(t, m, `vim.b[`+itoa(buf)+`].termocode_exited`) {
			t.Fatal("the shell did not end")
		}
		m.refreshPaneExits()
		m.inPaneWin = true // nvim's current window is the pane
		return buf
	}
	// Other keys are not taken; a live pane is never touched.
	m.inPaneWin = true
	if m.exitedPaneKey(tea.KeyMsg{Type: tea.KeyEnter}) {
		t.Fatal("Enter on a live shell must go to the shell")
	}

	old := endShell()
	if m.exitedPaneKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}) {
		t.Error("only Enter and x are taken")
	}
	if !m.exitedPaneKey(tea.KeyMsg{Type: tea.KeyEnter}) {
		t.Fatal("Enter on an ended pane must restart it")
	}
	tab := m.terminalTabs[0]
	if tab.Splits[0].BufID == old || tab.paneExited(1) || tab.paneCount() != 2 {
		t.Fatalf("restart: tab=%+v (old buf %d)", tab, old)
	}
	if !waitNvim(t, m, `vim.bo[`+itoa(tab.Splits[0].BufID)+`].buftype == 'terminal'`) {
		t.Error("the new pane must be a terminal")
	}
	if bar := paneWinbar(t, m, tab.Splits[0].WinID); strings.Contains(bar, "Enter to restart") {
		t.Errorf("the hint must go after a restart, winbar %q", bar)
	}

	endShell()
	if !m.exitedPaneKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}) {
		t.Fatal("x on an ended pane must close it")
	}
	if m.terminalTabs[0].paneCount() != 1 || nvimWinCount(t, m) != 2 {
		t.Errorf("close: panes=%d wins=%d", m.terminalTabs[0].paneCount(), nvimWinCount(t, m))
	}
}

// panelRegion returns how many rows the panel takes: everything below the
// editor's status line down to the last pane's status line.
func panelRegion(t *testing.T, m *Model) int {
	t.Helper()
	tab := m.terminalTabs[0]
	wins := []int{}
	for i := 0; i < tab.paneCount(); i++ {
		wins = append(wins, m.paneWin(tab, i))
	}
	out, _ := m.nvim.EvalLuaString(`local bottom = 0
		for _, w in ipairs({` + intList(wins) + `}) do
			bottom = math.max(bottom, vim.api.nvim_win_get_position(w)[1] + vim.api.nvim_win_get_height(w) + 1)
		end
		local eb = 0
		for _, w in ipairs(vim.api.nvim_list_wins()) do
			if not vim.w[w].termocode_panel then
				eb = vim.api.nvim_win_get_position(w)[1] + vim.api.nvim_win_get_height(w) + 1
			end
		end
		return tostring(bottom - eb)`)
	return parseInt(out)
}

func TestResizeKeepsStackedPanelHeight(t *testing.T) {
	m := splitTestModel(t, 120, 40)
	m.splitTerminalPane(paneDown)
	if got := panelRegion(t, m); got != 13 {
		t.Fatalf("before resize the panel is %d rows, want 13 (12 + its status line)", got)
	}
	for _, rows := range []int{20, 9} {
		m.terminalRows = rows
		m.resizeTerminalSplit()
		if got := panelRegion(t, m); got != rows+1 {
			t.Errorf("terminalRows=%d: the panel is %d rows, want %d", rows, got, rows+1)
		}
	}
}

func TestSplitKeepsTheEditorHeight(t *testing.T) {
	m := splitTestModel(t, 120, 40)
	editorH := func() int {
		out, _ := m.nvim.EvalLuaString(`local h = 0
			for _, w in ipairs(vim.api.nvim_list_wins()) do
				if not vim.w[w].termocode_panel then h = vim.api.nvim_win_get_height(w) end
			end
			return tostring(h)`)
		return parseInt(out)
	}
	before := editorH()
	m.splitTerminalPane(paneDown)
	m.splitTerminalPane(paneRight)
	if got := editorH(); got != before {
		t.Errorf("editor height %d -> %d: splitting the terminal must not resize it", before, got)
	}
}

func TestGrowAndShrinkThePaneBesideAnother(t *testing.T) {
	m := splitTestModel(t, 120, 30)
	m.splitTerminalPane(paneRight)
	win := m.terminalTabs[0].Splits[0].WinID
	width := func() int {
		out, _ := m.nvim.EvalLuaString(`return tostring(vim.api.nvim_win_get_width(` + itoa(win) + `))`)
		return parseInt(out)
	}
	w0 := width()
	if !m.resizeTerminalPane(4) || width() != w0+4 {
		t.Fatalf("grow: width %d -> %d, want +4", w0, width())
	}
	m.resizeTerminalPane(-4)
	if width() != w0 {
		t.Errorf("shrink: width %d, want %d", width(), w0)
	}
	m.resizeTerminalPane(-500) // never below the smallest usable pane
	if width() < termPaneMinCols {
		t.Errorf("pane shrank to %d columns, minimum is %d", width(), termPaneMinCols)
	}
}

func TestGrowAStackedPaneKeepsThePanelHeight(t *testing.T) {
	m := splitTestModel(t, 120, 40)
	m.splitTerminalPane(paneDown)
	m.resizeTerminalPane(3)
	if got := panelRegion(t, m); got != 13 {
		t.Errorf("panel is %d rows after growing a pane, want 13", got)
	}
	if m.resizeTerminalPane(1) == false {
		t.Error("resize must report success")
	}
	single := splitTestModel(t, 120, 30)
	if single.resizeTerminalPane(2) {
		t.Error("a tab with one pane has nothing to resize")
	}
}

func TestPaletteSplitCommands(t *testing.T) {
	m := splitTestModel(t, 120, 30)
	for _, it := range termPanePaletteItems() {
		if !strings.HasPrefix(it.Title, "Terminal: ") {
			t.Errorf("%s: title %q must start with Terminal:", it.ID, it.Title)
		}
	}
	m.dispatchPaletteAction("terminal-split-right")
	m.dispatchPaletteAction("terminal-split-down")
	if m.terminalTabs[0].paneCount() != 3 {
		t.Fatalf("two splits give %d panes, want 3 (err=%q)", m.terminalTabs[0].paneCount(), m.err)
	}
	m.dispatchPaletteAction("terminal-focus-prev-pane")
	if m.terminalTabs[0].PaneFocus != 1 {
		t.Errorf("focus = %d, want 1", m.terminalTabs[0].PaneFocus)
	}
	m.dispatchPaletteAction("terminal-close-pane")
	if m.terminalTabs[0].paneCount() != 2 {
		t.Errorf("close pane left %d panes, want 2", m.terminalTabs[0].paneCount())
	}
	if m.dispatchPaletteAction("terminal-not-a-command") != nil {
		t.Error("an unknown command is not ours")
	}
}
