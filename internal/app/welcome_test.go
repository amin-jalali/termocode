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
	out := renderWelcome(120, 36, 0, true)
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
	// START tiles: title, subtitle and shortcut keycap.
	for _, label := range []string{
		"Open file", "Open workspace", "Clone repository", "Find in files", "Command palette", "New terminal",
		"Ctrl+P", "F1", "F8", "Ctrl+T", // UI review #3: one key notation
	} {
		mustContain(t, out, label)
	}
	for _, sub := range []string{"browse your filesystem", "run any action", "integrated terminal"} {
		mustContain(t, out, sub)
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

// TestWelcome_QuickActionHits: START is 6 tiles in 2 columns, one hit
// each, in reading order (left to right, top to bottom).
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
		if h.ColEnd-h.ColStart != w || w < 30 {
			t.Errorf("hit %d is %d cells wide, want the tile width %d", i, h.ColEnd-h.ColStart, w)
		}
		if i%2 == 1 && (h.Row != hits[i-1].Row || h.ColStart <= hits[i-1].ColEnd-1) {
			t.Errorf("hit %d is not right of hit %d: %+v %+v", i, i-1, h, hits[i-1])
		}
		if i >= 2 && h.Row <= hits[i-2].Row {
			t.Errorf("hit %d on row %d, not below hit %d", i, h.Row, i-2)
		}
	}
	want := []string{"Open file", "Command palette", "Find in files", "New terminal", "Open workspace", "Clone repository"}
	for i, a := range welcomeQuickActions() {
		if a.title != want[i] {
			t.Errorf("row %d = %q, want %q", i, a.title, want[i])
		}
	}
}

// TestWelcome_NoBoxesWhenShort: when the tiles do not fit, START is the
// one-line rows and no box is drawn.
func TestWelcome_NoBoxesWhenShort(t *testing.T) {
	withConfigDir(t)
	for _, sz := range [][2]int{{120, 18}, {64, 16}, {44, 14}} {
		out := ansi.Strip(renderWelcome(sz[0], sz[1], 0, true))
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
			out := renderWelcome(w, h, 0, true)
			if out == "" {
				t.Errorf("renderWelcome(%d, %d, true) returned empty", w, h)
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
	// Short pane: the one-line rows carry the cursor bar.
	for focus, title := range []string{"Open file", "Command palette", "Find in files"} {
		out := ansi.Strip(renderWelcome(120, 14, focus, true))
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
	out := ansi.Strip(renderWelcome(120, 36, 0, true))
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
