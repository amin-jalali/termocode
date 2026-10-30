package search

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func key(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func screen(m Model) []string { return strings.Split(ansi.Strip(m.View()), "\n") }

// cellOf returns the screen cell (x, y) where text first appears.
func cellOf(t *testing.T, m Model, text string) (int, int) {
	t.Helper()
	for y, line := range screen(m) {
		if i := strings.Index(line, text); i >= 0 {
			return ansi.StringWidth(line[:i]), y
		}
	}
	t.Fatalf("%q not on screen:\n%s", text, strings.Join(screen(m), "\n"))
	return 0, 0
}

// The whole round trip: type, the debounce tick runs the search in the
// root, results show, Enter opens the cursor row.
func TestOverlaySearchRoundTrip(t *testing.T) {
	noRg(t)
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "one needle\n")
	writeFile(t, dir, "sub/b.txt", "needle two\n")
	m := New()
	m.SetSize(100, 30)
	m.SetRoots([]string{dir})
	m = typeText(m, "needle")
	if !strings.Contains(strings.Join(screen(m), "\n"), "basic") {
		t.Fatal("no basic-mode hint without rg")
	}
	// A tick for an older version does nothing.
	if _, cmd := m.Update(tickMsg{version: m.queryVer - 1}); cmd != nil {
		t.Fatal("stale tick ran a search")
	}
	m, cmd := m.Update(tickMsg{version: m.queryVer})
	if cmd == nil || !m.Pending() {
		t.Fatal("tick did not start the search")
	}
	if !strings.Contains(strings.Join(screen(m), "\n"), "searching...") {
		t.Fatal("no searching hint")
	}
	msg := cmd()
	// Results for an older version are dropped.
	if old, _ := m.Update(resultsMsg{version: m.queryVer - 1}); !old.Pending() {
		t.Fatal("stale results were taken")
	}
	m, _ = m.Update(msg)
	if m.Pending() || len(m.results) != 2 {
		t.Fatalf("results %+v", m.results)
	}
	view := strings.Join(screen(m), "\n")
	if !strings.Contains(view, "2 results · basic") || !strings.Contains(view, "a.txt") ||
		!strings.Contains(view, filepath.Join("sub", "b.txt")) {
		t.Fatalf("view:\n%s", view)
	}
	m, _ = m.Update(key(tea.KeyCtrlN))
	m, _ = m.Update(key(tea.KeyDown)) // stays on the last row
	_, cmd = m.Update(key(tea.KeyEnter))
	sel, ok := cmd().(SelectMsg)
	if !ok || sel.Line != 1 || sel.Path != m.results[1].Path {
		t.Fatalf("select %+v", sel)
	}
	m, _ = m.Update(key(tea.KeyCtrlP))
	m, _ = m.Update(key(tea.KeyUp))
	if m.cursor != 0 {
		t.Fatalf("cursor %d", m.cursor)
	}
}

func TestOverlayNoRootsSearchesWorkingFolder(t *testing.T) {
	noRg(t)
	dir := t.TempDir()
	writeFile(t, dir, "w.txt", "cwdneedle\n")
	t.Chdir(dir)
	m := New()
	m.SetRoots([]string{"/somewhere"})
	m.SetRoots(nil)
	m = typeText(m, "cwdneedle")
	_, cmd := m.Update(tickMsg{version: m.queryVer})
	res := cmd().(resultsMsg)
	if len(res.results) != 1 || filepath.Base(res.results[0].Path) != "w.txt" {
		t.Fatalf("got %+v", res.results)
	}
	m, _ = m.Update(res)
	m.SetSize(100, 30)
	// Shown relative to the working folder.
	if _, y := cellOf(t, m, " w.txt"); y < 0 {
		t.Fatal("relative path missing")
	}
}

func TestOverlayErrorAndEmptyStates(t *testing.T) {
	m := New()
	m.SetSize(100, 30)
	m.rgAvailable = true
	if !strings.Contains(strings.Join(screen(m), "\n"), "type to search") {
		t.Fatal("no empty hint")
	}
	m.rgAvailable = false
	if !strings.Contains(strings.Join(screen(m), "\n"), "install rg") {
		t.Fatal("no install hint without rg")
	}
	m = typeText(m, "(x")
	m, _ = m.Update(resultsMsg{version: m.queryVer, err: errors.New("regex parse error")})
	if !strings.Contains(strings.Join(screen(m), "\n"), "regex parse error") {
		t.Fatal("error not shown")
	}
	m, _ = m.Update(resultsMsg{version: m.queryVer})
	if m.lastErr != "" || !strings.Contains(strings.Join(screen(m), "\n"), "no results") {
		t.Fatal("error not cleared on a clean empty run")
	}
	// Enter with no results does nothing.
	if _, cmd := m.Update(key(tea.KeyEnter)); cmd != nil {
		t.Fatal("enter on no results")
	}
	// Deleting the whole query clears results without a search.
	m, _ = m.Update(resultsMsg{version: m.queryVer, results: []Result{{Path: "a"}}})
	m, _ = m.Update(key(tea.KeyBackspace))
	m, cmd := m.Update(key(tea.KeyBackspace))
	if cmd != nil || m.input != "" || m.results != nil {
		t.Fatalf("input %q results %v", m.input, m.results)
	}
	if _, cmd := m.Update(key(tea.KeyBackspace)); cmd != nil {
		t.Fatal("backspace on empty field searched")
	}
	if m.View() == "" {
		t.Fatal("sized view is empty")
	}
	if (Model{}).View() != "" {
		t.Fatal("unsized view must be empty")
	}
}

