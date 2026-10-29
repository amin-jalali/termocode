//go:build linux || darwin

package termimg

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/creack/pty"
)

// fakeTerminal is a pty whose far side runs answer once the probe's DA1
// query arrives.
func fakeTerminal(t *testing.T, answer func(ptm *os.File)) (tty *os.File) {
	t.Helper()
	ptm, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	t.Cleanup(func() { ptm.Close(); tty.Close() })
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
		answer(ptm)
	}()
	return tty
}

func TestProbeReadsTheCellSize(t *testing.T) {
	c := probeWithReply(t, nil, "\x1b[6;20;10t\x1b[?62;22c")
	if c.CellW != 10 || c.CellH != 20 {
		t.Errorf("cell %dx%d, want 10x20", c.CellW, c.CellH)
	}
	for _, odd := range []string{
		"\x1b[6;x;10t\x1b[?62c", // not a number
		"\x1b[6;0;10t\x1b[?62c", // zero height
		"\x1b[6;20t\x1b[?62c",   // one number
		"\x1b[6;20;10\x1b[?62c", // no final t before DA1… and none after
	} {
		if c := probeWithReply(t, nil, odd); c.CellW != 0 || c.CellH != 0 {
			t.Errorf("odd answer %q gave cell %dx%d", odd, c.CellW, c.CellH)
		}
	}
}

func TestProbeIgnoresOddKittyAnswers(t *testing.T) {
	kittyEnv := map[string]string{"TERM": "xterm-kitty"}
	for name, reply := range map[string]string{
		"another image id":  "\x1b_Gi=99;OK\x1b\\\x1b[?62;22c",
		"no message":        "\x1b_Gi=31\x1b\\\x1b[?62;22c",
		"no end of the APC": "\x1b_Gi=31;OK\x1b[?62;22c",
		"noise before DA1":  "\x1b[?5n\x1b[?62;22c",
	} {
		c := probeWithReply(t, kittyEnv, reply)
		if c.Protocol != Blocks || !c.Answered {
			t.Errorf("%s: protocol %v answered %v (%s), want blocks after a DA1 without kitty OK", name, c.Protocol, c.Answered, c.Reason)
		}
	}
}

// A terminal that hangs up in the middle of the probe: the probe ends at
// once (no wait for the timeout) and keeps the environment's guess.
func TestProbeTerminalThatHangsUp(t *testing.T) {
	tty := fakeTerminal(t, func(ptm *os.File) {
		_, _ = ptm.WriteString("\x1b[6;")
		ptm.Close()
	})
	start := time.Now()
	c := Probe(tty, tty, 5*time.Second, env(map[string]string{"TERM": "xterm-kitty"}))
	if c.Protocol != Kitty || c.Answered {
		t.Errorf("hung-up terminal: protocol %v answered %v, want the kitty env guess", c.Protocol, c.Answered)
	}
	if time.Since(start) > 4*time.Second {
		t.Error("the probe waited for the whole timeout after the terminal hung up")
	}
}

func TestProbeWithoutATerminal(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("TMUX", "")
	t.Setenv("KITTY_WINDOW_ID", "")
	c := ProbeWith(nil, nil, time.Second, nil, ProbeOpts{})
	if c.Protocol != Kitty || c.Answered {
		t.Errorf("no TTY: %+v, want the environment's kitty and no answer", c)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if got := Query(r, w, "\x1b[c", time.Second, hasDA1); got != nil {
		t.Errorf("Query on a pipe = %q, want nil", got)
	}
	if got := Query(nil, w, "\x1b[c", time.Second, hasDA1); got != nil {
		t.Errorf("Query with no input = %q", got)
	}
	if c := ProbeWith(r, w, time.Second, env(nil), ProbeOpts{}); c.Answered {
		t.Error("a pipe answered the probe")
	}
}

func TestProbeTerminalAsksNothingInsideTmux(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
	tty := fakeTerminal(t, func(ptm *os.File) { _, _ = ptm.WriteString("\x1b[?1u\x1b[?62c") })
	c := ProbeTerminal(tty, tty, 200*time.Millisecond, nil)
	if c.KittyKeys || c.Answered || c.Protocol != Blocks {
		t.Errorf("inside tmux: %+v, want no questions asked", c)
	}
}

func TestQueryStopsAtTheTimeoutWithoutAnAnswer(t *testing.T) {
	tty := fakeTerminal(t, func(ptm *os.File) { _, _ = ptm.WriteString("\x1b[?") }) // half an answer
	start := time.Now()
	got := Query(tty, tty, "\x1b[c", 150*time.Millisecond, hasDA1)
	if hasDA1(got) {
		t.Errorf("half an answer counted as DA1: %q", got)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("Query took %v with a 150ms timeout", d)
	}
}
