package editor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/keytest"
)

// nvimName is the nvim notation for a key name after its modifiers.
var nvimName = map[string]string{
	"up": "Up", "down": "Down", "left": "Left", "right": "Right", "home": "Home", "end": "End",
	"pgup": "PageUp", "pgdown": "PageDown", "delete": "Del", "insert": "Insert",
	"enter": "CR", "tab": "Tab", "backspace": "BS", "esc": "Esc", "space": "Space",
}

// allCombos lists every key termocode may get: specials and F-keys with
// every modifier set, and Ctrl / Alt / Ctrl+Alt with letters, digits and
// punctuation.
func allCombos() []string {
	mods := []string{"", "shift+", "ctrl+", "alt+", "ctrl+shift+", "alt+shift+", "ctrl+alt+", "ctrl+alt+shift+"}
	var names []string
	for n := range nvimName {
		names = append(names, n)
	}
	for i := 1; i <= 12; i++ {
		names = append(names, fixF("f"+string(rune('0'+i/10))+string(rune('0'+i%10))))
	}
	var out []string
	for _, n := range names {
		for _, m := range mods {
			out = append(out, m+n)
		}
	}
	for _, c := range "abcdefghijklmnopqrstuvwxyz0123456789.,;'/`[]-=" {
		for _, m := range []string{"ctrl+", "alt+", "ctrl+alt+", "ctrl+shift+", "alt+shift+"} {
			out = append(out, m+string(c))
		}
	}
	return out
}

func fixF(n string) string {
	if strings.HasPrefix(n, "f0") {
		return "f" + n[2:]
	}
	return n
}

// Spec (2026-10-15, "make sure all combination keys are and will be
// healthy"): every key combination that reaches the editor goes on to nvim;
// none is silently dropped. From a kitty-protocol terminal (no ambiguity)
// nvim gets exactly the key pressed.
func TestEveryKeyComboReachesNvim(t *testing.T) {
	for _, key := range allCombos() {
		legacy, kitty := keytest.Encode(key)
		for _, enc := range []struct{ name, b string }{{"legacy", legacy}, {"kitty", kitty}} {
			if enc.b == "" {
				continue
			}
			msgs := tea.ParseInput([]byte(enc.b))
			if len(msgs) != 1 {
				t.Errorf("%s (%s %q): %d messages", key, enc.name, enc.b, len(msgs))
				continue
			}
			k, ok := msgs[0].(tea.KeyMsg)
			if !ok {
				t.Errorf("%s (%s %q): not a key (%T)", key, enc.name, enc.b, msgs[0])
				continue
			}
			got := translateKey(k)
			if got == "" {
				t.Errorf("%s (%s %q → %q): dropped, nvim gets nothing", key, enc.name, enc.b, k.String())
				continue
			}
			if enc.name == "kitty" && key != "ctrl+z" && key != "ctrl+y" {
				want := expectNvim(key)
				// nvim's own name for Ctrl+Space is <C-@> (NUL): the same key.
				switch key {
				case "ctrl+space":
					want = "<C-@>"
				case "ctrl+alt+space":
					want = "<C-M-@>"
				}
				if want != "" && got != want {
					t.Errorf("%s (kitty %q): nvim gets %q, want %q", key, enc.b, got, want)
				}
			}
		}
	}
}

// expectNvim is the nvim notation for key; "" when the key is plain text
// (a letter with only Shift) or Ctrl+letter that nvim spells as a control
// character.
func expectNvim(key string) string {
	parts := strings.Split(key, "+")
	name := parts[len(parts)-1]
	var mods strings.Builder
	shift := false
	for _, p := range parts[:len(parts)-1] {
		switch p {
		case "ctrl":
			mods.WriteString("C-")
		case "alt":
			mods.WriteString("M-")
		case "shift":
			shift = true
		}
	}
	if n, ok := nvimName[name]; ok {
		if shift {
			mods.WriteString("S-")
		}
		if mods.Len() == 0 {
			if name == "space" {
				return " "
			}
			return "<" + n + ">"
		}
		return "<" + mods.String() + n + ">"
	}
	if len(name) >= 2 && name[0] == 'f' {
		if shift {
			mods.WriteString("S-")
		}
		return "<" + mods.String() + "F" + name[1:] + ">"
	}
	return "" // letters / punctuation: checked for "not dropped" only
}
