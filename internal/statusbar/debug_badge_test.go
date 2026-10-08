package statusbar

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/theme"
)

func TestDebugBadgeSpanMatchesRenderedText(t *testing.T) {
	m := New(theme.Theme{})
	m.SetWidth(120)
	s := State{Path: "/a.go", Project: "proj", Lang: "Go", Line: 1, Col: 1,
		Branch: "main", Term: true, Debug: true}
	x0, x1, ok := m.DebugBadgeSpan(s)
	if !ok {
		t.Fatal("badge should be visible")
	}
	row := []rune(stripCSI(m.View(s)))
	if got := string(row[x0:x1]); got != "● DEBUG" {
		t.Fatalf("span [%d,%d) = %q", x0, x1, got)
	}
	s.Debug = false
	if _, _, ok := m.DebugBadgeSpan(s); ok {
		t.Fatal("no session → not ok")
	}
}
