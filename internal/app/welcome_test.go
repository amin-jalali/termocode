package app

import (
	"strings"
	"testing"
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
	mustContain(t, out, "S T A R T")
	mustContain(t, out, "R E C E N T   F I L E S")
	mustContain(t, out, "W O R K S P A C E S")
	// Empty state copy: present when there are no recents/workspaces.
	mustContain(t, out, "No files yet")
	mustContain(t, out, "Open a file or start a new project")
	mustContain(t, out, "No recent workspaces")
	// Action card titles + subtitles + shortcut keycaps.
	for _, label := range []string{
		"Open file", "Command palette", "Find in files", "Open shell",
		"browse your filesystem", "run any action",
		"project-wide search", "integrated terminal",
		"^P", "F1", "F8", "^T",
	} {
		mustContain(t, out, label)
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

// TestWelcome_QuickActionHits ensures each action card emits at least
// one hit rectangle pointing at the right keymap action.
func TestWelcome_QuickActionHits(t *testing.T) {
	withConfigDir(t)
	hits := welcomeQuickActionHits(120, 36)
	if len(hits) < 4 {
		t.Fatalf("expected ≥4 quick-action hits (one per card), got %d", len(hits))
	}
	seen := map[int]bool{}
	for _, h := range hits {
		if h.ColEnd <= h.ColStart {
			t.Errorf("hit has empty column range: %+v", h)
		}
		seen[int(h.Action)] = true
	}
	if len(seen) < 4 {
		t.Errorf("expected hits for 4 distinct actions, got %d", len(seen))
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
	out0 := renderWelcome(120, 36, 0)
	out2 := renderWelcome(120, 36, 2)
	if out0 == "" || out2 == "" {
		t.Fatal("expected non-empty welcome output")
	}
	// Focusing different cards must produce a different rendered
	// frame (style escapes / color bytes flip even when plain text
	// stays the same).
	if out0 == out2 {
		t.Errorf("expected focus=0 and focus=2 to render differently")
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
