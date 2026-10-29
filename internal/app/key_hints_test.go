package app

import (
	"strings"
	"testing"
)

// Spec: key_hints is on until the user turns it off.
func TestKeyHintsDefaultOn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if !keyHintsEnabled(loadSettings()) {
		t.Fatal("key_hints must default to on")
	}
}

// Spec: "Help: Toggle Key Hints" flips the setting, saves it and says so.
func TestToggleKeyHintsPersists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := New()
	m.toggleKeyHints()
	if keyHintsEnabled(loadSettings()) {
		t.Error("first toggle must turn it off")
	}
	m.toggleKeyHints()
	if !keyHintsEnabled(loadSettings()) {
		t.Error("second toggle must turn it on")
	}
}

// Spec: the palette lists it, and the Settings row reads the saved value.
func TestKeyHintsPaletteAndRow(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	found := false
	for _, it := range paletteItems() {
		if it.ID == "toggle-key-hints" && strings.HasPrefix(it.Title, "Help:") {
			found = true
		}
	}
	if !found {
		t.Error("palette has no Help: Toggle Key Hints")
	}
	r, ok := findSettingsRow("setting-key-hints")
	if !ok || r.Label != "key_hints" || r.Default != "true" {
		t.Fatalf("row = %+v ok=%v", r, ok)
	}
	m := New()
	m.toggleKeyHints()
	if got := currentSettingValue(r, loadSettings()); got != "false" {
		t.Errorf("row value after toggle = %q", got)
	}
}
