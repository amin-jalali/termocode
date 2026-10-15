package tea

import "testing"

// termocode patch: kitty keyboard protocol (CSI u), "disambiguate" flag.
// Keys a legacy terminal can send must come out exactly as before (same
// Type, same String), and the combos legacy cannot send get the names
// termocode's keymap uses (ctrl+shift+o, ctrl+., ctrl+tab, shift+enter).
func TestCSIuKeys(t *testing.T) {
	cases := []struct {
		in   string
		want string
		typ  KeyType
	}{
		{"\x1b[99;5u", "ctrl+c", KeyCtrlC},
		{"\x1b[115;5u", "ctrl+s", KeyCtrlS},
		{"\x1b[99;69u", "ctrl+c", KeyCtrlC},  // + caps lock
		{"\x1b[99;133u", "ctrl+c", KeyCtrlC}, // + num lock
		{"\x1b[99;5:1u", "ctrl+c", KeyCtrlC}, // press event
		{"\x1b[105;5u", "tab", KeyTab},       // ctrl+i is tab, as in legacy
		{"\x1b[27u", "esc", KeyEscape},
		{"\x1b[27;3u", "alt+esc", KeyEscape},
		{"\x1b[120;3u", "alt+x", KeyRunes},
		{"\x1b[114;4u", "alt+R", KeyRunes},     // legacy: ESC R
		{"\x1b[97;7u", "alt+ctrl+a", KeyCtrlA}, // legacy: ESC ^A
		{"\x1b[32;5u", "ctrl+@", KeyCtrlAt},
		{"\x1b[32;3u", "alt+ ", KeySpace},
		{"\x1b[13;3u", "alt+enter", KeyEnter},
		{"\x1b[9;2u", "shift+tab", KeyShiftTab},
		{"\x1b[127;3u", "alt+backspace", KeyBackspace},
		// New: combos legacy terminals cannot send.
		{"\x1b[111;6u", "ctrl+shift+o", KeyExtended},
		{"\x1b[111:79;6u", "ctrl+shift+o", KeyExtended}, // with the shifted key
		{"\x1b[46;5u", "ctrl+.", KeyExtended},
		{"\x1b[46;6u", "ctrl+shift+.", KeyExtended},
		{"\x1b[47;5u", "ctrl+/", KeyExtended},
		{"\x1b[9;5u", "ctrl+tab", KeyExtended},
		{"\x1b[9;6u", "ctrl+shift+tab", KeyExtended},
		{"\x1b[13;2u", "shift+enter", KeyExtended},
		{"\x1b[13;5u", "ctrl+enter", KeyExtended},
		{"\x1b[105;7u", "ctrl+alt+i", KeyExtended}, // legacy would be alt+tab
		{"\x1b[116;8u", "ctrl+alt+shift+t", KeyExtended},
	}
	for _, c := range cases {
		w, msg := detectOneMsg([]byte(c.in), false)
		if w != len(c.in) {
			t.Errorf("%q: consumed %d of %d bytes", c.in, w, len(c.in))
		}
		k, ok := msg.(KeyMsg)
		if !ok {
			t.Errorf("%q: got %T %v, want a key", c.in, msg, msg)
			continue
		}
		if k.String() != c.want || k.Type != c.typ {
			t.Errorf("%q: got %q (type %d), want %q (type %d)", c.in, k.String(), k.Type, c.want, c.typ)
		}
	}
}

// The answer to the flags query (CSI ? flags u) and key releases are not
// key presses.
func TestCSIuNotKeys(t *testing.T) {
	for _, in := range []string{"\x1b[?1u", "\x1b[?0u", "\x1b[99;5:3u"} {
		w, msg := detectOneMsg([]byte(in), false)
		if w != len(in) {
			t.Errorf("%q: consumed %d of %d bytes", in, w, len(in))
		}
		if _, ok := msg.(KeyMsg); ok {
			t.Errorf("%q: got key %v", in, msg)
		}
	}
}

// Legacy input is unchanged.
func TestCSIuLegacyUnchanged(t *testing.T) {
	for in, want := range map[string]string{"\x03": "ctrl+c", "\x1bx": "alt+x", "\x1b[A": "up", "\x1b[1;5C": "ctrl+right", "a": "a"} {
		_, msg := detectOneMsg([]byte(in), false)
		if k, ok := msg.(KeyMsg); !ok || k.String() != want {
			t.Errorf("%q: got %v, want %q", in, msg, want)
		}
	}
}

// Each CSI u form of a key a legacy terminal can send decodes to exactly
// the Key the legacy bytes give.
func TestCSIuMatchesLegacy(t *testing.T) {
	pairs := map[string]string{
		"\x1b[99;5u":  "\x03",
		"\x1b[120;3u": "\x1bx",
		"\x1b[114;4u": "\x1bR",
		"\x1b[97;7u":  "\x1b\x01",
		"\x1b[32;5u":  "\x00",
		"\x1b[32;3u":  "\x1b ",
		"\x1b[13;3u":  "\x1b\r",
		"\x1b[9;2u":   "\x1b[Z",
		"\x1b[127;3u": "\x1b\x7f",
		"\x1b[27;3u":  "\x1b\x1b",
		"\x1b[92;5u":  "\x1c",
		"\x1b[95;5u":  "\x1f",
		"\x1b[106;5u": "\n",
		"\x1b[91;5u":  "\x1b", // ctrl+[ is Esc for vim users
	}
	for csiu, leg := range pairs {
		_, a := detectOneMsg([]byte(csiu), false)
		_, b := detectOneMsg([]byte(leg), false)
		ka, kb := a.(KeyMsg), b.(KeyMsg)
		if ka.Type != kb.Type || ka.Alt != kb.Alt || ka.String() != kb.String() {
			t.Errorf("%q → %+v (%q); legacy %q → %+v (%q)", csiu, ka, ka.String(), leg, kb, kb.String())
		}
	}
}

// Modified special keys the v1 tables do not list (Ctrl+F12, Ctrl+Shift+
// PgUp, Shift+F12, Shift+Alt+F5 …) still come through, named with the
// modifiers in the order ctrl, alt, shift; listed ones keep their names.
func TestModifiedSpecialKeys(t *testing.T) {
	for in, want := range map[string]string{
		"\x1b[24;5~": "ctrl+f12",
		"\x1b[5;6~":  "ctrl+shift+pgup",
		"\x1b[6;6~":  "ctrl+shift+pgdown",
		"\x1b[24;2~": "shift+f12",
		"\x1b[23;2~": "shift+f11",
		"\x1b[15;4~": "alt+shift+f5",
		"\x1b[3;5~":  "ctrl+delete",
		"\x1b[3;2~":  "shift+delete", // upstream v1 table said alt+insert
		"\x1b[2;3~":  "alt+insert",
		"\x1b[1;7P":  "ctrl+alt+f1",
		"\x1b[1;5A":  "ctrl+up",   // listed: unchanged
		"\x1b[5;5~":  "ctrl+pgup", // listed: unchanged
		"\x1b[1;2R":  "f15",       // listed (xterm Shift+F3): unchanged
	} {
		msgs := ParseInput([]byte(in))
		if len(msgs) != 1 {
			t.Errorf("%q: %d msgs", in, len(msgs))
			continue
		}
		if k, ok := msgs[0].(KeyMsg); !ok || k.String() != want {
			t.Errorf("%q: got %v (%T), want %s", in, msgs[0], msgs[0], want)
		}
	}
}
