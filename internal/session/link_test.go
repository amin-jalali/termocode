//go:build unix

package session

import (
	"net"
	"reflect"
	"strings"
	"testing"
)

func TestSeqFilter(t *testing.T) {
	f := newSeqFilter(linkPing, linkPong)
	out, found := f.Filter([]byte("a" + linkPing + "b" + linkPong + linkPing + "c"))
	if string(out) != "abc" || !reflect.DeepEqual(found, []int{0, 1, 0}) {
		t.Errorf("got %q %v", out, found)
	}
	// Split across reads: held back, then found.
	out, found = f.Filter([]byte("x" + linkPing[:5]))
	if string(out) != "x" || found != nil {
		t.Errorf("first half: %q %v", out, found)
	}
	out, found = f.Filter([]byte(linkPing[5:] + "y"))
	if string(out) != "y" || !reflect.DeepEqual(found, []int{0}) {
		t.Errorf("second half: %q %v", out, found)
	}
	// A prefix that turns out to be something else passes through whole.
	out, _ = f.Filter([]byte("\x1b_termo"))
	if string(out) != "" {
		t.Errorf("held prefix leaked: %q", out)
	}
	out, found = f.Filter([]byte("cat\x1b\\ \x1b[A"))
	if string(out) != "\x1b_termocat\x1b\\ \x1b[A" || found != nil {
		t.Errorf("not ours: %q %v", out, found)
	}
	// A lone Esc key or Alt+_ never waits for the next key.
	for _, k := range []string{"q\x1b", "\x1b_"} {
		if out, _ := f.Filter([]byte(k)); string(out) != k {
			t.Errorf("%q held back: %q", k, out)
		}
	}
}

// TestAttachLinkAnswersPing: a --link client announces itself, answers
// each ping on its output at once, and never passes a ping to the UI.
func TestAttachLinkAnswersPing(t *testing.T) {
	inputs := make(chan string, 16)
	s := newFakeServer(t, func(c *net.UnixConn) {
		for {
			typ, p, err := readFrame(c)
			if err != nil {
				return
			}
			if typ == frameInput {
				inputs <- string(p)
				if strings.Contains(string(p), "q") {
					_ = writeFrame(c, frameExit, encodeCode(0))
				}
			}
		}
	})
	keys, out, code := attachWith(t, s, AttachOpts{Link: true})
	waitFor(t, "the link hello", func() bool { return strings.HasPrefix(readOut(out), linkHello) })
	_, _ = keys.WriteString(linkPing)
	waitFor(t, "the pong", func() bool { return strings.Contains(readOut(out), linkPong) })
	_, _ = keys.WriteString("a" + linkPing + "q")
	waitCode(t, code)
	if n := strings.Count(readOut(out), linkPong); n != 2 {
		t.Errorf("%d pongs, want 2", n)
	}
	close(inputs)
	var got string
	for k := range inputs {
		got += k
	}
	if got != "aq" {
		t.Errorf("the UI got %q, want the keys without pings", got)
	}
}

// TestAttachNoLinkNoHello: a plain client prints no link hello and passes
// a ping through as it came (it is not at the end of termocode ssh).
func TestAttachNoLinkNoHello(t *testing.T) {
	inputs := make(chan string, 4)
	s := newFakeServer(t, func(c *net.UnixConn) {
		typ, p, _ := readFrame(c)
		if typ == frameInput {
			inputs <- string(p)
		}
		_ = writeFrame(c, frameExit, encodeCode(0))
	})
	keys, out, code := attachWith(t, s, AttachOpts{})
	_, _ = keys.WriteString(linkPing)
	waitCode(t, code)
	if strings.Contains(readOut(out), "termocode:link") {
		t.Errorf("plain client announced a link")
	}
	if got := <-inputs; got != linkPing {
		t.Errorf("input %q", got)
	}
}
