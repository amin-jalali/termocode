package localhost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/amin-jalali/termocode/internal/workspace"
)

// The project folder is deleted while termocode runs in it: Cwd gives a
// workspace with no root (not a crash), and a relative root cannot be
// made absolute.
func TestCwdOfDeletedFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a folder in use cannot be deleted on Windows")
	}
	dir := filepath.Join(t.TempDir(), "proj")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	ws := Cwd()
	if ws.Kind != workspace.KindLocal || ws.Host == nil {
		t.Fatalf("Cwd = %+v", ws)
	}
	// Linux cannot name a deleted cwd; macOS still gives its old path.
	if runtime.GOOS != "linux" {
		return
	}
	if ws.Root != "" {
		t.Fatalf("Cwd = %+v", ws)
	}
	if _, err := New("sub"); err == nil {
		t.Fatal("a relative root was made absolute in a deleted folder")
	}
}

// A file where a folder is needed: write and rename say so and change
// nothing.
func TestWriteAndRenameUnderAFile(t *testing.T) {
	ws, dir := newWS(t)
	f, ctx := ws.Host.FS(), context.Background()
	if err := os.WriteFile(filepath.Join(dir, "notes"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.WriteFile(ctx, "notes/today.md", []byte("hi"), 0o644); err == nil {
		t.Fatal("wrote under a file")
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.Rename(ctx, "a.txt", "notes/a.txt"); err == nil {
		t.Fatal("renamed under a file")
	}
	if b, err := os.ReadFile(filepath.Join(dir, "a.txt")); err != nil || string(b) != "a" {
		t.Fatalf("a.txt = %q, %v", b, err)
	}
}

func TestRenameRefusesRoot(t *testing.T) {
	ws, dir := newWS(t)
	f, ctx := ws.Host.FS(), context.Background()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := f.Rename(ctx, ".", "sub/moved"); !errors.Is(err, workspace.ErrIsRoot) {
		t.Fatalf("rename root: %v", err)
	}
	if err := f.Rename(ctx, "sub", "."); !errors.Is(err, workspace.ErrIsRoot) {
		t.Fatalf("rename onto root: %v", err)
	}
	if err := f.Rename(ctx, "sub", "../out"); !errors.Is(err, workspace.ErrOutsideRoot) {
		t.Fatalf("rename out of root: %v", err)
	}
}
