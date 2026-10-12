//go:build unix

package main

import (
	"os"
	"testing"

	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/termimg"
)

// TestApplyClientEnv: the new client's terminal variables replace ours; a
// variable the client does not set is unset; other variables stay.
func TestApplyClientEnv(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("TERMOCODE_TEST_KEEP", "yes")
	applyClientEnv(&session.ClientCaps{Env: map[string]string{"TERM": "xterm-256color", "LANG": "C.UTF-8"}})
	if got := os.Getenv("TERM"); got != "xterm-256color" {
		t.Errorf("TERM = %q", got)
	}
	if got := os.Getenv("LANG"); got != "C.UTF-8" {
		t.Errorf("LANG = %q", got)
	}
	if v, ok := os.LookupEnv("COLORTERM"); ok {
		t.Errorf("COLORTERM still set to %q", v)
	}
	if os.Getenv("TERMOCODE_TEST_KEEP") != "yes" {
		t.Error("a non-terminal variable was touched")
	}
}

// TestGraphicsCapsRoundTrip: the probe result survives the trip from
// client to UI.
func TestGraphicsCapsRoundTrip(t *testing.T) {
	for _, c := range []termimg.Caps{
		{Protocol: termimg.Kitty, CellW: 9, CellH: 18, Reason: "probe"},
		{Protocol: termimg.Sixel, InTmux: true, Passthrough: true},
		{Protocol: termimg.Blocks},
	} {
		if got := graphicsFromSession(graphicsToSession(c)); got != c {
			t.Errorf("round trip %+v -> %+v", c, got)
		}
	}
	if got := graphicsFromSession(&session.GraphicsCaps{Protocol: "hologram"}); got.Protocol != termimg.Blocks {
		t.Errorf("unknown protocol -> %v, want blocks", got.Protocol)
	}
}

// TestTerminalChangeWithoutProbe: a client that did not probe (images
// off on its side) still switches graphics by its environment.
func TestTerminalChangeWithoutProbe(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	msg := terminalChange(&session.ClientCaps{Env: map[string]string{"TERM": "xterm-kitty"}})
	if msg.Graphics.Protocol != termimg.Kitty {
		t.Errorf("kitty terminal -> %v", msg.Graphics.Protocol)
	}
	msg = terminalChange(&session.ClientCaps{Env: map[string]string{"TERM": "xterm-256color"}, Gen: 4})
	if msg.Graphics.Protocol != termimg.Blocks {
		t.Errorf("plain xterm -> %v", msg.Graphics.Protocol)
	}
	if msg.Gen != 4 {
		t.Errorf("attach number %d, want 4", msg.Gen)
	}
	msg = terminalChange(&session.ClientCaps{Graphics: &session.GraphicsCaps{Protocol: "sixel"}, Gen: 6})
	if msg.Graphics.Protocol != termimg.Sixel || msg.Gen != 6 {
		t.Errorf("probed: %v gen %d", msg.Graphics.Protocol, msg.Gen)
	}
}
