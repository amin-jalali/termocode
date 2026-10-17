package tea

import "testing"

// A terminal's late answers to the startup probe (kitty graphics APC,
// XTVERSION DCS, OSC colour reports) reach the input after Bubble Tea took
// the TTY. They must never become typed keys (terminal-compat R3).
func TestEscapeStringsAreNotKeys(t *testing.T) {
	for _, in := range []string{
		"\x1b_Gi=31;OK\x1b\\",                  // kitty graphics answer (APC, ST)
		"\x1b_Gi=31;ENOENT:no such image\x1b\\", // kitty "no"
		"\x1bP>|kitty(0.36.4)\x1b\\",           // XTVERSION (DCS)
		"\x1bP1$r0m\x1b\\",                     // DECRQSS
		"\x1b]11;rgb:1010/1010/1010\x07",        // OSC 11 (BEL)
		"\x1b]11;rgb:1010/1010/1010\x1b\\",      // OSC 11 (ST)
		"\x1b^private\x1b\\",                   // PM
		"\x1bXsos\x1b\\",                       // SOS
	} {
		for _, m := range ParseInput([]byte(in)) {
			if k, ok := m.(KeyMsg); ok {
				t.Errorf("%q: key %q leaked", in, k.String())
			}
		}
	}
	// Keys around a string still arrive.
	msgs := ParseInput([]byte("a\x1b_Gi=31;OK\x1b\\b"))
	var keys []string
	for _, m := range msgs {
		if k, ok := m.(KeyMsg); ok {
			keys = append(keys, k.String())
		}
	}
	if len(keys) != 2 || keys[0] != "a" || keys[1] != "b" {
		t.Errorf("keys around an APC = %v, want [a b]", keys)
	}
	// Alt+_ , Alt+P, Alt+] without a terminator stay keys.
	for in, want := range map[string]string{"\x1b_": "alt+_", "\x1bP": "alt+P", "\x1b]": "alt+]"} {
		msgs := ParseInput([]byte(in))
		if len(msgs) != 1 {
			t.Errorf("%q: %d msgs %v", in, len(msgs), msgs)
			continue
		}
		if k, ok := msgs[0].(KeyMsg); !ok || k.String() != want {
			t.Errorf("%q = %v, want %s", in, msgs[0], want)
		}
	}
}
