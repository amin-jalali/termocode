package testkit

import (
	"strings"
	"testing"
)

// Spec (UI review signature #2): clicking the termocode mark at the top
// of the activity bar opens the command palette.
func TestActivityMarkOpensPalette(t *testing.T) {
	d := New(t, Options{W: 120, H: 40})
	if strings.Contains(screen(d), "Command Palette") {
		t.Fatal("palette open before the click")
	}
	d.Click(1, 0)
	if !strings.Contains(screen(d), "Command Palette") {
		t.Errorf("the command palette did not open:\n%s", screen(d))
	}
}
