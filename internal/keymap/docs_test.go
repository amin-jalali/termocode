package keymap

import "testing"

// Docs: every Action must have a name (for keymap.json) and a description
// (for docs/reference/keys.md).
func TestEveryActionDocumented(t *testing.T) {
	for a := ActionQuit; a <= ActionDebugStop; a++ {
		if ActionName(a) == "" {
			t.Errorf("Action %d has no name in actionNames", a)
		}
		info := Describe(a)
		if info.Description == "" {
			t.Errorf("Action %s has no description in docs.go", ActionName(a))
			continue
		}
		known := false
		for _, ar := range Areas {
			known = known || ar == info.Area
		}
		if !known {
			t.Errorf("Action %s: unknown area %q", ActionName(a), info.Area)
		}
	}
}

func TestHumanKey(t *testing.T) {
	cases := map[string]string{
		"ctrl+s":          "Ctrl+S",
		"ctrl+shift+pgup": "Ctrl+Shift+PgUp",
		"alt+f5":          "Alt+F5",
		"alt+f17":         "Shift+Alt+F5",
		"f23":             "Shift+F11",
		"f16":             "Shift+F4",
		"alt+T":           "Alt+Shift+T",
		"alt+shift+r":     "Alt+Shift+R",
		"ctrl+_":          "Ctrl+/",
		"ctrl+\\":         "Ctrl+\\",
		"alt+enter":       "Alt+Enter",
		"ctrl+`":          "Ctrl+`",
	}
	for in, want := range cases {
		if got := HumanKey(in); got != want {
			t.Errorf("HumanKey(%q) = %q, want %q", in, got, want)
		}
	}
}
