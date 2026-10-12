//go:build unix

package session

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testConn returns both ends of a fresh unix socket connection.
func testConn(t *testing.T) (server, client *net.UnixConn) {
	t.Helper()
	dir, err := os.MkdirTemp("", "tcs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(dir, "s.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	client, err = net.DialUnix("unix", nil, ln.Addr().(*net.UnixAddr))
	if err != nil {
		t.Fatal(err)
	}
	server, err = ln.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close(); _ = client.Close() })
	return server, client
}

// readUntil reads frames from c until want returns true for one of them
// or the connection ends. It returns all output bytes seen.
func readUntil(t *testing.T, c *net.UnixConn, want func(typ byte, p []byte) bool) (string, bool) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	var out strings.Builder
	for {
		typ, p, err := readFrame(c)
		if err != nil {
			return out.String(), false
		}
		if typ == frameOutput {
			out.Write(p)
		}
		if want(typ, p) {
			return out.String(), true
		}
	}
}

// TestServerDetachRequest: the UI writes DetachSeq; the client gets its
// terminal reset and the "detached" message, never the sequence itself,
// and the session keeps running for the next client.
func TestServerDetachRequest(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "detached")
	// The UI: draw, ask for a detach, then wait for a file before drawing
	// again (so the second client sees fresh output).
	script := `trap '' USR1; printf '\033[?1049hhello'; printf '\033_termocode:detach\033\\'; ` +
		`while [ ! -e "` + marker + `" ]; do sleep 0.02; done; printf 'again'; sleep 30`
	s := &Server{
		paths: Paths{Workspace: dir, Info: filepath.Join(dir, "info.json")},
		uiCmd: func() *exec.Cmd { return exec.Command("sh", "-c", script) },
		modes: NewModes(), started: time.Now(), done: make(chan int, 1),
	}
	t.Cleanup(func() {
		s.mu.Lock()
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		s.mu.Unlock()
	})

	srv, cli := testConn(t)
	if err := s.attach(srv, 80, 24); err != nil {
		t.Fatal(err)
	}
	var msg string
	out, ok := readUntil(t, cli, func(typ byte, p []byte) bool {
		msg = string(p)
		return typ == frameDetached
	})
	if !ok {
		t.Fatalf("no detached frame; output %q", out)
	}
	if !strings.Contains(msg, "termocode attach") {
		t.Errorf("detached message %q does not say how to go back", msg)
	}
	if strings.Contains(out, "termocode:detach") {
		t.Errorf("detach request reached the client terminal: %q", out)
	}
	if !strings.Contains(out, "hello") || !strings.HasSuffix(out, "\x1b[?1049l") {
		t.Errorf("want UI output then a terminal reset, got %q", out)
	}
	s.mu.Lock()
	gone := s.client == nil
	s.mu.Unlock()
	if !gone {
		t.Errorf("client still attached after a detach request")
	}

	// A new client attaches to the same, still running UI.
	srv2, cli2 := testConn(t)
	if err := s.attach(srv2, 80, 24); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	out, ok = readUntil(t, cli2, func(typ byte, p []byte) bool {
		return typ == frameOutput && strings.Contains(string(p), "again")
	})
	if !ok {
		t.Fatalf("second client got no output from the same UI: %q", out)
	}
	if !strings.HasPrefix(out, "\x1b[?1049h") {
		t.Errorf("modes not replayed to the second client: %q", out)
	}
}
