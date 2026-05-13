package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestRenderTerminalTabBarTwoTabs renders the tab bar with two tabs and
// verifies the output contains the expected glyphs (label, separator, both
// tab names with their shell icons, the close-x glyphs, the "+" button,
// the right-side controls). The exact column layout is sensitive to width
// — we just spot-check that all the key visual elements made it into the
// styled output.
func TestRenderTerminalTabBarTwoTabs(t *testing.T) {
	m := Model{
		w:           120,
		h:           40,
		showExp:     true,
		explorerWidth: defaultExplorerWidth,
		termOpen:    true,
		terminalRows: 10,
		terminalTabs: []terminalTab{
			{Name: "termocode", Cwd: "/root/termocode", Shell: "zsh", LastExit: 0},
			{Name: "scratch", Cwd: "/tmp", Shell: "bash", LastExit: 0},
		},
		terminalActiveTab: 0,
	}
	width := m.editorPaneWidth()
	out := m.renderTerminalTabBar(width)
	if w := lipgloss.Width(out); w != width {
		t.Fatalf("tab bar width = %d, want %d", w, width)
	}
	mustContain := []string{
		"Terminal", // label
		"│",        // separator after label
		"%",        // zsh icon (active tab)
		"$",        // bash icon (inactive tab)
		"termocode",
		"scratch",
		"×", // tab close glyphs + panel close glyph
		"+", // new-tab button
		"●", // status dot
		"↑", // maximize
		"−", // minimize
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("tab bar missing %q in output:\n%q", s, stripANSITerminal(out))
		}
	}
}

// TestRenderTerminalTabBarHitTestActiveTab verifies that the layout's Tab
// slots cover the visible cells of each tab so a click on the tab name
// hits terminalHitTabActivate, and that the close-x slot fires
// terminalHitTabClose.
func TestRenderTerminalTabBarHitTestActiveTab(t *testing.T) {
	m := Model{
		w:           120,
		h:           40,
		showExp:     true,
		explorerWidth: defaultExplorerWidth,
		termOpen:    true,
		terminalRows: 10,
		terminalTabs: []terminalTab{
			{Name: "termocode", Cwd: "/root/termocode", Shell: "zsh", LastExit: 0},
			{Name: "scratch", Cwd: "/tmp", Shell: "bash", LastExit: 0},
		},
		terminalActiveTab: 0,
	}
	width := m.editorPaneWidth()
	layout := m.computeTerminalTabBarLayout(width)
	if len(layout.Tabs) != 2 {
		t.Fatalf("expected 2 tab slots, got %d", len(layout.Tabs))
	}
	// Active tab must be the first slot.
	if !layout.Tabs[0].Active || layout.Tabs[1].Active {
		t.Errorf("active flag wrong: tabs=%+v", layout.Tabs)
	}
	// New-tab button must be present (we have plenty of width).
	if layout.NewStart < 0 || layout.NewEnd <= layout.NewStart {
		t.Errorf("new-tab button missing: %+v", layout)
	}
	// Right-side controls — three distinct columns.
	if layout.MaxStart >= layout.MinStart || layout.MinStart >= layout.CloseStart {
		t.Errorf("right-side controls overlap: max=%d min=%d close=%d",
			layout.MaxStart, layout.MinStart, layout.CloseStart)
	}
	// Hit-test the centre of the second tab → must be activate, idx=1.
	editorStart := m.w - width
	tab := layout.Tabs[1]
	absX := editorStart + (tab.Start+tab.End)/2
	absY := m.terminalTabBarRowAbsolute()
	hit, idx := m.terminalTabBarHitTest(absX, absY)
	if hit != terminalHitTabActivate || idx != 1 {
		t.Errorf("expected activate idx=1, got hit=%v idx=%d", hit, idx)
	}
	// Hit-test the close-x of the second tab.
	absX = editorStart + tab.CloseStart
	hit, idx = m.terminalTabBarHitTest(absX, absY)
	if hit != terminalHitTabClose || idx != 1 {
		t.Errorf("expected tab-close idx=1, got hit=%v idx=%d", hit, idx)
	}
	// Hit-test the panel × button.
	absX = editorStart + layout.CloseStart
	hit, _ = m.terminalTabBarHitTest(absX, absY)
	if hit != terminalHitClosePanel {
		t.Errorf("expected close-panel, got hit=%v", hit)
	}
	// Hit-test the new-tab button.
	absX = editorStart + layout.NewStart
	hit, _ = m.terminalTabBarHitTest(absX, absY)
	if hit != terminalHitNewTab {
		t.Errorf("expected new-tab, got hit=%v", hit)
	}
}

