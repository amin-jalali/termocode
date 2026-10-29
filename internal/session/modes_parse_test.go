package session

import (
	"strings"
	"testing"
)

// Broken or cut-off sequences in the UI output must not leave the mode
// tracker stuck: the next proper sequence still counts.
func TestModesRecoverFromBrokenSequences(t *testing.T) {
	cases := map[string]struct {
		in   string
		want string // in Replay
		not  string // not in Replay
	}{
		"OSC ended by ESC backslash": {in: "\x1b]11;#101010\x1b\\", want: "\x1b]11;#101010\x07"},
		"OSC cut by a new sequence":  {in: "\x1b]11;#202020\x1b[?1049h", want: "\x1b[?1049h", not: "#202020"},
		"CSI cancelled by CAN":       {in: "\x1b[?1004\x18h", not: "1004"},
		"CSI cancelled by SUB":       {in: "\x1b[?1004\x1ah", not: "1004"},
		"CSI cut by a new sequence":  {in: "\x1b[?10\x1b[?2004h", want: "\x1b[?2004h", not: "?10h"},
		"double ESC":                 {in: "\x1b\x1b[?1000h", want: "\x1b[?1000h"},
		"ESC then a plain char":      {in: "\x1b7\x1b[?1006h", want: "\x1b[?1006h"},
		"APC cut by a new sequence":  {in: "\x1b_junk\x1b[?25l", want: "\x1b[?25l"},
		"DCS cut by a new sequence":  {in: "\x1bPjunk\x1b[?1002h", want: "\x1b[?1002h"},
	}
	for name, c := range cases {
		m := NewModes()
		feedSplit(m, c.in)
		got := string(m.Replay())
		if c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: replay %q lacks %q", name, got, c.want)
		}
		if c.not != "" && strings.Contains(got, c.not) {
			t.Errorf("%s: replay %q has %q", name, got, c.not)
		}
	}
}

// Kitty keyboard flags: a pop without a count pops one, a bad count too.
func TestModesKittyKeyboardPop(t *testing.T) {
	m := NewModes()
	m.Feed([]byte("\x1b[>1u\x1b[>3u\x1b[>7u"))
	m.Feed([]byte("\x1b[<u"))
	if got := string(m.Replay()); !strings.HasSuffix(strings.Split(got, "\x1b]")[0], "\x1b[>1u\x1b[>3u") {
		t.Fatalf("after one pop: %q", got)
	}
	m.Feed([]byte("\x1b[<xu\x1b[<5u"))
	if got := string(m.Replay()); strings.Contains(got, ">1u") {
		t.Fatalf("after popping all: %q", got)
	}
}
