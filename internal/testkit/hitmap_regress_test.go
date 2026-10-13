package testkit

import (
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
