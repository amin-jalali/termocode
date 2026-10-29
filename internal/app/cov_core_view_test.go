package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// The 💡 chip of a preferred code action shows below the cursor with
// the live keys, stays inside the screen at every width, and is cut to
// a short title.
func TestCovCorePreferredActionHint(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	covEditorFile(t, &m, "a.go", "package a\n\nfunc A() {}\n")
	m.preferredCodeAction = &nvim.CodeAction{Title: "Add the missing import for the very long package name here\nsecond line"}
	screen := strings.Join(covScreen(m), "\n")
	if !strings.Contains(screen, "💡") || !strings.Contains(screen, "Apply: ") || !strings.Contains(screen, "…") {
		t.Fatalf("no hint chip:\n%s", screen)
	}
	if strings.Contains(screen, "second line") {
		t.Error("the hint shows the title's second line")
	}
	// An empty title reads "Quick Fix".
	m.preferredCodeAction = &nvim.CodeAction{Title: "  "}
	if !strings.Contains(strings.Join(covScreen(m), "\n"), "Quick Fix") {
		t.Error("empty title not shown as Quick Fix")
	}
	for _, w := range []int{40, 60, 90} {
		m, _ = m.update(tea.WindowSizeMsg{Width: w, Height: 20})
		covFitsScreen(t, m, "hint chip")
	}
	// No room in a 3-row window: nothing is drawn, nothing breaks.
	m, _ = m.update(tea.WindowSizeMsg{Width: 80, Height: 3})
	covFitsScreen(t, m, "hint chip, tiny window")
	// Zen mode hides it.
	m, _ = m.update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.zenMode = true
	if strings.Contains(strings.Join(covScreen(m), "\n"), "💡") {
		t.Error("hint shown in zen mode")
	}
}

func TestCovCoreHintKeys(t *testing.T) {
	for in, want := range map[string]string{
		"ctrl+shift+.": "Ctrl+Shift+.",
		"alt+enter":    "Alt+Enter",
		"cmd+k":        "Cmd+k",
		"meta+x":       "Cmd+x",
		" ":            "",
		"f5":           "f5",
	} {
		if got := prettyHintKey(in); got != want {
			t.Errorf("prettyHintKey(%q) = %q, want %q", in, got, want)
		}
	}
	km := keymap.Default()
	if got := keyForAction(km, keymap.ActionNone, "Fallback"); got != "Fallback" {
		t.Errorf("unbound action: %q", got)
	}
	// The shortest of several keys wins.
	if got := keyForAction(km, keymap.ActionToggleTerminal, "x"); got != "Ctrl+t" && got != "Alt+`" && got != "Ctrl+`" {
		t.Errorf("toggle terminal key: %q", got)
	}
}

// The launcher chip of an unpinned actions panel sits above the status bar.
func TestCovCoreActionsLauncherChip(t *testing.T) {
	withConfigDir(t)
	m := New()
	m, _ = m.update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.actionsOpen, m.actionsPinned = true, false
	x0, _, y, ok := m.actionsLauncherRect()
	if !ok {
		t.Fatal("no launcher rect")
	}
	rows := covScreen(m)
	if y >= len(rows) || ansi.StringWidth(rows[y]) <= x0 || strings.TrimSpace(ansi.Cut(rows[y], x0, m.w)) == "" {
		t.Errorf("launcher row %d empty: %q", y, rows[y])
	}
	covFitsScreen(t, m, "launcher")
	for _, w := range []int{30, 50} {
		m, _ = m.update(tea.WindowSizeMsg{Width: w, Height: 12})
		covFitsScreen(t, m, "launcher narrow")
	}
}

// The (disabled) overview ruler still marks the cursor, errors over
// warnings, at the right rows.
func TestCovCoreScrollbarRuler(t *testing.T) {
	withConfigDir(t)
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	m := New()
	if m.renderEditorScrollbar(0) != "" {
		t.Error("zero-height ruler")
	}
	m.scrollbarTotalLines = 100
	m.scrollbarMarks = []nvim.DiagnosticMark{
		{Line: 0, Severity: 2}, {Line: 0, Severity: 1},
		{Line: 50, Severity: 2}, {Line: 99, Severity: 1}, {Line: -5, Severity: 2}, {Line: 500, Severity: 1},
	}
	rows := strings.Split(m.renderEditorScrollbar(10), "\n")
	if len(rows) != 10 {
		t.Fatalf("%d rows", len(rows))
	}
	for i, r := range rows {
		if ansi.StringWidth(r) != 1 {
			t.Errorf("row %d is %d wide", i, ansi.StringWidth(r))
		}
	}
	if rows[0] == rows[3] || rows[5] == rows[3] || rows[9] == rows[3] {
		t.Errorf("marked rows look like the track: %q", rows)
	}
	if rows[0] != rows[9] {
		t.Errorf("error rows differ: %q vs %q", rows[0], rows[9])
	}
	if rows[5] == rows[0] {
		t.Error("a warning row looks like an error row")
	}
}

func TestCovCoreTextHelpers(t *testing.T) {
	for _, c := range []struct {
		in   string
		max  int
		want string
	}{
		{"/a/b/c/main.go", 0, ""},
		{"/a/b/c/main.go", 40, "/a/b/c/main.go"},
		{"/a/b/c/main.go", 8, "…main.go"},
		{"日本語のパス", 5, ""},
	} {
		got := leftEllipsize(c.in, c.max)
		if c.max == 5 {
			if ansi.StringWidth(got) > 5 || !strings.HasPrefix(got, "…") {
				t.Errorf("wide runes: %q", got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("leftEllipsize(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
	for _, w := range []int{5, 12, 20, 40} {
		h := sidebarHeader(w, "Explorer", "termocode-project")
		if got := ansi.StringWidth(h); got > w && w >= 12 {
			t.Errorf("sidebarHeader(%d) is %d wide: %q", w, got, ansi.Strip(h))
		}
		if !strings.Contains(ansi.Strip(h), "EXPLORER") {
			t.Errorf("sidebarHeader(%d) lost its label: %q", w, ansi.Strip(h))
		}
	}
	if !strings.Contains(ansi.Strip(sidebarHeader(30, "Search", "")), "SEARCH") {
		t.Error("header without a project")
	}
}

// The find bar floats in the editor and is cut at a narrow pane.
func TestCovCoreFindBarNarrowPane(t *testing.T) {
	m := covNvimModel(t, 120, 30)
	covEditorFile(t, &m, "f.txt", "hello\n")
	m.findOpen = true
	m.find.SetWidth(80)
	for _, w := range []int{120, 70, 45, 36} {
		m, _ = m.update(tea.WindowSizeMsg{Width: w, Height: 30})
		covFitsScreen(t, m, "find bar")
	}
	m.replaceOpen, m.findOpen = true, false
	m, _ = m.update(tea.WindowSizeMsg{Width: 90, Height: 30})
	covFitsScreen(t, m, "replace bar")
}
