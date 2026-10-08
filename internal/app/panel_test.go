package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"termocode/internal/nvim"
)

// panelTestModel is a panel with one Output tab (active) and two shells.
func panelTestModel() Model {
	return Model{
		w:             120,
		h:             40,
		showExp:       true,
		explorerWidth: defaultExplorerWidth,
		termOpen:      true,
		terminalRows:  10,
		terminalTabs: []terminalTab{
			{Name: "termocode", Shell: "zsh"},
			{Name: "scratch", Shell: "bash"},
		},
		panelTabs:   []panelKind{panelKindOutput},
		panelActive: panelKindOutput,
		output:      newOutputStore(),
	}
}

func TestInsertPanelTabKeepsKindOrder(t *testing.T) {
	var tabs []panelKind
	tabs = insertPanelTab(tabs, panelKindTests)
	tabs = insertPanelTab(tabs, panelKindProblems)
	tabs = insertPanelTab(tabs, panelKindOutput)
	tabs = insertPanelTab(tabs, panelKindOutput) // duplicate → no-op
	want := []panelKind{panelKindProblems, panelKindOutput, panelKindTests}
	if len(tabs) != len(want) {
		t.Fatalf("tabs = %v, want %v", tabs, want)
	}
	for i := range want {
		if tabs[i] != want[i] {
			t.Fatalf("tabs = %v, want %v", tabs, want)
		}
	}
}

func TestCycleIndexWraps(t *testing.T) {
	cases := []struct{ cur, step, n, want int }{
		{0, 1, 3, 1}, {2, 1, 3, 0}, {0, -1, 3, 2}, {1, -1, 3, 0}, {0, 1, 0, 0},
	}
	for _, c := range cases {
		if got := cycleIndex(c.cur, c.step, c.n); got != c.want {
			t.Errorf("cycleIndex(%d,%d,%d) = %d, want %d", c.cur, c.step, c.n, got, c.want)
		}
	}
}

func TestPanelBarEntriesOrder(t *testing.T) {
	m := panelTestModel()
	e := m.panelBarEntries()
	if len(e) != 3 || e[0].Kind != panelKindOutput || e[1].Kind != panelKindTerminal || e[2].Index != 1 {
		t.Fatalf("entries = %+v", e)
	}
	if !m.panelEntryActive(e[0]) || m.panelEntryActive(e[1]) {
		t.Errorf("active flags wrong: %+v", e)
	}
}

func TestCyclePanelTabsCrossesKinds(t *testing.T) {
	m := panelTestModel()
	m.cyclePanelTabs(1) // Output → shell 0
	if m.panelActive != panelKindTerminal || m.terminalActiveTab != 0 {
		t.Fatalf("after +1: kind=%d term=%d", m.panelActive, m.terminalActiveTab)
	}
	m.cyclePanelTabs(1) // → shell 1
	if m.terminalActiveTab != 1 {
		t.Fatalf("after +2: term=%d", m.terminalActiveTab)
	}
	m.cyclePanelTabs(1) // wraps → Output
	if m.panelActive != panelKindOutput {
		t.Fatalf("after +3: kind=%d", m.panelActive)
	}
	m.cyclePanelTabs(-1) // back → shell 1
	if m.panelActive != panelKindTerminal || m.terminalActiveTab != 1 {
		t.Fatalf("after -1: kind=%d term=%d", m.panelActive, m.terminalActiveTab)
	}
}

func TestClosePanelTabFallsBackToTerminal(t *testing.T) {
	m := panelTestModel()
	m.terminalActiveTab = 1
	m.closePanelTab(panelKindOutput)
	if len(m.panelTabs) != 0 {
		t.Fatalf("panelTabs = %v", m.panelTabs)
	}
	if m.panelActive != panelKindTerminal || m.terminalActiveTab != 1 {
		t.Errorf("active = %d/%d, want terminal/1", m.panelActive, m.terminalActiveTab)
	}
	if !m.termOpen {
		t.Error("panel must stay open while terminal tabs remain")
	}
}

func TestTabBarWithOutputTab(t *testing.T) {
	m := panelTestModel()
	width := m.editorPaneWidth()
	out := m.renderTerminalTabBar(width)
	if w := lipgloss.Width(out); w != width {
		t.Fatalf("tab bar width = %d, want %d", w, width)
	}
	plain := stripANSITerminal(out)
	if !strings.HasPrefix(plain, " Output │  ≡ Output ×") {
		t.Errorf("bar should lead with the Output label + tab, got %q", plain)
	}
	if !strings.Contains(plain, "termocode") || !strings.Contains(plain, "scratch") {
		t.Errorf("terminal tabs missing: %q", plain)
	}
	if strings.Contains(plain, "●") {
		t.Errorf("status dot should be blank on a non-terminal tab: %q", plain)
	}

	layout := m.computeTerminalTabBarLayout(width)
	if len(layout.Tabs) != 3 || layout.Tabs[0].Kind != panelKindOutput || !layout.Tabs[0].Active {
		t.Fatalf("slots = %+v", layout.Tabs)
	}
	// Rendered text and slot geometry agree: the Output slot covers
	// "≡ Output ×".
	s := layout.Tabs[0]
	if got := strings.TrimSpace(string([]rune(plain)[s.Start:s.End])); got != "≡ Output ×" {
		t.Errorf("slot text = %q", got)
	}

	editorStart := m.w - width
	absY := m.terminalTabBarRowAbsolute()
	if hit, idx := m.terminalTabBarHitTest(editorStart+s.Start+3, absY); hit != panelHitTabActivate || idx != 0 {
		t.Errorf("Output tab click = %v/%d", hit, idx)
	}
	if hit, idx := m.terminalTabBarHitTest(editorStart+s.CloseStart, absY); hit != panelHitTabClose || idx != 0 {
		t.Errorf("Output × click = %v/%d", hit, idx)
	}
	t1 := layout.Tabs[1]
	if hit, idx := m.terminalTabBarHitTest(editorStart+(t1.Start+t1.End)/2, absY); hit != terminalHitTabActivate || idx != 0 {
		t.Errorf("shell tab click = %v/%d", hit, idx)
	}
}

