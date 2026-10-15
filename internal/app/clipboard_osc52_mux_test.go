package app

import (
	"encoding/base64"
	"strings"
	"testing"
)

// Spec (Group S1): a copy on a server reaches the user's local clipboard
// through OSC 52, also inside tmux and GNU screen on that server. tmux
// forwards a raw OSC 52 only with set-clipboard on, and a DCS
// passthrough only with allow-passthrough on, so both are sent. screen
// never forwards OSC 52 itself: it needs DCS passthrough, in pieces of
// at most 768 bytes.
func TestOSC52SeqMultiplexers(t *testing.T) {
	text := "copy → 界"
	b64 := base64.StdEncoding.EncodeToString([]byte(text))
	raw := "\x1b]52;c;" + b64 + "\x07"
	env := func(kv ...string) func(string) string {
		m := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return func(k string) string { return m[k] }
	}

	if got := osc52Seq(text, env("TERM", "xterm-kitty")); got != raw {
		t.Errorf("plain terminal: got %q, want %q", got, raw)
	}

	tmuxWrapped := "\x1bPtmux;\x1b\x1b]52;c;" + b64 + "\x07\x1b\\"
	for _, e := range []func(string) string{
		env("TMUX", "/tmp/tmux-1000/default,1,0", "TERM", "screen-256color"),
		env("TERM", "tmux-256color"), // tmux on the client, across ssh
	} {
		got := osc52Seq(text, e)
		if !strings.Contains(got, raw) || !strings.Contains(got, tmuxWrapped) {
			t.Errorf("tmux: got %q, want raw %q and passthrough %q", got, raw, tmuxWrapped)
		}
	}

	got := osc52Seq(text, env("STY", "1234.pts-0.host", "TERM", "screen"))
	if !strings.HasPrefix(got, "\x1bP\x1b]52;c;") || !strings.HasSuffix(got, "\x07\x1b\\") {
		t.Errorf("screen: not a DCS passthrough: %q", got)
	}
	// The pieces, joined, are the raw sequence.
	inner := strings.TrimSuffix(strings.TrimPrefix(got, "\x1bP"), "\x1b\\")
	if j := strings.ReplaceAll(inner, "\x1b\\\x1bP", ""); j != raw {
		t.Errorf("screen: pieces join to %q, want %q", j, raw)
	}

	// A long copy: every screen piece stays under screen's 768-byte limit.
	long := strings.Repeat("x", 5000)
	for _, piece := range strings.Split(osc52Seq(long, env("STY", "1.x")), "\x1b\\") {
		if len(piece) > 768 {
			t.Fatalf("screen piece of %d bytes", len(piece))
		}
	}
}

// Spec (Group S1): inside screen the outer terminal decides, like tmux;
// the variables screen inherited may be stale.
func TestOSC52HonouredInScreen(t *testing.T) {
	env := map[string]string{"STY": "1.pts", "VTE_VERSION": "7600"}
	if !osc52Honoured(func(k string) string { return env[k] }) {
		t.Errorf("inside screen OSC 52 should count")
	}
}
