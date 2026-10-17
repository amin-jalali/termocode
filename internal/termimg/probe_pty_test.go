//go:build linux || darwin

package termimg

import (
	"bytes"
	"strings"
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

// Terminal R1: the probe also asks for the kitty keyboard flags (CSI ? u).
// Only a terminal that answers gets the protocol turned on.
func TestProbeKittyKeyboard(t *testing.T) {
	if got := probeWithReply(t, nil, "\x1b[?0u\x1b[?62;22c"); !got.KittyKeys {
		t.Errorf("answered CSI ? u: KittyKeys = false (%s)", got.Reason)
	}
	if got := probeWithReply(t, nil, "\x1b[?62;22c"); got.KittyKeys {
		t.Error("no CSI ? u answer: KittyKeys = true")
	}
}

// With images off the keyboard question is asked on its own.
func TestProbeKeysOnly(t *testing.T) {
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
		if bytes.Contains(got, []byte("\x1b_G")) {
			_, _ = ptm.WriteString("unexpected graphics query")
		}
		_, _ = ptm.WriteString("\x1b[?1u\x1b[?62c")
	}()
	if keys, answered := ProbeKittyKeyboard(tty, tty, 2*time.Second, env(nil)); !keys || !answered {
		t.Errorf("answered CSI ? u: keys=%v answered=%v", keys, answered)
	}
}

// probeRecord runs ProbeWith on a pty whose "terminal" answers DA1 (when
// answer is true) and returns what the probe wrote.
func probeRecord(t *testing.T, envm map[string]string, o ProbeOpts, answer bool) (Caps, string) {
	t.Helper()
	ptm, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	defer ptm.Close()
	defer tty.Close()
	got := make(chan []byte, 1)
	go func() {
		var b []byte
		buf := make([]byte, 256)
		for !bytes.Contains(b, []byte("\x1b[c")) {
			n, err := ptm.Read(buf)
			if err != nil {
				break
			}
			b = append(b, buf[:n]...)
		}
		if answer {
			_, _ = ptm.WriteString("\x1b[?62;22c")
		}
		got <- b
	}()
	caps := ProbeWith(tty, tty, 300*time.Millisecond, env(envm), o)
	select {
	case b := <-got:
		return caps, string(b)
	case <-time.After(2 * time.Second):
		return caps, ""
	}
}

// Terminal R3: the kitty graphics query is an APC string. A terminal that
// does not know APC prints it ("Gi=31,s=1…" flashes at start), so it goes
// out only when the environment names a kitty-graphics terminal or the
// terminal answered the probe on an earlier run.
func TestProbeKittyQueryOnlyWhenHinted(t *testing.T) {
	cases := []struct {
		name  string
		envm  map[string]string
		o     ProbeOpts
		kitty bool
	}{
		{"unknown terminal, first run", nil, ProbeOpts{}, false},
		{"unknown terminal that answered before", nil, ProbeOpts{AnsweredBefore: true}, true},
		{"kitty env", map[string]string{"TERM": "xterm-kitty"}, ProbeOpts{}, true},
		{"ghostty env", map[string]string{"TERM_PROGRAM": "ghostty"}, ProbeOpts{}, true},
		{"vte env", map[string]string{"VTE_VERSION": "7800"}, ProbeOpts{}, false},
	}
	for _, c := range cases {
		caps, sent := probeRecord(t, c.envm, c.o, true)
		if got := strings.Contains(sent, "\x1b_G"); got != c.kitty {
			t.Errorf("%s: kitty query sent = %v, want %v (%q)", c.name, got, c.kitty, sent)
		}
		if !strings.Contains(sent, "\x1b[16t") || !strings.HasSuffix(sent, "\x1b[c") {
			t.Errorf("%s: cell size + DA1 missing: %q", c.name, sent)
		}
		if !caps.Answered {
			t.Errorf("%s: DA1 answer not recorded", c.name)
		}
	}
	if caps, _ := probeRecord(t, nil, ProbeOpts{}, false); caps.Answered {
		t.Error("silent terminal: Answered = true")
	}
}
