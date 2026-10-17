package main

import (
	"strings"
	"testing"
)

// Spec (terminal R4): termocode leaves the user's terminal as it found it.
// It erases the scrollback (CSI 3 J) and paints the host background
// (OSC 11) only in VTE terminals, where they hide the scrollbar overlay and
// the purple strip under the last row; never inside tmux / screen (the
// pane would keep the colour) and never elsewhere (Windows Terminal,
// kitty … would lose the user's shell history). It never erases the
// scrollback on the way out.
func TestHostEscapes(t *testing.T) {
	cases := []struct {
		name         string
		env          map[string]string
		erase, paint bool
	}{
		{"gnome terminal", map[string]string{"VTE_VERSION": "7600"}, true, true},
		{"vte in tmux", map[string]string{"VTE_VERSION": "7600", "TMUX": "/tmp/t,1,0"}, false, false},
		{"vte in screen", map[string]string{"VTE_VERSION": "7600", "STY": "1.pts"}, false, false},
		{"kitty", map[string]string{"TERM": "xterm-kitty"}, false, false},
		{"windows terminal", map[string]string{"WT_SESSION": "x"}, false, false},
	}
	for _, c := range cases {
		enter, leave := hostEscapes(func(k string) string { return c.env[k] })
		if got := strings.Contains(enter, "\x1b[3J"); got != c.erase {
			t.Errorf("%s: scrollback erase = %v, want %v", c.name, got, c.erase)
		}
		if got := strings.Contains(enter, "\x1b]11;"); got != c.paint {
			t.Errorf("%s: background paint = %v, want %v", c.name, got, c.paint)
		}
		if got := strings.Contains(leave, "\x1b]111"); got != c.paint {
			t.Errorf("%s: background reset = %v, want %v", c.name, got, c.paint)
		}
		if strings.Contains(leave, "\x1b[3J") {
			t.Errorf("%s: scrollback erased on exit", c.name)
		}
	}
}

// Terminal R14: the Linux console has no alternate screen, so the IDE
// frame would stay on the TTY after quit; the screen is cleared on the
// way out. No background paint, no BDSM request (CSI 8 l) there.
func TestHostEscapesLinuxConsole(t *testing.T) {
	getenv := func(k string) string { return map[string]string{"TERM": "linux"}[k] }
	enter, leave := hostEscapes(getenv)
	if enter != "" {
		t.Errorf("enter = %q, want nothing", enter)
	}
	if !strings.HasSuffix(leave, "\x1b[2J\x1b[H") {
		t.Errorf("leave = %q, want the screen cleared", leave)
	}
	if e, l := rtlEscapes("\x1b[8l", "\x1b[8h", getenv); e != "" || l != "" {
		t.Errorf("BDSM on the console: %q %q", e, l)
	}
	if e, _ := rtlEscapes("\x1b[8l", "\x1b[8h", func(string) string { return "" }); e != "\x1b[8l" {
		t.Errorf("BDSM dropped elsewhere: %q", e)
	}
}
