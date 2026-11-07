package tabbar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// GITX-39: each editor tab shows the git status letter of its file after
// the name (M modified, A added, U untracked, D deleted), like the
// explorer does.
func TestTabShowsGitStatusBadge(t *testing.T) {
	m := New()
	m.SetBuffers([]nvim.BufferInfo{
		{ID: 1, Path: "/repo/changed.go"},
		{ID: 2, Path: "/repo/new.go"},
		{ID: 3, Path: "/repo/clean.go"},
		{ID: 4, Path: "/repo/staged.go"},
	}, 1)
	m.SetWidth(200)
	m.SetGitStatus(map[string]string{
		"/repo/changed.go": "M",
		"/repo/new.go":     "U",
		"/repo/staged.go":  "A",
	})
	out := ansi.Strip(m.View())
	for _, want := range []string{"changed.go M", "new.go U", "staged.go A"} {
		if !strings.Contains(out, want) {
			t.Errorf("tab bar %q lacks %q", out, want)
		}
	}
	if strings.Contains(out, "clean.go M") || strings.Contains(out, "clean.go U") || strings.Contains(out, "clean.go A") {
		t.Errorf("clean file shows a badge: %q", out)
	}
}

// An ignored file gets no badge; the tab looks like a clean one.
func TestTabIgnoredFileHasNoBadge(t *testing.T) {
	m := New()
	m.SetBuffers([]nvim.BufferInfo{{ID: 1, Path: "/repo/build.log"}}, 1)
	m.SetWidth(80)
	plain := ansi.Strip(m.View())
	m.SetGitStatus(map[string]string{"/repo/build.log": "I"})
	if got := ansi.Strip(m.View()); got != plain {
		t.Fatalf("ignored file changed the tab:\n%q\n%q", plain, got)
	}
}

// The bar is cached: a new git status must redraw it, and the click
// targets must follow the wider tab (the close glyph moves right).
func TestTabGitBadgeRedrawsAndMovesTargets(t *testing.T) {
	m := New()
	m.SetBuffers([]nvim.BufferInfo{{ID: 1, Path: "/repo/a.go"}, {ID: 2, Path: "/repo/b.go"}}, 1)
	m.SetWidth(80)
	before := m.View()
	closeBefore := closeX(m.Targets(), 1)
	m.SetGitStatus(map[string]string{"/repo/a.go": "M"})
	if after := m.View(); after == before {
		t.Fatal("tab bar did not redraw after SetGitStatus")
	}
	if got := closeX(m.Targets(), 1); got != closeBefore+2 {
		t.Fatalf("close glyph at %d, want %d (badge + space)", got, closeBefore+2)
	}
	m.SetGitStatus(nil)
	if got := m.View(); got != before {
		t.Fatal("clearing the git status did not restore the plain tab")
	}
}

func closeX(ts []Target, id int) int {
	for _, tg := range ts {
		if tg.Kind == TargetClose && tg.ID == id {
			return tg.X
		}
	}
	return -1
}
