//go:build linux || darwin

package termimg

import (
	"bytes"
	"testing"
	"time"

	"github.com/creack/pty"
)

// probeWithReply runs Probe on a pty whose "terminal" answers reply once
// the query arrives.
func probeWithReply(t *testing.T, envm map[string]string, reply string) Caps {
	t.Helper()
	ptm, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	defer ptm.Close()
	defer tty.Close()
	go func() {
		var got []byte
		buf := make([]byte, 256)
		for !bytes.Contains(got, []byte("\x1b[c")) {
			n, err := ptm.Read(buf)
			if err != nil {
				return
			}
			got = append(got, buf[:n]...)
		}
		_, _ = ptm.WriteString(reply)
	}()
	return Probe(tty, tty, 2*time.Second, env(envm))
}

// A terminal that names itself kitty (env) but answers the graphics
// query with an error, e.g. a terminal the client reattached from
// through a layer without kitty graphics, cannot show kitty images: the
// explicit error wins over the environment guess.
func TestProbeKittyErrorReply(t *testing.T) {
	kittyEnv := map[string]string{"TERM": "xterm-kitty"}
	cases := []struct {
		name  string
		envm  map[string]string
		reply string
		want  Protocol
	}{
		{"ok", kittyEnv, "\x1b_Gi=31;OK\x1b\\\x1b[?62;22c", Kitty},
		{"enoent", kittyEnv, "\x1b_Gi=31;ENOENT:No such image\x1b\\\x1b[?62;22c", Blocks},
		{"einval sixel", kittyEnv, "\x1b_Gi=31;EINVAL:bad\x1b\\\x1b[?62;4;22c", Sixel},
		// DA1 but no kitty answer: not kitty, whatever the environment
		// says (kitty, Ghostty and WezTerm always answer before DA1). A
		// KITTY_WINDOW_ID leaks into VS Code's or JetBrains' terminal
		// started from a kitty shell; sending kitty images there prints
		// garbage. (terminal R8)
		{"no kitty answer", kittyEnv, "\x1b[?62;22c", Blocks},
		{"wt env, DA1 with sixel", map[string]string{"WT_SESSION": "x"}, "\x1b[?61;4;6c", Sixel},
		{"wt env, old WT without sixel", map[string]string{"WT_SESSION": "x"}, "\x1b[?61;6c", Blocks},
		{"error no env", nil, "\x1b_Gi=31;ENOENT:x\x1b\\\x1b[?62;22c", Blocks},
	}
	for _, c := range cases {
		if got := probeWithReply(t, c.envm, c.reply); got.Protocol != c.want {
			t.Errorf("%s: protocol %v (%s), want %v", c.name, got.Protocol, got.Reason, c.want)
		}
	}
}

// A terminal that does not answer at all keeps the environment's guess.
func TestProbeNoAnswerKeepsEnv(t *testing.T) {
	ptm, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	defer ptm.Close()
	defer tty.Close()
	go func() { _, _ = ptm.Read(make([]byte, 256)) }()
	got := Probe(tty, tty, 150*time.Millisecond, env(map[string]string{"TERM": "xterm-kitty"}))
	if got.Protocol != Kitty {
		t.Errorf("protocol %v (%s), want Kitty", got.Protocol, got.Reason)
	}
}
