package keymap

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/keytest"
)

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

func TestExportedCanonicalKey(t *testing.T) {
	if got := CanonicalKey("shift+alt+f"); got != "alt+shift+f" {
		t.Fatalf("CanonicalKey = %q, want alt+shift+f", got)
	}
}

// Spec: Shift+F1 opens the guide for the focused item, whether the
// terminal sends it as shift+f1 (kitty) or f13 (xterm); plain F1 stays
// the palette.
func TestShiftF1IsContextHelp(t *testing.T) {
	k := Default()
	legacy, kitty := keytest.Encode("shift+f1")
	for _, b := range []string{legacy, kitty, "\x1b[1;2P"} {
		msgs := tea.ParseInput([]byte(b))
		if len(msgs) != 1 {
			t.Errorf("%q: %d messages", b, len(msgs))
			continue
		}
		km, _ := msgs[0].(tea.KeyMsg)
		if got := k.Match(km); got != ActionContextHelp {
			t.Errorf("%q (%s) = %v, want ActionContextHelp", b, km.String(), got)
		}
	}
	if got := k.Match(tea.KeyMsg{Type: tea.KeyF1}); got != ActionCommandPalette {
		t.Errorf("f1 = %v, want the palette", got)
	}
	if name := actionNames[ActionContextHelp]; name != "ContextHelp" {
		t.Errorf("name = %q", name)
	}
	if HumanKey("f13") != "Shift+F1" || HumanKey("shift+f1") != "Shift+F1" {
		t.Errorf("HumanKey: %q, %q", HumanKey("f13"), HumanKey("shift+f1"))
	}
}
