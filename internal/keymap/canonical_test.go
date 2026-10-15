package keymap

import "testing"

// Spec: one key has one canonical name, whatever spelling the terminal or
// the keymap uses: modifiers in the order ctrl, alt, shift; xterm's F13–F24
// are Shift+F1–F12; an upper-case letter after Alt or Ctrl is Shift.
func TestCanonicalKey(t *testing.T) {
	for in, want := range map[string]string{
		"ctrl+s": "ctrl+s", "shift+alt+f": "alt+shift+f", "alt+F": "alt+shift+f",
		"f15": "shift+f3", "f17": "shift+f5", "alt+f17": "alt+shift+f5", "f23": "shift+f11",
		"shift+ctrl+pgup": "ctrl+shift+pgup", "A": "A", "a": "a", "alt+shift+r": "alt+shift+r",
		"alt+R": "alt+shift+r", "ctrl+_": "ctrl+_", "f1": "f1", "shift+f3": "shift+f3",
	} {
		if got := canonicalKey(in); got != want {
			t.Errorf("canonicalKey(%q) = %q, want %q", in, got, want)
		}
	}
}