func TestSpliceOverpaintsOutputContent(t *testing.T) {
	m := panelTestModel()
	m.output.Append("build", "first line\nsecond line")
	paneW := m.editorPaneWidth()
	editorH := 30
	body := strings.Repeat(strings.Repeat("x", paneW)+"\n", editorH-1) + strings.Repeat("x", paneW)
	out := m.spliceTerminalTabBar(body, paneW, editorH)
	rows := strings.Split(out, "\n")
	if len(rows) != editorH {
		t.Fatalf("rows = %d, want %d", len(rows), editorH)
	}
	barIdx := editorH - m.integratedTerminalRows() - 1
	for i, r := range rows[barIdx+1:] {
		if w := lipgloss.Width(r); w != paneW {
			t.Errorf("content row %d width = %d, want %d", i, w, paneW)
		}
		if strings.Contains(r, "x") {
			t.Errorf("content row %d leaks the nvim split: %q", i, stripANSITerminal(r))
		}
	}
	joined := stripANSITerminal(strings.Join(rows[barIdx+1:], "\n"))
	for _, s := range []string{"build", "Clear", "first line", "second line"} {
		if !strings.Contains(joined, s) {
			t.Errorf("content missing %q:\n%s", s, joined)
		}
	}
	// Rows above the bar keep the editor.
	if !strings.Contains(rows[0], "x") {
		t.Error("editor rows above the bar were overwritten")
	}
}

func TestPanelContentMouseAndKeys(t *testing.T) {
	m := panelTestModel()
	for i := 0; i < 50; i++ {
		m.output.Append("log", "line")
	}
	x, y, w, h, ok := m.panelContentRect()
	if !ok || h != m.integratedTerminalRows() || w != m.editorPaneWidth() {
		t.Fatalf("rect = %d,%d %dx%d ok=%v", x, y, w, h, ok)
	}
	m.output.window(h - 1) // as View would, so scroll clamps to the body

	// Wheel inside the content scrolls the Output view and is consumed.
	if _, handled := m.routePanelContentMouse(tea.MouseMsg{X: x + 2, Y: y + 2, Type: tea.MouseWheelUp}); !handled {
		t.Fatal("wheel over content not handled")
	}
	if c := m.output.chans["log"]; c.scroll != 3 {
		t.Errorf("scroll = %d, want 3", c.scroll)
	}
	// Click inside focuses the panel; Esc drops focus.
	m.routePanelContentMouse(tea.MouseMsg{X: x + 2, Y: y + 2, Type: tea.MouseLeft})
	if !m.panelFocused {
		t.Fatal("click should focus the panel")
	}
	if _, handled := m.routePanelKey(tea.KeyMsg{Type: tea.KeyEnd}); !handled || m.output.chans["log"].scroll != 0 {
		t.Error("End should jump to the tail")
	}
	if _, handled := m.routePanelKey(tea.KeyMsg{Type: tea.KeyEsc}); !handled || m.panelFocused {
		t.Error("Esc should drop panel focus")
	}
	// Click outside is not consumed and drops focus.
	m.panelFocused = true
	if _, handled := m.routePanelContentMouse(tea.MouseMsg{X: x + 2, Y: y - 5, Type: tea.MouseLeft}); handled {
		t.Error("click above the panel must not be consumed")
	}
	if m.panelFocused {
		t.Error("click outside should drop panel focus")
	}
	// Terminal tab active → content belongs to nvim.
	m.panelActive = panelKindTerminal
	if _, handled := m.routePanelContentMouse(tea.MouseMsg{X: x + 2, Y: y + 2, Type: tea.MouseLeft}); handled {
		t.Error("terminal content must reach nvim")
	}
}

