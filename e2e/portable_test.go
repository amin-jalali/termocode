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
