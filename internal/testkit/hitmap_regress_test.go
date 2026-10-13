package testkit

import (
	"strconv"
	"strings"
	"testing"
)

// Regressions found by the hit-map check (hitmap_test.go). Each one was
// red before its fix.

// Spec: Given the Settings gear at the bottom of the activity bar, whose
// lower half sits on the last screen row, when the user clicks that lower
// half, then Settings opens (the status bar is only under the editor, so
// that row is not the status bar's).
func TestActivityBarLastRowClick(t *testing.T) {
	d := New(t, Options{W: 120, H: 40})
	if got := d.Model().ProbeClick(2, d.H-1); got != "activity:Settings" {
		t.Errorf("a click on the last row of the activity bar reaches %q, want the Settings gear", got)
	}
	d.Click(2, d.H-1)
	if !strings.Contains(screen(d), "⚙ Settings") {
		t.Errorf("Settings did not open:\n%s", screen(d))
	}
}

// Spec: Given zen mode, which draws the editor (here the Welcome page) from
// the top-left corner with no chrome, when the user clicks a card near
// its left edge, then the click reaches that card.
func TestZenWelcomeCardClick(t *testing.T) {
	d := New(t, Options{W: 120, H: 40})
	d.Palette("zen-mode")
	for i, title := range []string{"Open file", "Command palette", "Find in files", "Open shell"} {
		x, y, ok := find(d, title)
		if !ok {
			t.Fatalf("no %q card:\n%s", title, screen(d))
		}
		// The card's left border is 6 cells before the title ("│  >  ").
		x -= 5
		want := "welcome:card:" + strconv.Itoa(i)
		if got := d.Model().ProbeClick(x, y); got != want {
			t.Errorf("a click on the %q card at (%d,%d) reaches %q, want %q", title, x, y, got, want)
		}
	}
	x, y, _ := find(d, "Command palette")
	d.Click(x-5, y)
	if !strings.Contains(screen(d), "Command Palette") {
		t.Errorf("the command palette did not open:\n%s", screen(d))
	}
}
