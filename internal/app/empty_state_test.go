package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Spec (UI review #10): every empty panel uses one look: a dim glyph, a
// title, a hint and (optionally) the key to press, centred, filling the
// area exactly; small areas keep at least the title.
func TestEmptyStateFitsAndShowsParts(t *testing.T) {
	es := emptyState{Glyph: theme.IconSearch, Title: "Find in files", Hint: "Search the whole workspace. Ctrl+F finds in the current file.", Key: "f8"}
	for _, sz := range [][2]int{{20, 3}, {24, 8}, {40, 12}, {80, 30}} {
		w, h := sz[0], sz[1]
		rows := renderEmptyState(w, h, theme.BgSidebar, es)
		if len(rows) != h {
			t.Fatalf("%dx%d: %d rows", w, h, len(rows))
		}
		for i, r := range rows {
			if got := ansi.StringWidth(r); got != w {
				t.Errorf("%dx%d: row %d is %d wide", w, h, i, got)
			}
		}
		all := ansi.Strip(strings.Join(rows, "\n"))
		if !strings.Contains(all, "Find in files") {
			t.Errorf("%dx%d: no title:\n%s", w, h, all)
		}
		if h >= 8 && (!strings.Contains(all, "Search the whole") || !strings.Contains(all, "F8")) {
			t.Errorf("%dx%d: hint or key missing:\n%s", w, h, all)
		}
	}
}

// The panels use it.
func TestEmptyPanelsUseEmptyState(t *testing.T) {
	old := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	defer func() { theme.CurrentIconMode = old }()
	out := ansi.Strip(strings.Join(scmNotRepoEmpty(30, 20), "\n"))
	if !strings.Contains(out, "Not a git repository") || !strings.Contains(out, theme.IconGit.String()) {
		t.Errorf("SCM empty state:\n%s", out)
	}
}
