package theme

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Spec (UI review #3): every key hint uses one notation, the one of the
// keyboard reference: Enter, Esc, Tab, Ctrl+C, Shift+Enter, Alt+A, F1.
// Plain letters (review keys a / r / A) and arrows stay as they are.
func TestKeyLabel(t *testing.T) {
	cases := map[string]string{
		"enter": "Enter", "↵": "Enter", "esc": "Esc", "tab": "Tab", "⇥": "Tab",
		"ctrl+c": "Ctrl+C", "shift+enter": "Shift+Enter", "alt+a": "Alt+A",
		"f6": "F6", "F1": "F1", "^P": "Ctrl+P", "^T": "Ctrl+T",
		"⌃↵": "Ctrl+Enter", "⌥↵": "Alt+Enter", "ctrl+shift+p": "Ctrl+Shift+P",
		"a/r": "a/r", "A/R": "A/R", "n/p": "n/p", "e": "e", "t": "t",
		"↑↓": "↑↓", "←→": "←→", "space": "Space",
	}
	for in, want := range cases {
		if got := KeyLabel(in); got != want {
			t.Errorf("KeyLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKeycapUsesKeyLabel(t *testing.T) {
	if got := ansi.Strip(HintBar(LG(BgFloat), "esc", "close", "enter", "select")); !strings.Contains(got, " Esc ") || !strings.Contains(got, " Enter ") {
		t.Errorf("hint bar %q", got)
	}
}
