//go:build unix

package session

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fallbackDir points Dir at its $TMPDIR fallback (the runtime dir path is
// too long for a socket) and returns the folder it will use.
func fallbackDir(t *testing.T) string {
	t.Helper()
	tmp, err := os.MkdirTemp("", "tcd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	t.Setenv("XDG_RUNTIME_DIR", "/"+strings.Repeat("x", 120))
	t.Setenv("TMPDIR", tmp)
	return filepath.Join(tmp, "termocode-"+strconv.Itoa(os.Getuid()))
}

func TestDirFallbackCreatesPrivateDir(t *testing.T) {
	want := fallbackDir(t)
	got, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Dir() = %s, want %s", got, want)
	}
	fi, err := os.Lstat(got)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Errorf("mode %v, want a 0700 dir", fi.Mode())
	}
	if _, err := Dir(); err != nil {
		t.Errorf("second Dir(): %v", err)
	}
}

// TestDirRefusesSymlink: in a shared /tmp another user could create the
// folder first, as a link to a place they control.
func TestDirRefusesSymlink(t *testing.T) {
	d := fallbackDir(t)
	if err := os.Symlink(t.TempDir(), d); err != nil {
		t.Fatal(err)
	}
	if _, err := Dir(); err == nil {
		t.Fatalf("Dir() accepted a symlink")
	}
}

func TestDirRefusesOpenMode(t *testing.T) {
	d := fallbackDir(t)
	if err := os.Mkdir(d, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Dir(); err == nil {
		t.Fatalf("Dir() accepted a dir with mode 0755")
	}
}

func TestDirRefusesFile(t *testing.T) {
	d := fallbackDir(t)
	if err := os.WriteFile(d, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Dir(); err == nil {
		t.Fatalf("Dir() accepted a file")
	}
}
