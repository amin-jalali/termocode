package tea

import "testing"

// termocode (Group S4): inside tmux or GNU screen on a server (TERM=
// screen*/tmux*) keys arrive in the multiplexer's encoding, not the
// outer terminal's. Each must decode to the same key.
func TestTmuxScreenKeys(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"\x1b[1~", "home"}, // tmux / screen Home
		{"\x1b[4~", "end"},  // tmux / screen End
		{"\x1b[H", "home"},  // tmux with xterm-keys
		{"\x1b[F", "end"},
		{"\x1b[3~", "delete"},
		{"\x1b[5~", "pgup"},
		{"\x1b[6~", "pgdown"},
		{"\x1b[1;5C", "ctrl+right"}, // xterm-keys on (default since tmux 2.x)
		{"\x1b[1;2A", "shift+up"},
		{"\x1b[Z", "shift+tab"},
		{"\x1bOP", "f1"},
		{"\x1bx", "alt+x"},
		{"\x1bOA", "up"}, // application cursor keys
	}
	for _, c := range cases {
		msgs := ParseInput([]byte(c.in))
		if len(msgs) != 1 {
			t.Errorf("%q: %d messages %#v", c.in, len(msgs), msgs)
			continue
		}
		k, ok := msgs[0].(KeyMsg)
		if !ok || k.String() != c.want {
			t.Errorf("%q: got %v, want %s", c.in, msgs[0], c.want)
		}
	}
	// SGR mouse, which tmux passes on with mouse on.
	msgs := ParseInput([]byte("\x1b[<0;10;5M"))
	if len(msgs) != 1 {
		t.Fatalf("mouse: %#v", msgs)
	}
	if m, ok := msgs[0].(MouseMsg); !ok || m.X != 9 || m.Y != 4 {
		t.Errorf("mouse: got %#v", msgs[0])
	}
}
