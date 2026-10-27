package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/keymap"
)

// Spec: Alt+Shift+I (legacy "alt+I", CSI-u "alt+shift+i") and Alt+O open
// Itermoc; the same key again goes back to the view the sidebar had.
// Alt+I stays Edit with AI.
func TestItermocToggleKeys(t *testing.T) {
	km := keymap.Default()
	for _, k := range []string{"alt+I", "alt+shift+i", "alt+o"} {
		if a := km.Bindings()[k]; a != keymap.ActionItermocToggle {
			t.Errorf("%s -> %v", k, keymap.ActionName(a))
		}
	}
	if a := km.Bindings()["alt+i"]; a != keymap.ActionAIEditSelection {
		t.Errorf("alt+i -> %v, must stay Edit with AI", keymap.ActionName(a))
	}
	keys := map[string]tea.KeyMsg{
		"alt+I": {Type: tea.KeyRunes, Runes: []rune{'I'}, Alt: true},
		"alt+o": {Type: tea.KeyRunes, Runes: []rune{'o'}, Alt: true},
	}
	for name, k := range keys {
		m := itermocTestModel(t, 70, 40)
		m.activity.SetActive(activity.ViewGit)
		m.focus = FocusEditor
		m.keys = keymap.Default()
		nm, _ := m.handleGlobalKey(k)
		*m = nm
		if m.activity.Active() != activity.ViewItermoc {
			t.Fatalf("%s did not open Itermoc (view %v)", name, m.activity.Active())
		}
		nm, _ = m.handleGlobalKey(k)
		*m = nm
		if m.activity.Active() != activity.ViewGit {
			t.Fatalf("%s again did not go back (view %v)", name, m.activity.Active())
		}
	}
}

// Spec: the palette row "Itermoc: Open" shows the key.
func TestItermocKeyShownInPalette(t *testing.T) {
	if paletteActions["itermoc-open"] != keymap.ActionItermocToggle {
		t.Error("the palette row Itermoc: Open has no key hint")
	}
}
