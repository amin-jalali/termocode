package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

// TestWelcome_RendersWithEmptyRecents: with nothing recent the page is
// the wordmark, START and the hint bar; empty sections are not drawn.
func TestWelcome_RendersWithEmptyRecents(t *testing.T) {
	withConfigDir(t)
	old := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	defer func() { theme.CurrentIconMode = old; invalidateWelcome() }()
	out := renderWelcome(120, 36, 0)
	if out == "" {
		t.Fatal("expected non-empty welcome output")
	}
	// Tagline (letter-spaced caps).
	mustContain(t, out, "T E R M I N A L")
	mustContain(t, out, "START")
	for _, gone := range []string{"RECENT FILES", "WORKSPACES", "No files yet", "Open a file or start a new project",
		"No recent workspaces", "go 1.", "Docs:", "for help"} {
		if strings.Contains(out, gone) {
			t.Errorf("empty page still shows %q", gone)
		}
	}
	// START rows + shortcut keycaps; no subtitles.
	for _, label := range []string{
		"Open file", "Open workspace", "Clone repository", "Find in files", "Command palette", "New terminal",
		"Ctrl+P", "F1", "F8", "Ctrl+T", // UI review #3: one key notation
	} {
		mustContain(t, out, label)
	}
	for _, noise := range []string{"browse your filesystem", "run any action", "integrated terminal"} {
		if strings.Contains(out, noise) {
			t.Errorf("subtitle %q still on the page", noise)
		}
	}
	// The hint bar: how to move, open and reach every command.
	for _, hint := range []string{"move", "open", "commands", "v0."} {
		mustContain(t, out, hint)
	}
}

// TestWelcome_RecentsHits: up to 5 recent files, one hit each, then an
// "All N recent files…" row (Ctrl+R) when there are more.
func TestWelcome_RecentsHits(t *testing.T) {
	withConfigDir(t)
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		pushRecent("/tmp/welcome-test-" + n + ".go")
	}
	out, rHits, _, qHits := renderWelcomeFull(120, 40, 0)
	if len(qHits) == 0 {
		t.Fatal("expected quick-action hits")
	}
	if len(rHits) != 6 {
		t.Fatalf("got %d recent hits, want 6 (5 files + All…): %+v", len(rHits), rHits)
	}
	if rHits[5].Path != welcomeSentinelViewAll {
		t.Errorf("last recent hit is %q, want the All… row", rHits[5].Path)
	}
	for i := 1; i < len(rHits); i++ {
		if rHits[i].Row != rHits[i-1].Row+1 {
			t.Errorf("recent hit %d on row %d, want %d", i, rHits[i].Row, rHits[i-1].Row+1)
		}
	}
	for _, n := range []string{"g", "f", "e", "d", "c"} {
		mustContain(t, out, "welcome-test-"+n+".go")
	}
	mustContain(t, out, "All 7 recent files")
	mustContain(t, out, "Ctrl+R")
	if strings.Contains(out, "view all") || strings.Contains(out, "of 7") {
		t.Error("old link / counter still drawn")
	}
}

// TestWelcome_QuickActionHits: START is a list of 6 one-line rows, each
// one hit as wide as the content column, in reading order.
func TestWelcome_QuickActionHits(t *testing.T) {
	withConfigDir(t)
	hits := welcomeQuickActionHits(120, 36)
	if len(hits) != 6 {
		t.Fatalf("got %d quick-action hits, want 6 (one per row): %+v", len(hits), hits)
	}
	w := hits[0].ColEnd - hits[0].ColStart
	for i, h := range hits {
		if h.Index != i {
			t.Errorf("hit %d has index %d", i, h.Index)
		}
		if h.ColEnd-h.ColStart != w || w < 40 {
			t.Errorf("hit %d is %d cells wide, want the row width %d", i, h.ColEnd-h.ColStart, w)
		}
		if i > 0 && h.Row != hits[i-1].Row+1 {
			t.Errorf("hit %d on row %d, want %d (one row each, no gaps)", i, h.Row, hits[i-1].Row+1)
		}
	}
	want := []string{"Open file", "Open workspace", "Clone repository", "Find in files", "Command palette", "New terminal"}
	for i, a := range welcomeQuickActions() {
		if a.title != want[i] {
			t.Errorf("row %d = %q, want %q", i, a.title, want[i])
		}
	}
}

// TestWelcome_NoTiles: no boxes on the start page (rounded borders are
// for floating surfaces only).
func TestWelcome_NoTiles(t *testing.T) {
	withConfigDir(t)
	for _, sz := range [][2]int{{120, 36}, {64, 28}, {44, 22}} {
		out := ansi.Strip(renderWelcome(sz[0], sz[1], 0))
		for _, g := range []string{"╭", "╰", "╮", "╯"} {
			if strings.Contains(out, g) {
				t.Errorf("%v: border glyph %q on the start page", sz, g)
			}
		}
	}
}

// TestWelcome_NarrowDoesNotPanic exercises the layout path on tiny
// terminals.
func TestWelcome_NarrowDoesNotPanic(t *testing.T) {
	withConfigDir(t)
	for _, w := range []int{20, 40, 64, 100} {
		for _, h := range []int{6, 12, 30} {
			out := renderWelcome(w, h, 0)
			if out == "" {
				t.Errorf("renderWelcome(%d, %d) returned empty", w, h)
			}
		}
	}
}

// TestWelcome_FocusHighlightsCard confirms that passing different focus
// indices produces different rendered output — the focused card is
// tinted with a lighter card bg + accent-colored title so the active
// card visually pops. Color escapes are stripped by lipgloss in non-TTY
// test runs, so we rely on the rendered string differing rather than a
// specific glyph.
func TestWelcome_FocusHighlightsCard(t *testing.T) {
	withConfigDir(t)
	old := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	defer func() { theme.CurrentIconMode = old; invalidateWelcome() }()
	for focus, title := range []string{"Open file", "Open workspace", "Clone repository"} {
		out := ansi.Strip(renderWelcome(120, 36, focus))
		bars := 0
		for _, line := range strings.Split(out, "\n") {
			if !strings.Contains(line, "▎") {
				continue
			}
			bars++
			if !strings.Contains(line, "▎  "+title) {
				t.Errorf("focus %d: bar on the wrong row %q", focus, line)
			}
		}
		if bars != 1 {
			t.Errorf("focus %d: %d rows carry the cursor bar, want 1", focus, bars)
		}
	}
}

// TestWelcome_RecentEntryHasOneLine: a recent file is one row: glyph,
// name, directory, time.
func TestWelcome_RecentEntryHasOneLine(t *testing.T) {
	withConfigDir(t)
	pushRecent("/tmp/welcome-1line/a.go")
	out := ansi.Strip(renderWelcome(120, 36, 0))
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "a.go") {
			if !strings.Contains(line, "/tmp/welcome-1line") {
				t.Errorf("name and directory not on one row: %q", line)
			}
			return
		}
	}
	t.Fatal("recent file not on the page")
}

func mustContain(t *testing.T, hay, needle string) {
	t.Helper()
	if !strings.Contains(hay, needle) {
		t.Errorf("output missing %q\n--- output ---\n%s", needle, hay)
	}
}
