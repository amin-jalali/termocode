package termfont

import (
	"strings"
	"testing"
)

func env(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want Method
	}{
		{"kitty window", map[string]string{"KITTY_WINDOW_ID": "1", "TERM": "xterm-kitty"}, Kitty},
		{"kitty over ssh (TERM only)", map[string]string{"TERM": "xterm-kitty"}, Kitty},
		{"kitty inside tmux: the DCS does not reach kitty", map[string]string{"KITTY_WINDOW_ID": "1", "TMUX": "/tmp/t,1,0"}, None},
		{"iTerm2", map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM": "xterm-256color"}, None},
		{"WezTerm", map[string]string{"TERM_PROGRAM": "WezTerm", "TERM": "xterm-256color"}, None},
		{"Ghostty", map[string]string{"TERM_PROGRAM": "ghostty", "TERM": "xterm-ghostty"}, None},
		{"GNOME / VTE", map[string]string{"VTE_VERSION": "7600", "TERM": "xterm-256color"}, None},
		{"Windows Terminal", map[string]string{"WT_SESSION": "x", "TERM": "xterm-256color"}, None},
		{"foot", map[string]string{"TERM": "foot"}, None},
		{"empty", map[string]string{}, None},
	}
	for _, c := range cases {
		if got := Detect(env(c.env)); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRequestKitty(t *testing.T) {
	got := Request(Kitty, -2, false)
	want := "\x1bP@kitty-cmd{\"cmd\":\"set-font-size\",\"version\":[0,14,2],\"no_response\":true," +
		"\"payload\":{\"size\":2,\"increment_op\":\"-\"}}\x1b\\"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	if up := Request(Kitty, 3, true); !strings.Contains(up, `"no_response":false`) ||
		!strings.Contains(up, `"size":3,"increment_op":"+"`) {
		t.Errorf("grow with reply: %q", up)
	}
	if Request(Kitty, 0, false) != "" || Request(None, -2, false) != "" {
		t.Error("no escape is wanted for delta 0 or an unsupported terminal")
	}
}

func TestParseKitty(t *testing.T) {
	for _, d := range []int{-3, -1, 1, 4} {
		body := strings.TrimSuffix(strings.TrimPrefix(Request(Kitty, d, d > 0), "\x1bP"), "\x1b\\")
		if got, ok := ParseKitty(body); !ok || got != d {
			t.Errorf("delta %d: got %d, %v", d, got, ok)
		}
	}
	for _, body := range []string{
		`@kitty-cmd{"cmd":"ls","version":[0,14,2]}`,
		`@kitty-cmd{"cmd":"set-font-size","version":[0,14,2],"payload":{"size":12}}`, // absolute: not a delta
		`@kitty-cmd{"cmd":"set-font-size","payload":{"size":2,"increment_op":"*"}}`,
		`@kitty-cmd{broken`,
		`+q544e`,
	} {
		if d, ok := ParseKitty(body); ok {
			t.Errorf("%q: parsed as delta %d", body, d)
		}
	}
}

func TestKittyAnswer(t *testing.T) {
	cases := []struct {
		reply          string
		answered, isOK bool
	}{
		{"\x1bP@kitty-cmd{\"ok\": true}\x1b\\", true, true},
		{"\x1b[?62;c\x1bP@kitty-cmd{\"ok\":true}\x1b\\", true, true},
		{"\x1bP@kitty-cmd{\"ok\": false, \"error\": \"Remote control is disabled\"}\x1b\\", true, false},
		{"\x1bP@kitty-cmd{\"ok\": tr", false, false}, // not all in yet
		{"\x1b[?62;c", false, false},
		{"", false, false},
	}
	for _, c := range cases {
		a, ok := KittyAnswer([]byte(c.reply))
		if a != c.answered || ok != c.isOK {
			t.Errorf("%q: got answered=%v ok=%v", c.reply, a, ok)
		}
	}
}

// fakeTerm answers a query the way a terminal would.
func fakeTerm(reply string, asked *string) Querier {
	return func(q string, done func([]byte) bool) []byte {
		*asked = q
		if reply != "" && !done([]byte(reply)) {
			panic("done() must be true once kitty answered")
		}
		return []byte(reply)
	}
}

func TestChange(t *testing.T) {
	kitty := env(map[string]string{"KITTY_WINDOW_ID": "7"})
	var asked string

	// kitty with remote control on: applied, restored by the inverse.
	s := Change(kitty, -2, fakeTerm("\x1bP@kitty-cmd{\"ok\": true}\x1b\\", &asked))
	if !s.Applied() {
		t.Fatal("kitty answered ok but the change is not marked applied")
	}
	if !strings.Contains(asked, `"no_response":false`) || !strings.Contains(asked, `"increment_op":"-"`) {
		t.Errorf("the first request must ask for an answer: %q", asked)
	}
	if got, want := s.Restore(), Request(Kitty, 2, false); got != want {
		t.Errorf("restore %q, want %q", got, want)
	}

	// kitty with remote control off: kitty says no; nothing to restore.
	s = Change(kitty, -2, fakeTerm("\x1bP@kitty-cmd{\"ok\": false, \"error\": \"Remote control is disabled\"}\x1b\\", &asked))
	if s.Applied() || s.Restore() != "" {
		t.Errorf("refused change: applied=%v restore=%q", s.Applied(), s.Restore())
	}

	// No answer (timeout): not applied.
	s = Change(kitty, -2, fakeTerm("", &asked))
	if s.Applied() || s.Restore() != "" {
		t.Errorf("silent terminal: applied=%v restore=%q", s.Applied(), s.Restore())
	}

	// Other terminals and delta 0: the terminal is never asked.
	for _, c := range []struct {
		getenv func(string) string
		delta  int
	}{{env(map[string]string{"TERM_PROGRAM": "iTerm.app"}), -2}, {kitty, 0}} {
		asked = ""
		s = Change(c.getenv, c.delta, fakeTerm("\x1bP@kitty-cmd{\"ok\": true}\x1b\\", &asked))
		if asked != "" || s.Applied() || s.Restore() != "" {
			t.Errorf("delta %d: asked %q, applied=%v", c.delta, asked, s.Applied())
		}
	}
}

func TestTipNeeded(t *testing.T) {
	if (State{}).TipNeeded(0) {
		t.Error("font_delta 0 needs no tip")
	}
	if !(State{}).TipNeeded(-2) {
		t.Error("an unapplied font_delta needs the tip")
	}
	if (State{method: Kitty, delta: -2}).TipNeeded(-2) {
		t.Error("an applied font_delta needs no tip")
	}
}
