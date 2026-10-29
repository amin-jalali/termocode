//go:build unix

package session

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeServer is a session server the test drives by hand: it holds the
// workspace lock (it "runs") and serves each accepted client with the
// next function of serve.
type fakeServer struct {
	p     Paths
	ln    *net.UnixListener
	hello chan []byte // the hello payload of every client
}

func newFakeServer(t *testing.T, serve ...func(c *net.UnixConn)) *fakeServer {
	t.Helper()
	dir, err := os.MkdirTemp("", "tcr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	p := Paths{Workspace: dir, Socket: filepath.Join(dir, "s.sock"), Lock: filepath.Join(dir, "s.lock")}
	lock, err := lockFile(p.Lock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: p.Socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	s := &fakeServer{p: p, ln: ln, hello: make(chan []byte, 8)}
	go func() {
		for _, f := range serve {
			c, err := ln.AcceptUnix()
			if err != nil {
				return
			}
			_, h, _ := readFrame(c)
			s.hello <- h
			f(c)
		}
	}()
	return s
}

// attachTo runs Attach (with reconnect) against s in the background and
// returns the key pipe, the output file and the exit code channel.
func attachTo(t *testing.T, s *fakeServer, caps *ClientCaps) (*os.File, string, <-chan int) {
	t.Helper()
	c, err := Dial(s.p)
	if err != nil {
		t.Fatal(err)
	}
	in, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close(); _ = inW.Close() })
	outPath := filepath.Join(s.p.Workspace, "out")
	out, err := os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = out.Close() })
	code := make(chan int, 1)
	go func() { code <- Attach(c, s.p, AttachOpts{In: in, Out: out, Caps: caps, Reconnect: true}) }()
	return inW, outPath, code
}

func waitCode(t *testing.T, code <-chan int) int {
	t.Helper()
	select {
	case c := <-code:
		return c
	case <-time.After(10 * time.Second):
		t.Fatal("Attach did not return")
		return 0
	}
}

func readOut(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

// TestAttachReconnectsToLiveServer: a live server that drops the client
// gets it back by itself: the screen stays, the offline line shows, and
// the client says hello again on a new connection.
func TestAttachReconnectsToLiveServer(t *testing.T) {
	s := newFakeServer(t,
		func(c *net.UnixConn) {
			_ = writeFrame(c, frameOutput, []byte("first-frame"))
			_ = c.Close() // dropped
		},
		func(c *net.UnixConn) {
			_ = writeFrame(c, frameOutput, []byte("second-frame"))
			_ = writeFrame(c, frameExit, encodeCode(7))
		},
	)
	_, out, code := attachTo(t, s, &ClientCaps{Env: map[string]string{"LANG": "C.UTF-8"}})
	if got := waitCode(t, code); got != 7 {
		t.Errorf("exit code %d, want the UI's 7", got)
	}
	got := readOut(out)
	for _, want := range []string{"first-frame", "Reconnecting… (attempt 1)", "second-frame"} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q: %q", want, got)
		}
	}
	if strings.Contains(got, "termocode attach") || strings.Contains(got, "\x1b[?1049l") {
		t.Errorf("client left the screen instead of reconnecting: %q", got)
	}
	if n := len(s.hello); n != 2 {
		t.Errorf("%d hellos, want 2 (the first attach and the reconnect)", n)
	}
}

// TestAttachOfflineCtrlCQuits: while the server cannot be reached, keys
// are not sent, and Ctrl+C leaves (the session keeps running).
func TestAttachOfflineCtrlCQuits(t *testing.T) {
	var s *fakeServer
	s = newFakeServer(t, func(c *net.UnixConn) {
		_ = s.ln.Close() // no more clients: every reconnect fails
		_ = c.Close()
	})
	keys, out, code := attachTo(t, s, nil)
	waitFor(t, "the offline line", func() bool { return strings.Contains(readOut(out), "Reconnecting") })
	_, _ = keys.WriteString("typed-offline")
	_, _ = keys.WriteString("\x03")
	if got := waitCode(t, code); got != 0 {
		t.Errorf("exit code %d, want 0", got)
	}
	if got := readOut(out); !strings.Contains(got, "session keeps running") || !strings.Contains(got, "\x1b[?1049l") {
		t.Errorf("Ctrl+C offline: %q", got)
	}
}

// TestAttachTakenOverDoesNotReconnect: another terminal took over; this
// client leaves as before.
func TestAttachTakenOverDoesNotReconnect(t *testing.T) {
	s := newFakeServer(t, func(c *net.UnixConn) {
		_ = writeFrame(c, frameDetached, []byte(msgTakenOver))
		_ = c.Close()
	})
	_, out, code := attachTo(t, s, nil)
	if got := waitCode(t, code); got != 0 {
		t.Errorf("exit code %d", got)
	}
	if got := readOut(out); strings.Contains(got, "Reconnecting") || !strings.Contains(got, "attached from another terminal") {
		t.Errorf("taken over: %q", got)
	}
}
