//go:build unix

package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDirWithoutRuntimeDir(t *testing.T) {
	// A short folder: macOS's long $TMPDIR would push the socket path
	// past the limit and Dir would fall back to the temp dir.
	home, err := os.MkdirTemp("/tmp", "tch")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", home)
	got, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".cache", "termocode", "sessions"); got != want {
		t.Fatalf("Dir() = %s, want %s", got, want)
	}
	t.Setenv("HOME", "")
	if _, err := Dir(); err == nil {
		t.Fatal("no home at all: want an error")
	}
	if _, err := PathsFor("."); err == nil {
		t.Fatal("PathsFor without a session dir: want an error")
	}
}

// A session dir another user owns is refused: they could read the
// sockets' traffic or put their own socket there.
func TestEnsurePrivateDirRefusesOtherOwner(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root owns /")
	}
	err := ensurePrivateDir("/")
	if err == nil || !strings.Contains(err.Error(), "not owned by this user") {
		t.Fatalf("err = %v", err)
	}
}

func TestEnsurePrivateDirCannotCreate(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateDir(filepath.Join(file, "a", "b")); err == nil {
		t.Error("parent is a file: want an error")
	}
	if os.Geteuid() == 0 {
		return
	}
	ro := filepath.Join(dir, "ro")
	if err := os.Mkdir(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
	if err := ensurePrivateDir(filepath.Join(ro, "termocode")); err == nil {
		t.Error("read-only parent: want an error")
	}
}

// A new dir gets mode 0700 even under a umask that takes owner bits.
func TestEnsurePrivateDirFixesUmask(t *testing.T) {
	d := filepath.Join(t.TempDir(), "s")
	old := unix.Umask(0o277)
	err := ensurePrivateDir(d)
	unix.Umask(old)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(d)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v %v", fi.Mode(), err)
	}
}

func TestCapsFileErrors(t *testing.T) {
	dir := t.TempDir()
	// A folder where the caps file goes: it cannot be read or replaced.
	p := filepath.Join(dir, "caps.json")
	if err := os.MkdirAll(filepath.Join(p, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCaps(p); err == nil {
		t.Error("ReadCaps of a folder: want an error")
	}
	if err := writeCaps(p, &ClientCaps{}); err == nil {
		t.Error("writeCaps over a folder: want an error")
	}
	if err := writeCaps(p, nil); err == nil {
		t.Error("removing a non-empty folder: want an error")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Errorf("temp file left: %d entries", len(ents))
	}
	if err := writeCaps(filepath.Join(dir, "no", "caps.json"), &ClientCaps{}); err == nil {
		t.Error("missing folder: want an error")
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCaps(bad); err == nil {
		t.Error("broken JSON: want an error")
	}
}

// Caps too big for one frame are left out of the hello: the size still
// goes, and the UI keeps what it has.
func TestEncodeHelloDropsHugeCaps(t *testing.T) {
	huge := &ClientCaps{Env: map[string]string{"TERM": strings.Repeat("x", maxFrame)}}
	b := encodeHello(120, 40, huge)
	cols, rows, caps, err := decodeHello(b)
	if err != nil || cols != 120 || rows != 40 || caps != nil {
		t.Fatalf("%d %d %+v %v", cols, rows, caps, err)
	}
}
