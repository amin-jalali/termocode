package app

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/editor"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// covNvimModel is a full model (New) on a real headless nvim, sized
// w×h, in a private config dir, with /bin/sh as the shell. The nvim is
// closed when the test ends.
func covNvimModel(t *testing.T, w, h int) Model {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed; skipping integration test")
	}
	t.Setenv("SHELL", "/bin/sh")
	withConfigDir(t)
	c, err := nvim.New()
	if err != nil {
		t.Fatalf("nvim.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(w, h); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	m := New()
	m.nvim = c
	m.editor = editor.New(c)
	m, _ = m.update(tea.WindowSizeMsg{Width: w, Height: h})
	m.invalidateTerminalProbeCache()
	return m
}

// covHitCell scans the screen for the first cell where hit is true.
func covHitCell(m Model, hit func(x, y int) bool) (int, int, bool) {
	for y := 0; y < m.h; y++ {
		for x := 0; x < m.w; x++ {
			if hit(x, y) {
				return x, y, true
			}
		}
	}
	return 0, 0, false
}

// covBarCell finds the tab bar cell of kind (and tab index, -1 = any).
func covBarCell(t *testing.T, m Model, kind terminalTabBarHit, idx int) (int, int) {
	t.Helper()
	x, y, ok := covHitCell(m, func(x, y int) bool {
		k, i := m.terminalTabBarHitTest(x, y)
		return k == kind && (idx < 0 || i == idx)
	})
	if !ok {
		t.Fatalf("no tab bar cell of kind %v (tab %d)", kind, idx)
	}
	return x, y
}

func covClick(m Model, x, y int) (Model, tea.Cmd) {
	m, cmd := m.update(tea.MouseMsg{X: x, Y: y, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m, _ = m.update(tea.MouseMsg{X: x, Y: y, Type: tea.MouseRelease, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	return m, cmd
}

// covWaitLua polls a Lua expression until it returns want.
func covWaitLua(t *testing.T, m Model, lua, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, _ := m.nvim.EvalLuaString(lua)
		if strings.TrimSpace(got) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s = %q, want %q", lua, got, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCovCoreShouldPassToTerminal(t *testing.T) {
	cases := []struct {
		msg  tea.KeyMsg
		pass bool
	}{
		{tea.KeyMsg{Type: tea.KeyCtrlC}, true},
		{tea.KeyMsg{Type: tea.KeyCtrlD}, true},
		{tea.KeyMsg{Type: tea.KeyCtrlT}, false},
		{tea.KeyMsg{Type: tea.KeyF5}, false},
		{tea.KeyMsg{Type: tea.KeyF12}, false},
		{tea.KeyMsg{Type: tea.KeyShiftTab}, true},
		{tea.KeyMsg{Type: tea.KeyEnter}, true},
		{tea.KeyMsg{Type: tea.KeyEsc}, true},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ls")}, true},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b"), Alt: true}, false},
		{tea.KeyMsg{Type: tea.KeyCtrlBackslash}, false},
		{tea.KeyMsg{Type: tea.KeyCtrlLeft}, true},
		{tea.KeyMsg{Type: tea.KeyShiftEnd}, true},
		{tea.KeyMsg{Type: tea.KeyCtrlShiftUp}, false},
		{tea.KeyMsg{Type: tea.KeyInsert}, false},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fx")}, true},
		{tea.KeyMsg{Type: tea.KeyExtended, Name: "shift+f4"}, false},
		{tea.KeyMsg{Type: tea.KeyExtended, Name: "ctrl+f12"}, false},
		{tea.KeyMsg{Type: tea.KeyExtended, Name: "ctrl+fn"}, false},
		{tea.KeyMsg{Type: tea.KeyExtended, Name: "ctrl+shift+k"}, false},
	}
	for _, c := range cases {
		if got := shouldPassToTerminal(c.msg); got != c.pass {
			t.Errorf("shouldPassToTerminal(%q) = %v, want %v", c.msg.String(), got, c.pass)
		}
	}
}

func TestCovCoreParseHelpers(t *testing.T) {
	if w, b := parseWinBufPair("1001:7"); w != 1001 || b != 7 {
		t.Errorf("pair = %d %d", w, b)
	}
	if w, b := parseWinBufPair("nope"); w != 0 || b != 0 {
		t.Errorf("bad pair = %d %d", w, b)
	}
	if parseInt("42abc") != 42 || parseInt("x") != 0 {
		t.Error("parseInt")
	}
	for sh, g := range map[string]string{"bash": "$", "sh": "$", "zsh": "%", "fish": ">", "nu": "λ", "elvish": "λ"} {
		if shellGlyphFor(sh) != g {
			t.Errorf("glyph(%s) = %q", sh, shellGlyphFor(sh))
		}
	}
	t.Setenv("SHELL", "")
	if detectShellBasename() != "bash" {
		t.Error("no SHELL: want bash")
	}
	t.Setenv("SHELL", "/usr/bin/fish")
	if detectShellBasename() != "fish" {
		t.Error("SHELL fish")
	}
}

// Shell tabs by mouse: + adds one, a tab click shows it, the × closes
// it, maximize and minimize resize the panel.
func TestCovCoreTerminalTabsByMouse(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	m.toggleTerminalPanel()
	if !m.termOpen || len(m.terminalTabs) != 1 {
		t.Fatalf("open: %v tabs %d", m.termOpen, len(m.terminalTabs))
	}
	x, y := covBarCell(t, m, terminalHitNewTab, -1)
	m, _ = covClick(m, x, y)
	if len(m.terminalTabs) != 2 || m.terminalActiveTab != 1 {
		t.Fatalf("after +: %d tabs, active %d", len(m.terminalTabs), m.terminalActiveTab)
	}
	x, y = covBarCell(t, m, terminalHitTabActivate, 0)
	m, _ = covClick(m, x, y)
	if m.terminalActiveTab != 0 {
		t.Errorf("tab click: active %d", m.terminalActiveTab)
	}
	covWaitLua(t, m, `return tostring(vim.api.nvim_win_get_buf(`+itoa(m.terminalWinID)+`))`, itoa(m.terminalTabs[0].BufID))

	x, y = covBarCell(t, m, terminalHitMaximize, -1)
	m, _ = covClick(m, x, y)
	if m.terminalRows != m.h-8 || m.terminalMinimized {
		t.Errorf("maximize: rows %d (want %d), minimized %v", m.terminalRows, m.h-8, m.terminalMinimized)
	}
	if s := loadSession(); s.TerminalRows != m.h-8 {
		t.Errorf("maximized rows not saved: %d", s.TerminalRows)
	}
	x, y = covBarCell(t, m, terminalHitMinimize, -1)
	m, _ = covClick(m, x, y)
	if !m.terminalMinimized || m.integratedTerminalRows() != 0 {
		t.Errorf("minimize: %v, rows %d", m.terminalMinimized, m.integratedTerminalRows())
	}
	m.terminalMinimized = false
	m.applyLayout()

	gone := m.terminalTabs[1].BufID
	x, y = covBarCell(t, m, terminalHitTabClose, 1)
	m, _ = covClick(m, x, y)
	if len(m.terminalTabs) != 1 {
		t.Fatalf("× : %d tabs", len(m.terminalTabs))
	}
	covWaitLua(t, m, `return vim.api.nvim_buf_is_valid(`+itoa(gone)+`) and '1' or '0'`, "0")
	// Out of range: nothing happens.
	m.closeTerminalTab(5)
	m.switchTerminalTab(-1)
	if len(m.terminalTabs) != 1 {
		t.Error("an out-of-range close removed a tab")
	}
	// The last × closes the whole panel.
	x, y = covBarCell(t, m, terminalHitTabClose, 0)
	m, _ = covClick(m, x, y)
	if m.termOpen || len(m.terminalTabs) != 0 {
		t.Errorf("last ×: open %v, tabs %d", m.termOpen, len(m.terminalTabs))
	}
}

// Dragging the bar resizes the panel: down past the minimum minimizes,
// up past the top stops at the cap, the release saves the height.
func TestCovCoreTerminalDragResize(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	m.toggleTerminalPanel()
	x, y := covBarCell(t, m, terminalHitDrag, -1)
	press := tea.MouseMsg{X: x, Y: y, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	m, _ = m.update(press)
	if m.dragKind != dragTerminalTop {
		t.Fatalf("press on the bar: drag %v", m.dragKind)
	}
	motion := func(y int) {
		m, _ = m.update(tea.MouseMsg{X: x, Y: y, Type: tea.MouseMotion, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	}
	motion(m.h - 2) // far down: below the minimum
	if !m.terminalMinimized {
		t.Errorf("drag down: not minimized (rows %d)", m.terminalRows)
	}
	motion(m.h - 2) // again: stays minimized
	motion(1)       // far up: capped
	if m.terminalMinimized || m.terminalRows != m.h-8 {
		t.Errorf("drag up: minimized %v rows %d, want %d", m.terminalMinimized, m.terminalRows, m.h-8)
	}
	motion(m.h - 15)
	rows := m.terminalRows
	m, _ = m.update(tea.MouseMsg{X: x, Y: m.h - 15, Type: tea.MouseRelease, Action: tea.MouseActionRelease})
	if m.dragKind != dragNone || m.terminalBarHeld {
		t.Errorf("release: drag %v held %v", m.dragKind, m.terminalBarHeld)
	}
	if s := loadSession(); s.TerminalRows != rows {
		t.Errorf("saved rows %d, want %d", s.TerminalRows, rows)
	}
}

// The tab bar fits its width at every size, with many tabs.
func TestCovCoreTerminalTabBarWidths(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	m.toggleTerminalPanel()
	m.newTerminalTab()
	m.newTerminalTab()
	m.terminalTabs[1].Name = strings.Repeat("very-long-folder-name-", 4)
	m.terminalTabs[2].LastExit = 1
	for _, w := range []int{1, 5, 12, 20, 33, 50, 80, 119, 200} {
		bar := m.renderTerminalTabBar(w)
		if got := ansi.StringWidth(bar); got > w {
			t.Errorf("width %d: bar is %d wide", w, got)
		}
	}
	for _, sz := range [][2]int{{60, 20}, {40, 15}, {200, 60}} {
		m, _ = m.update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		covFitsScreen(t, m, "terminal open")
	}
}

// A shell that ends drops its tab; when the last one ends the panel
// state is reset.
func TestCovCoreTerminalShellsEnd(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	m.toggleTerminalPanel()
	m.newTerminalTab()
	first, second := m.terminalTabs[0].BufID, m.terminalTabs[1].BufID
	kill := func(buf int) {
		_ = m.nvim.ExecLua(`pcall(vim.api.nvim_buf_delete, ` + itoa(buf) + `, {force = true})`)
	}
	kill(second)
	m.terminalCwdAt = time.Time{}
	m.refreshTerminalCwd()
	if len(m.terminalTabs) != 1 || m.terminalTabs[0].BufID != first {
		t.Fatalf("after one shell ended: %+v", m.terminalTabs)
	}
	kill(first)
	m.terminalCwdAt = time.Time{}
	m.refreshTerminalCwd()
	if m.termOpen || len(m.terminalTabs) != 0 {
		t.Errorf("after every shell ended: open %v tabs %d", m.termOpen, len(m.terminalTabs))
	}
	// Throttled: a second refresh right away does nothing.
	m.termOpen = true
	m.terminalTabs = []terminalTab{{BufID: 99999}}
	m.terminalCwdAt = time.Now()
	m.refreshTerminalCwd()
	if len(m.terminalTabs) != 1 {
		t.Error("the throttle did not hold")
	}
	// The probe sees no terminal: the state is reset.
	m.invalidateTerminalProbeCache()
	if !m.syncTerminalState() || m.termOpen || m.terminalTabs != nil {
		t.Errorf("sync with a dead panel: open %v tabs %v", m.termOpen, m.terminalTabs)
	}
}

// "Open in Terminal" opens the panel and cds the shell; copy takes the
// shell's text; paste with an empty clipboard sends nothing.
func TestCovCoreTerminalOpenAtCopyPaste(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	if m.terminalCopyToClipboard() != 0 {
		t.Error("copy with no terminal")
	}
	m.terminalPasteFromClipboard()
	dir := t.TempDir()
	m.openIntegratedTerminalAt(dir)
	if !m.termOpen || m.terminalCwd != dir || m.activeTerminalTab().Cwd != dir {
		t.Fatalf("open at: open %v cwd %q", m.termOpen, m.terminalCwd)
	}
	buf := itoa(m.activeTerminalTab().BufID)
	_ = m.nvim.ExecLua(`vim.fn.chansend(vim.bo[` + buf + `].channel, "echo covcore-$((40+2))\n")`)
	covWaitLua(t, m, `return table.concat(vim.api.nvim_buf_get_lines(`+buf+`, 0, -1, false), '\n'):find('covcore%-42') and '1' or '0'`, "1")
	if n := m.terminalCopyToClipboard(); n < 1 {
		t.Errorf("copy took %d lines", n)
	}
	m.terminalPasteFromClipboard() // headless: the clipboard is empty
	// A second open-at reuses the panel.
	m.openIntegratedTerminalAt(t.TempDir())
	if len(m.terminalTabs) != 1 {
		t.Errorf("open-at made %d tabs", len(m.terminalTabs))
	}
}