func TestOverlayKeysCloseFocusAndSpace(t *testing.T) {
	m := New()
	for _, k := range []tea.KeyType{tea.KeyEsc, tea.KeyCtrlC} {
		_, cmd := m.Update(key(k))
		if _, ok := cmd().(CloseMsg); !ok {
			t.Fatalf("%v does not close", k)
		}
	}
	m, _ = m.Update(key(tea.KeyShiftTab))
	if m.focus != fieldContext {
		t.Fatalf("shift+tab from query = %d", m.focus)
	}
	m, cmd := m.Update(key(tea.KeySpace))
	if cmd != nil || m.contextLines != "" {
		t.Fatal("space typed into the context field")
	}
	m, _ = m.Update(key(tea.KeyShiftTab))
	m = typeText(m, "a")
	m, _ = m.Update(key(tea.KeySpace))
	m = typeText(m, "b")
	if m.exclude != "a b" {
		t.Fatalf("exclude %q", m.exclude)
	}
	if got, cmd := m.Update(altKey('z')); cmd != nil || got.exclude != "a b" {
		t.Fatal("alt+z changed something")
	}
	if _, cmd := m.Update(key(tea.KeyF5)); cmd != nil {
		t.Fatal("unknown key did something")
	}
	if _, cmd := m.Update(tea.FocusMsg{}); cmd != nil {
		t.Fatal("unknown message did something")
	}
	// A big number is capped.
	m.contextLines = "99"
	if m.Options().Context != maxContext {
		t.Fatalf("context %d", m.Options().Context)
	}
	m.SetMaxResults(-4)
	if m.Options().MaxResults != 0 {
		t.Fatal("negative cap must mean unlimited")
	}
	// Refresh with no query schedules nothing.
	empty := New()
	if empty.Refresh() != nil || empty.Pending() {
		t.Fatal("refresh with no query")
	}
}

func TestOverlayClicksOnScreenText(t *testing.T) {
	m := New()
	m.SetSize(100, 30)
	x, y := cellOf(t, m, "ab .*")
	m, cmd := m.HandleMouse(x, y, tea.MouseActionPress, tea.MouseButtonLeft)
	if !m.wholeWord || cmd != nil { // no query yet → no search
		t.Fatalf("ab click: word=%v cmd=%v", m.wholeWord, cmd)
	}
	// Click on the title text, not a toggle: nothing.
	tx, ty := cellOf(t, m, "Find in Files")
	if m2, _ := m.HandleMouse(tx, ty, tea.MouseActionPress, tea.MouseButtonLeft); m2.caseSensitive || m2.useRegex || !m2.wholeWord {
		t.Fatal("title click toggled something")
	}
	// Filter row: include, exclude, context, then the query row.
	for _, c := range []struct {
		label string
		want  field
	}{{"include", fieldInclude}, {"exclude", fieldExclude}, {"context", fieldContext}, {" > ", fieldQuery}} {
		x, y := cellOf(t, m, c.label)
		m, _ = m.HandleMouse(x+1, y, tea.MouseActionPress, tea.MouseButtonLeft)
		if m.focus != c.want {
			t.Fatalf("click on %q focus = %d, want %d", c.label, m.focus, c.want)
		}
	}
	// Right click and wheel are ignored; a click on the footer too.
	x, y = cellOf(t, m, "Aa")
	if m2, _ := m.HandleMouse(x, y, tea.MouseActionPress, tea.MouseButtonRight); m2.caseSensitive {
		t.Fatal("right click toggled")
	}
	fx, fy := cellOf(t, m, "enter open")
	if m2, cmd := m.HandleMouse(fx, fy, tea.MouseActionPress, tea.MouseButtonLeft); cmd != nil || m2.focus != m.focus {
		t.Fatal("footer click did something")
	}
	if _, cmd := (Model{}).HandleMouse(1, 1, tea.MouseActionPress, tea.MouseButtonLeft); cmd != nil {
		t.Fatal("unsized model took a click")
	}
}