// TestSpliceTerminalTabBarRowCount verifies the splice produces exactly
// editorH rows and that the bar lands at the correct boundary index.
func TestSpliceTerminalTabBarRowCount(t *testing.T) {
	m := Model{
		w:           80,
		h:           24,
		showExp:     false,
		termOpen:    true,
		terminalRows: 6,
		terminalTabs: []terminalTab{
			{Name: "shell", Cwd: "/", Shell: "bash", LastExit: 0},
		},
		terminalActiveTab: 0,
	}
	editorH := 20
	// nvim renders the FULL editorH rows; the splice overpaints nvim's
	// split-separator row (one row above where nvim's terminal split
	// begins) with the bar. terminalRows = visible shell rows.
	rows := make([]string, editorH)
	for i := range rows {
		rows[i] = "row" + string(rune('A'+i%26))
	}
	in := strings.Join(rows, "\n")
	out := m.spliceTerminalTabBar(in, m.editorPaneWidth(), editorH)
	gotRows := strings.Split(out, "\n")
	if len(gotRows) != editorH {
		t.Fatalf("spliced rows = %d, want %d", len(gotRows), editorH)
	}
	// Tab bar should contain "Terminal" — pinpoint the row.
	tabBarIdx := -1
	for i, r := range gotRows {
		if strings.Contains(r, "Terminal") {
			tabBarIdx = i
			break
		}
	}
	expected := editorH - m.integratedTerminalRows() - 1
	if tabBarIdx != expected {
		t.Errorf("tab bar at row %d, want %d", tabBarIdx, expected)
	}
}

// TestSpliceTerminalTabBarMinimized verifies the minimized path replaces
// the bottom row of nvim's grid with the bar (no editor content lost),
// keeping the output at exactly editorH rows.
func TestSpliceTerminalTabBarMinimized(t *testing.T) {
	m := Model{
		w:                 80,
		h:                 24,
		showExp:           false,
		termOpen:          true,
		terminalRows:      6,
		terminalMinimized: true,
		terminalTabs: []terminalTab{
			{Name: "shell", Cwd: "/", Shell: "bash", LastExit: 0},
		},
		terminalActiveTab: 0,
	}
	editorH := 20
	// In minimized mode applyLayout sizes nvim to editorH rows.
	rows := make([]string, editorH)
	for i := range rows {
		rows[i] = "row" + string(rune('A'+i%26))
	}
	in := strings.Join(rows, "\n")
	out := m.spliceTerminalTabBar(in, m.editorPaneWidth(), editorH)
	gotRows := strings.Split(out, "\n")
	if len(gotRows) != editorH {
		t.Fatalf("minimized rows = %d, want %d", len(gotRows), editorH)
	}
	if !strings.Contains(gotRows[editorH-1], "Terminal") {
		t.Errorf("bar should be on the last row, got %q", stripANSITerminal(gotRows[editorH-1]))
	}
	// Ensure the second-to-last editor row is preserved (we replaced only
	// the very bottom row).
	if !strings.Contains(gotRows[editorH-2], "row") {
		t.Errorf("editor content lost from second-to-last row: %q", stripANSITerminal(gotRows[editorH-2]))
	}
}

// TestTerminalMinimizedRows verifies integratedTerminalRows returns 0
// when minimized, so the panel collapses to just the bar.
func TestTerminalMinimizedRows(t *testing.T) {
	m := Model{terminalRows: 10, terminalMinimized: true}
	if got := m.integratedTerminalRows(); got != 0 {
		t.Errorf("minimized rows = %d, want 0", got)
	}
	m.terminalMinimized = false
	if got := m.integratedTerminalRows(); got != 10 {
		t.Errorf("normal rows = %d, want 10", got)
	}
}

// TestShellGlyphFor confirms the shell-icon mapping for each known shell
// + the fallback.
func TestShellGlyphFor(t *testing.T) {
	cases := map[string]string{
		"bash":    "$",
		"sh":      "$",
		"zsh":     "%",
		"fish":    ">",
		"nu":      "λ",
		"unknown": "λ",
	}
	for shell, want := range cases {
		if got := shellGlyphFor(shell); got != want {
			t.Errorf("shellGlyphFor(%q) = %q, want %q", shell, got, want)
		}
	}
}

// stripANSITerminal removes ANSI escape sequences from s for readable test
// failure output. Borrowed from view-test patterns; stays inside this
// file to avoid bloating helpers_test.go.
func stripANSITerminal(s string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		if c == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		out.WriteByte(c)
		i++
	}
	return out.String()
}
