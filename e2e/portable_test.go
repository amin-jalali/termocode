//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The harness runs on Linux and macOS. macOS has a long $TMPDIR
// (/var/folders/…/T/) that is reached through a symlink (/var →
// /private/var); these tests play that on any system.

// A session socket under shortTempDir fits a unix socket path even when
// $TMPDIR is long (sun_path is 104 bytes on macOS).
func TestShortTempDirFitsSessionSocket(t *testing.T) {
	long := filepath.Join(t.TempDir(), strings.Repeat("x", 70))
	if err := os.MkdirAll(long, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", long)
	dir, err := shortTempDir("tce2e")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "termocode", "0123456789abcdef.mcp.sock")
	if len(sock) > 100 {
		t.Errorf("socket path is %d bytes, want at most 100: %s", len(sock), sock)
	}
}

// NewRepo gives the real path of the workspace: termocode's working
// folder is the resolved one, and the session key is made from it.
func TestNewRepoPathHasNoSymlink(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "tmp-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	t.Run("repo", func(t *testing.T) {
		t.Setenv("TMPDIR", link)
		ws := NewRepo(t)
		resolved, err := filepath.EvalSymlinks(ws)
		if err != nil {
			t.Fatal(err)
		}
		if ws != resolved {
			t.Errorf("NewRepo = %s, want the resolved path %s", ws, resolved)
		}
	})
}

// A button that acts on the press (Don't Save) can end termocode before
// Click sends the release. On macOS the PTY then refuses the write (EIO);
// a closed pipe plays that here. Click must not fail the test for it.
func TestClickAfterThePressEndedTheApp(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	tm := &Term{t: t, ptmx: w, exited: make(chan struct{})}
	go func() {
		buf := make([]byte, 64)
		_, _ = r.Read(buf) // the press
		_ = r.Close()      // the app is gone: no reader for the release
		close(tm.exited)
	}()
	tm.Click(3, 4)
}
