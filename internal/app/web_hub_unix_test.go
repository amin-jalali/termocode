//go:build unix

package app

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// webTestDir is a short private temp dir for hub sockets (macOS sun_path
// is 104 bytes and its $TMPDIR is long).
func webTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if len(dir)+len("/0123456789abcdef.web.sock") > 100 {
		dir2, err := os.MkdirTemp("/tmp", "tw")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir2) })
		dir = dir2
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Spec (WEB-6, T51): the hub socket is <key>.web.sock in the session dir;
// a socket file left by a crashed run is removed on the next start, a live
// one is "busy", and a file that is not a socket is never touched.
func TestStaleHubSocketRemoved(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", webTestDir(t))
	path, err := webHubSocketPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(path) != ".sock" || filepath.Ext(path[:len(path)-len(".sock")]) != ".web" {
		t.Fatalf("hub socket %q is not <key>.web.sock", path)
	}
	stale, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	stale.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = stale.Close()

	ln, err := webHubListen(path)
	if err != nil {
		t.Fatalf("stale socket blocked the start: %v", err)
	}
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket %v, %v: want a 0600 socket", fi, err)
	}
	if _, err := webHubListen(path); !errors.Is(err, errWebHubBusy) {
		t.Fatalf("second listener: %v, want busy", err)
	}
	_ = ln.Close()

	plain := filepath.Join(filepath.Dir(path), "plain.web.sock")
	if err := os.WriteFile(plain, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := webHubListen(plain); err == nil {
		t.Fatal("listened over a plain file")
	}
	if b, err := os.ReadFile(plain); err != nil || string(b) != "keep" {
		t.Fatalf("plain file touched: %q %v", b, err)
	}
}