func TestOutputHeaderClickSwitchesAndClears(t *testing.T) {
	m := panelTestModel()
	m.output.Append("a", "one")
	m.output.Append("bb", "two")
	chips, clearStart, _ := outputHeaderLayout(m.output.Channels(), m.editorPaneWidth())
	if len(chips) != 2 || clearStart < 0 {
		t.Fatalf("chips=%+v clear=%d", chips, clearStart)
	}
	spec := panelKinds[panelKindOutput]
	spec.Click(&m, 0, chips[1].Start)
	if m.output.Active() != "bb" {
		t.Errorf("active = %q, want bb", m.output.Active())
	}
	spec.Click(&m, 0, clearStart+1)
	if len(m.output.Lines("bb")) != 0 || len(m.output.Lines("a")) != 1 {
		t.Error("Clear should empty only the active channel")
	}
}

func TestOutputHeaderLayoutNarrow(t *testing.T) {
	chips, clearStart, _ := outputHeaderLayout([]string{"alpha", "beta"}, 12)
	if clearStart != 5 {
		t.Errorf("clearStart = %d, want 5", clearStart)
	}
	if len(chips) != 0 {
		t.Errorf("no chip fits before Clear, got %+v", chips)
	}
	if _, cs, _ := outputHeaderLayout(nil, 4); cs != -1 {
		t.Errorf("Clear should be dropped at width 4")
	}
}

func TestOutputStoreAppendScrollCap(t *testing.T) {
	s := newOutputStore()
	s.Append("", "a\nb\n", "c")
	if got := s.Lines(outputDefaultChannel); strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("lines = %v", got)
	}
	if s.Active() != outputDefaultChannel {
		t.Errorf("first channel should become active, got %q", s.Active())
	}
	s.Append("other", "x")
	if s.Active() != outputDefaultChannel {
		t.Error("later channels must not steal the active one")
	}
	// Scrolled-up view stays anchored while lines arrive.
	if w := s.window(2); strings.Join(w, ",") != "b,c" {
		t.Fatalf("tail window = %v", w)
	}
	s.Scroll(-1)
	if w := s.window(2); strings.Join(w, ",") != "a,b" {
		t.Fatalf("scrolled window = %v", w)
	}
	s.Append(outputDefaultChannel, "d")
	if w := s.window(2); strings.Join(w, ",") != "a,b" {
		t.Errorf("anchored window = %v", w)
	}
	// Cap drops the oldest lines.
	for i := 0; i < outputMaxLines+10; i++ {
		s.Append("big", "z")
	}
	if n := len(s.Lines("big")); n != outputMaxLines {
		t.Errorf("cap: %d lines, want %d", n, outputMaxLines)
	}
	// nil store is inert.
	var nilStore *outputStore
	nilStore.Append("x", "y")
	if nilStore.Channels() != nil {
		t.Error("nil store should be empty")
	}
}

func TestSanitizeOutputLine(t *testing.T) {
	got := sanitizeOutputLine("\x1b[31mred\x1b[0m\tx\ry\x07")
	if got != "red    xy" {
		t.Errorf("sanitize = %q", got)
	}
}

func TestNotifyOutputHandler(t *testing.T) {
	m := Model{output: newOutputStore()}
	cmd := m.dispatchNotify(nvim.NotifyMsg{Method: "termocode_output", Args: []any{"lsp", "up\nready"}})
	if cmd != nil {
		t.Error("output handler should not return a cmd")
	}
	if got := m.output.Lines("lsp"); strings.Join(got, ",") != "up,ready" {
		t.Errorf("lines = %v", got)
	}
	// Unknown methods are dropped quietly.
	if m.dispatchNotify(nvim.NotifyMsg{Method: "nope"}) != nil {
		t.Error("unknown method should yield nil")
	}
}

func TestRegisterNotifyHandlerRejectsDuplicate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("duplicate registration should panic")
		}
	}()
	registerNotifyHandler("termocode_output", func(*Model, []any) tea.Cmd { return nil })
}

func TestNotifyArgHelpers(t *testing.T) {
	args := []any{"s", int64(4), true, map[string]any{"k": "v"}, []byte("b")}
	if notifyArgString(args, 0) != "s" || notifyArgString(args, 4) != "b" || notifyArgString(args, 9) != "" {
		t.Error("notifyArgString")
	}
	if notifyArgInt(args, 1) != 4 || notifyArgInt(args, 0) != 0 {
		t.Error("notifyArgInt")
	}
	if !notifyArgBool(args, 2) || notifyArgBool(args, 0) {
		t.Error("notifyArgBool")
	}
	if notifyArgMap(args, 3)["k"] != "v" || notifyArgMap(args, 0) != nil {
		t.Error("notifyArgMap")
	}
	found := false
	for _, m := range notifyMethods() {
		if m == "termocode_output" {
			found = true
		}
	}
	if !found {
		t.Error("built-in termocode_output not registered")
	}
}

func TestRegisterPanelKindGuards(t *testing.T) {
	for name, fn := range map[string]func(){
		"terminal":  func() { registerPanelKind(panelKindTerminal, panelKindSpec{Render: renderOutputTab}) },
		"no render": func() { registerPanelKind(panelKindTests, panelKindSpec{}) },
		"duplicate": func() { registerPanelKind(panelKindOutput, panelKindSpec{Render: renderOutputTab}) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: expected panic", name)
				}
			}()
			fn()
		}()
	}
}
