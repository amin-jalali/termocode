package localhost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/workspace"
)

func newWS(t *testing.T) (workspace.Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	ws, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ws, dir
}

func TestNew(t *testing.T) {
	ws, dir := newWS(t)
	if ws.Kind != workspace.KindLocal || ws.Root != dir || ws.ID != "local:"+dir {
		t.Fatalf("ws = %+v", ws)
	}
	if ws.Host == nil || !ws.Host.Online() || ws.Host.Label() != "local" || !ws.Host.Caps().HasShell {
		t.Fatalf("host = %+v", ws.Host)
	}
	if ws.Host.Exec() == nil {
		t.Fatal("no Exec")
	}
	argv := []string{"bash", "-l"}
	if got := ws.Host.TermArgv(dir, argv); len(got) != 2 || got[0] != "bash" {
		t.Fatalf("TermArgv = %v", got)
	}
}

func TestNewRelativeRootIsMadeAbsolute(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	ws, err := New("sub/..")
	if err != nil || ws.Root != dir {
		t.Fatalf("New(rel) = %+v, %v", ws, err)
	}
}

func TestCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("PWD", dir)
	ws := Cwd()
	if ws.Root != dir || ws.ID != "local:"+dir {
		t.Fatalf("Cwd = %+v", ws)
	}
}

func TestFSOps(t *testing.T) {
	ws, dir := newWS(t)
	fs, ctx := ws.Host.FS(), context.Background()

	if err := fs.WriteFile(ctx, "a/b/c.txt", []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(ctx, filepath.Join(dir, "a/b/c.txt"))
	if err != nil || string(data) != "hi" {
		t.Fatalf("ReadFile = %q, %v", data, err)
	}
	fi, err := fs.Stat(ctx, "a/b/c.txt")
	if err != nil || fi.Size() != 2 || fi.IsDir() {
		t.Fatalf("Stat = %v, %v", fi, err)
	}
	if err := fs.Mkdir(ctx, "d/e", 0o755); err != nil {
		t.Fatal(err)
	}
	ents, err := fs.ReadDir(ctx, ".")
	if err != nil || len(ents) != 2 || ents[0].Name() != "a" || ents[1].Name() != "d" {
		t.Fatalf("ReadDir = %v, %v", ents, err)
	}
	if err := fs.Rename(ctx, "a/b/c.txt", "d/e/moved.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "d/e/moved.txt")); err != nil {
		t.Fatal(err)
	}
	if err := fs.Remove(ctx, "d"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "d")); !os.IsNotExist(err) {
		t.Fatalf("d still there: %v", err)
	}
}

func TestWriteFileKeepsModeOfExistingFile(t *testing.T) {
	ws, dir := newWS(t)
	p := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(p, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ws.Host.FS().WriteFile(context.Background(), "run.sh", []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want 0755 kept", fi.Mode().Perm())
	}
}

func TestRemoveRefusesRoot(t *testing.T) {
	ws, dir := newWS(t)
	for _, p := range []string{"", ".", dir, "a/.."} {
		if err := ws.Host.FS().Remove(context.Background(), p); err == nil {
			t.Fatalf("Remove(%q) removed the root", p)
		}
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
}

func TestEscapesAreRefused(t *testing.T) {
	ws, dir := newWS(t)
	outside := filepath.Join(filepath.Dir(dir), "outside.txt")
	fs, ctx := ws.Host.FS(), context.Background()
	bad := []string{"../outside.txt", "a/../../outside.txt", outside, "/etc/passwd"}
	for _, p := range bad {
		if _, err := fs.Stat(ctx, p); !errors.Is(err, workspace.ErrOutsideRoot) {
			t.Errorf("Stat(%q) err = %v", p, err)
		}
		if _, err := fs.ReadDir(ctx, p); !errors.Is(err, workspace.ErrOutsideRoot) {
			t.Errorf("ReadDir(%q) err = %v", p, err)
		}
		if _, err := fs.ReadFile(ctx, p); !errors.Is(err, workspace.ErrOutsideRoot) {
			t.Errorf("ReadFile(%q) err = %v", p, err)
		}
		if err := fs.WriteFile(ctx, p, []byte("x"), 0o644); !errors.Is(err, workspace.ErrOutsideRoot) {
			t.Errorf("WriteFile(%q) err = %v", p, err)
		}
		if err := fs.Mkdir(ctx, p, 0o755); !errors.Is(err, workspace.ErrOutsideRoot) {
			t.Errorf("Mkdir(%q) err = %v", p, err)
		}
		if err := fs.Remove(ctx, p); !errors.Is(err, workspace.ErrOutsideRoot) {
			t.Errorf("Remove(%q) err = %v", p, err)
		}
		if err := fs.Rename(ctx, "x", p); !errors.Is(err, workspace.ErrOutsideRoot) {
			t.Errorf("Rename(x, %q) err = %v", p, err)
		}
		if err := fs.Rename(ctx, p, "x"); !errors.Is(err, workspace.ErrOutsideRoot) {
			t.Errorf("Rename(%q, x) err = %v", p, err)
		}
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatal("a file was written outside the root")
	}
}

// Symlink policy (same as the AI file tools today, ai.Workspace.Resolve):
// paths are checked by name only. A symlink inside the root is followed,
// even when it points outside; ".." can never climb out.
func TestSymlinkInsideRootIsFollowed(t *testing.T) {
	ws, dir := newWS(t)
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "f.txt"), []byte("out"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(dir, "link")); err != nil {
		t.Skip("no symlinks:", err)
	}
	data, err := ws.Host.FS().ReadFile(context.Background(), "link/f.txt")
	if err != nil || string(data) != "out" {
		t.Fatalf("ReadFile via symlink = %q, %v", data, err)
	}
}

func TestContextCanceled(t *testing.T) {
	ws, _ := newWS(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ws.Host.FS().Stat(ctx, "."); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stat on a canceled ctx = %v", err)
	}
}