func TestOverlayScrollsToKeepCursorBlockVisible(t *testing.T) {
	m := New()
	m.SetSize(80, 20) // small: only a few blocks fit
	m = typeText(m, "hit")
	var rs []Result
	for i := 0; i < 20; i++ {
		rs = append(rs, Result{
			Path: fmt.Sprintf("f%02d.go", i), Line: i + 1, Col: 1, Preview: "hit",
			Matches: []MatchRange{{0, 3}},
			Before:  []ContextLine{{i, "  before"}},
		})
	}
	m, _ = m.Update(resultsMsg{version: m.queryVer, results: rs, summary: Summary{Shown: 20, Total: 20}})
	for i := 0; i < 15; i++ {
		m, _ = m.Update(key(tea.KeyDown))
	}
	view := strings.Join(screen(m), "\n")
	if strings.Contains(view, "f00.go") || !strings.Contains(view, "f15.go") {
		t.Fatalf("did not scroll to the cursor:\n%s", view)
	}
	_, h := m.boxSize()
	if got := len(strings.Split(m.renderBox(m.boxSize()), "\n")); got != h {
		t.Fatalf("box height %d, want %d", got, h)
	}
}

func TestOverlayTinyScreenAndLongValues(t *testing.T) {
	m := New()
	m.SetSize(10, 5) // the box never grows past the screen
	m = typeText(m, strings.Repeat("q", 80))
	m.results = []Result{{Path: strings.Repeat("d/", 40) + "file.go", Line: 1, Col: 1,
		Preview: "\t  " + strings.Repeat("x", 100), After: []ContextLine{{2, strings.Repeat("y", 100)}}}}
	if w, h := m.boxSize(); w != 10 || h != 5 {
		t.Fatalf("tiny box %dx%d, want 10x5", w, h)
	}
	m.SetSize(40, 9)
	w, h := m.boxSize()
	if w != 40 || h != 9 {
		t.Fatalf("box %dx%d", w, h)
	}
	lines := strings.Split(m.renderBox(w, h), "\n")
	for i, l := range lines {
		if lipgloss.Width(l) != w {
			t.Fatalf("line %d width %d", i, lipgloss.Width(l))
		}
	}
	if !strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "…") {
		t.Fatal("long text not cut with …")
	}
	big := New()
	big.SetSize(300, 100)
	if w, h := big.boxSize(); w != 110 || h != 30 {
		t.Fatalf("big box %dx%d", w, h)
	}
	st := newStyles()
	if got := m.renderResults(st, 0, 10); len(got) != 0 {
		t.Fatal("zero rows must render nothing")
	}
	if got := m.renderField(st, "0123456789", "v", fieldQuery, 5); ansi.Strip(got) != "0123456789" {
		t.Fatalf("field with no room = %q", ansi.Strip(got))
	}
}

func TestDisplayPathMultiRoot(t *testing.T) {
	a := filepath.Join(t.TempDir(), "alpha")
	b := filepath.Join(t.TempDir(), "beta")
	m := New()
	m.SetRoots([]string{a, b})
	if got := m.displayPath(filepath.Join(b, "x", "y.go")); got != filepath.Join("beta", "x", "y.go") {
		t.Fatalf("got %q", got)
	}
	out := filepath.Join(string(os.PathSeparator), "elsewhere", "z.go")
	if got := m.displayPath(out); got != out {
		t.Fatalf("outside path changed: %q", got)
	}
}

func TestRenderHighlightedEdgeCases(t *testing.T) {
	plain := lipgloss.NewStyle()
	cases := []struct {
		name    string
		preview string
		matches []MatchRange
		maxW    int
		want    string
	}{
		{"no matches", "  abc", nil, 10, "abc"},
		{"leading space shifts ranges", "  foo bar", []MatchRange{{2, 5}}, 20, "foo bar"},
		{"overlapping ranges", "abcdef", []MatchRange{{0, 3}, {1, 4}}, 20, "abcdef"},
		{"range inside skipped space", "  abc", []MatchRange{{0, 1}, {2, 3}}, 20, "abc"},
		{"range past the end", "abc", []MatchRange{{5, 7}}, 20, "abc"},
		{"range end past the end", "abc", []MatchRange{{1, 9}}, 20, "abc"},
		{"cut before match", "aaaaaaaaaa foo", []MatchRange{{11, 14}}, 5, "aaaa…"},
		{"cut inside match", "foobarbaz", []MatchRange{{0, 9}}, 4, "foo…"},
		{"cut in tail", "foo and more text", []MatchRange{{0, 3}}, 8, "foo and…"},
	}
	for _, c := range cases {
		if got := ansi.Strip(renderHighlighted(c.preview, c.matches, plain, plain, c.maxW)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	if s, w := truncateBudget("abc", 0); s != "" || w != 0 {
		t.Fatal("zero budget")
	}
}
