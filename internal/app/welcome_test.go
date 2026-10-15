package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

// TestWelcome_RendersWithEmptyRecents asserts the start page renders a
// non-empty string when there are no recents and surfaces the empty-state
// copy.
func TestWelcome_RendersWithEmptyRecents(t *testing.T) {
	withConfigDir(t)
	out := renderWelcome(120, 36, 0)
	if out == "" {
		t.Fatal("expected non-empty welcome output")
	}
	// Tagline (letter-spaced caps).
	mustContain(t, out, "T E R M I N A L")
	// Section headers.
	mustContain(t, out, "START")
	mustContain(t, out, "RECENT FILES")
	mustContain(t, out, "WORKSPACES")
	// Empty state copy: present when there are no recents/workspaces.
	mustContain(t, out, "No files yet")
	mustContain(t, out, "Open a file or start a new project")
	mustContain(t, out, "No recent workspaces")
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
	// Footer keycap + help hint.
	mustContain(t, out, "for help")
}

// TestWelcome_RecentsHits returns hit-zones for each rendered recent entry
// plus the "view all" sentinel when there are more than 3 entries.
func TestWelcome_RecentsHits(t *testing.T) {
	withConfigDir(t)
	pushRecent("/tmp/welcome-test-a.go")
	pushRecent("/tmp/welcome-test-b.go")
	pushRecent("/tmp/welcome-test-c.go")
	pushRecent("/tmp/welcome-test-d.go") // four entries → "view all" sentinel appears

	out, rHits, _, qHits := renderWelcomeFull(120, 36, 0)
	if out == "" {
		t.Fatal("expected non-empty welcome output")
	}
	if len(qHits) == 0 {
		t.Fatal("expected quick-action hits")
	}
	if len(rHits) == 0 {
		t.Fatal("expected at least one recent hit")
	}
	// Each entry now spans two rows (name + path) and both rows
	// register a hit so a click on either line opens the file.
	// 3 entries × 2 rows + 1 view-all sentinel = 7 hits.
	if len(rHits) != 7 {
		t.Errorf("got %d recent hits, want 7 (3 entries × 2 lines + view-all)", len(rHits))
	}
	hasSentinel := false
	for _, h := range rHits {
		if h.Path == welcomeSentinelViewAll {
			hasSentinel = true
		}
	}
	if !hasSentinel {
		t.Error("expected welcomeSentinelViewAll hit when len(recents) > 3")
	}
	// Up to 3 file entries rendered (most-recent-first → d, c, b).
	mustContain(t, out, "welcome-test-d.go")
	mustContain(t, out, "welcome-test-c.go")
	mustContain(t, out, "welcome-test-b.go")
	mustContain(t, out, "view all")
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

// TestWelcome_RecentEntryHasTwoLines verifies the new hierarchy:
// each recent entry renders the file name and path on separate
// rows so the visual hierarchy is name-primary / path-secondary.
func TestWelcome_RecentEntryHasTwoLines(t *testing.T) {
	withConfigDir(t)
	pushRecent("/tmp/welcome-2line-a.go")
	out := renderWelcome(120, 36, 0)
	if !strings.Contains(out, "welcome-2line-a.go") {
		t.Fatal("expected entry name in output")
	}
	if !strings.Contains(out, "/tmp") {
		t.Errorf("expected entry directory '/tmp' in output (path line missing)")
	}
}

func mustContain(t *testing.T, hay, needle string) {
	t.Helper()
	if !strings.Contains(hay, needle) {
		t.Errorf("output missing %q\n--- output ---\n%s", needle, hay)
	}
}
