//go:build unix

package session

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// attachDropped runs Attach against a server that hangs up right after
// the hello, and returns what the client printed. serverAlive holds the
// workspace lock, as a running server does.
func attachDropped(t *testing.T, serverAlive bool) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tcc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	p := Paths{Workspace: dir, Socket: filepath.Join(dir, "s.sock"), Lock: filepath.Join(dir, "s.lock")}
	if serverAlive {
		lock, err := lockFile(p.Lock)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: p.Socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.AcceptUnix()
		if err != nil {
			return
		}
		_, _, _ = readFrame(c) // hello
		_ = c.Close()          // dropped, e.g. as a client that stopped reading
	}()
	c, err := Dial(p)
	if err != nil {
		t.Fatal(err)
	}
	in, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	defer inW.Close()
	out, err := os.Create(filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	Attach(c, p, in, out, nil)
	b, _ := os.ReadFile(out.Name())
	return string(b)
}

// TestAttachDroppedByLiveServer: a server that is still running and drops
// the client (it stopped reading for 5 s) is not "lost".
func TestAttachDroppedByLiveServer(t *testing.T) {
	got := attachDropped(t, true)
	if strings.Contains(got, "lost the session server") || !strings.Contains(got, "termocode attach") {
		t.Errorf("live server, got %q", got)
	}
	got = attachDropped(t, false)
	if !strings.Contains(got, "lost the session server") {
		t.Errorf("dead server, got %q", got)
	}
}
