package app

import "testing"

// Spec (terminal R5): "Copied (via terminal)" is shown only where OSC 52
// can work. VTE (GNOME Terminal, Tilix …), macOS Terminal.app, xterm (off
// by default), the Linux console and JetBrains' terminal ignore it, so a
// copy that only reached OSC 52 there is a failure, not a success.
func TestOSC52Honoured(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want bool
	}{
		{map[string]string{"TERM": "xterm-kitty", "KITTY_WINDOW_ID": "1"}, true},
		{map[string]string{"TERM_PROGRAM": "WezTerm"}, true},
		{map[string]string{"TERM": "xterm-256color"}, true}, // unknown: give it the benefit
		{map[string]string{"VTE_VERSION": "7600"}, false},
		{map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, false},
		{map[string]string{"XTERM_VERSION": "XTerm(390)"}, false},
		{map[string]string{"TERM": "linux"}, false},
		{map[string]string{"TERMINAL_EMULATOR": "JetBrains-JediTerm"}, false},
		// Inside tmux the outer terminal decides; tmux forwards OSC 52
		// with set-clipboard, so do not guess from inherited variables.
		{map[string]string{"TMUX": "/tmp/tmux-1/default,1,0", "VTE_VERSION": "7600"}, true},
	}
	for _, c := range cases {
		if got := osc52Honoured(func(k string) string { return c.env[k] }); got != c.want {
			t.Errorf("%v: got %v, want %v", c.env, got, c.want)
		}
	}
}
