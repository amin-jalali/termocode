//go:build unix

package session

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/termfont"
)

// attachDropped runs Attach against a server that hangs up right after
// the hello (after sending output, if any), and returns what the client
// printed. serverAlive holds the workspace lock, as a running server does.
func attachDropped(t *testing.T, serverAlive bool, output string) string {
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
		if output != "" {
			_ = writeFrame(c, frameOutput, []byte(output))
		}
		_ = c.Close() // dropped, e.g. as a client that stopped reading
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
	got := attachDropped(t, true, "")
	if strings.Contains(got, "lost the session server") || !strings.Contains(got, "termocode attach") {
		t.Errorf("live server, got %q", got)
	}
	got = attachDropped(t, false, "")
	if !strings.Contains(got, "lost the session server") {
		t.Errorf("dead server, got %q", got)
	}
}

// TestAttachLostRestoresFont: when the server dies with the UI's font
// change in place, the client undoes it (the server's reset never comes).
func TestAttachLostRestoresFont(t *testing.T) {
	got := attachDropped(t, false, "\x1b[?1049h"+termfont.Request(termfont.Kitty, -2, true))
	if !strings.Contains(got, termfont.Request(termfont.Kitty, 2, false)) {
		t.Errorf("lost server, font not restored: %q", got)
	}
	got = attachDropped(t, false, termfont.Request(termfont.Kitty, -2, true)+termfont.Request(termfont.Kitty, 2, false))
	if strings.Count(got, "kitty-cmd") != 2 {
		t.Errorf("font already restored by the UI, restored again: %q", got)
	}
}
