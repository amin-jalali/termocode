//go:build unix

package session

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var b bytes.Buffer
	if err := writeFrame(&b, frameHello, encodeSize(120, 36)); err != nil {
		t.Fatal(err)
	}
	if err := writeFrame(&b, frameOutput, []byte("héllo")); err != nil {
		t.Fatal(err)
	}
	if err := writeFrame(&b, frameExit, encodeCode(-3)); err != nil {
		t.Fatal(err)
	}
	typ, p, err := readFrame(&b)
	if err != nil || typ != frameHello {
		t.Fatalf("hello: %c %v", typ, err)
	}
	if c, r, _ := decodeSize(p); c != 120 || r != 36 {
		t.Errorf("size %dx%d", c, r)
	}
	if typ, p, _ = readFrame(&b); typ != frameOutput || string(p) != "héllo" {
		t.Errorf("output %c %q", typ, p)
	}
	if typ, p, _ = readFrame(&b); typ != frameExit || decodeCode(p) != -3 {
		t.Errorf("exit %c %d", typ, decodeCode(p))
	}
}

func TestFrameTooBig(t *testing.T) {
	hdr := []byte{frameOutput, 0xff, 0xff, 0xff, 0xff}
	if _, _, err := readFrame(bytes.NewReader(hdr)); err != errFrameTooBig {
		t.Errorf("err = %v, want errFrameTooBig", err)
	}
}

func TestKeyStable(t *testing.T) {
	a, b := Key("/tmp/x/../y"), Key("/tmp/y/")
	if a != b || len(a) != 16 {
		t.Errorf("Key not stable: %q %q", a, b)
	}
	if Key("/tmp/y") == Key("/tmp/z") {
		t.Error("different folders share a key")
	}
}

func TestDirIsPrivate(t *testing.T) {
	rt := shortTemp(t)
	t.Setenv("XDG_RUNTIME_DIR", rt)
	p, err := PathsFor(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(p.Socket) != filepath.Join(rt, "termocode") {
		t.Errorf("socket %s not under XDG_RUNTIME_DIR", p.Socket)
	}
	fi, err := os.Stat(filepath.Dir(p.Socket))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v, want 0700", fi.Mode().Perm())
	}
}

func TestDirFallbackAndLongPath(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	cache := shortTemp(t)
	t.Setenv("XDG_CACHE_HOME", cache)
	d, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if d != filepath.Join(cache, "termocode", "sessions") {
		t.Errorf("fallback dir %s", d)
	}
	t.Setenv("XDG_CACHE_HOME", filepath.Join(cache, strings.Repeat("d", 120)))
	if d, err = Dir(); err != nil {
		t.Fatal(err)
	}
	if len(d)+22 > maxSocketPath {
		t.Errorf("dir %s too long for a socket", d)
	}
}

// TestPeerUID: a connection from this process passes the check for our
// uid and fails it for any other uid.
func TestPeerUID(t *testing.T) {
	sock := filepath.Join(shortTemp(t), "p.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := net.Dial("unix", sock)
		if err == nil {
			defer c.Close()
			buf := make([]byte, 1)
			_, _ = c.Read(buf)
		}
	}()
	c, err := ln.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := checkPeer(c, os.Getuid()); err != nil {
		t.Errorf("own uid refused: %v", err)
	}
	if err := checkPeer(c, os.Getuid()+1); err == nil {
		t.Error("foreign uid accepted")
	}
}

// shortTemp is a temp dir with a short path: unix socket paths are
// limited to ~100 bytes and t.TempDir includes the test name.
func shortTemp(t *testing.T) string {
	t.Helper()
	// /tmp, not $TMPDIR: macOS's $TMPDIR (/var/folders/…/T/) is so long
	// that socket paths under it pass maxSocketPath.
	d, err := os.MkdirTemp("/tmp", "tcs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}
