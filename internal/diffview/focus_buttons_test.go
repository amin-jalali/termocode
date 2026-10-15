package diffview

import (
	"strings"
	"testing"
)

// Spec (UI review #12): only the focused change carries Accept / Reject;
// the others show a quiet state mark (· pending, ✓ accepted, ✕ rejected),
// and a click where their buttons used to be does not decide them.
func TestOnlyFocusedChangeHasButtons(t *testing.T) {
	m := demo(true) // two changes, the first focused
	s := plain(m)
	acc := icons().check + " Accept "
	n := 0
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, "Change ") && strings.Contains(l, acc) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d Accept buttons, want 1 (focused change only):\n%s", n, s)
	}
	head2 := lineWith(s, "Change 2 of 2 ·")
	if strings.Contains(head2, "Accept") || !strings.Contains(head2, icons().pend) {
		t.Errorf("unfocused change header %q: want a pending mark, no buttons", head2)
	}
	// Accept the first: it shows ✓, focus moves to the second, which now
	// has the buttons.
	m, _ = key(m, "a")
	s = plain(m)
	if h1 := lineWith(s, "Change 1 of 2 ·"); !strings.Contains(h1, icons().check) || strings.Contains(h1, "Undo") {
		t.Errorf("accepted, unfocused change %q: want ✓ only", h1)
	}
	if h2 := lineWith(s, "Change 2 of 2 ·"); !strings.Contains(h2, acc) {
		t.Errorf("focused change %q: want the buttons", h2)
	}
}

func lineWith(s, text string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, text) {
			return l
		}
	}
	return ""
}
