package containerhost

import (
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/amin-jalali/termocode/internal/workspace"
)

func TestRename(t *testing.T) {
	h, _, c := newHost(t)
	c.WriteFile(t, "/app/a.txt", []byte("A"), 0o644)
	c.WriteFile(t, "/app/old.txt", []byte("old"), 0o644)
	f := containerFS{h}
	if err := f.Rename(t.Context(), "a.txt", "new dir/b.txt"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(c.Local("/app/new dir/b.txt")); err != nil || string(data) != "A" {
		t.Fatalf("moved = %q, %v", data, err)
	}
	if _, err := os.Stat(c.Local("/app/a.txt")); !os.IsNotExist(err) {
		t.Fatal("source still there")
	}
	// Over an existing file: replaced, like os.Rename.
	if err := f.Rename(t.Context(), "new dir/b.txt", "old.txt"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(c.Local("/app/old.txt")); string(data) != "A" {
		t.Fatalf("replaced = %q", data)
	}
	// Onto a folder: refused, nothing moved into it.
	c.WriteFile(t, "/app/x", nil, 0o644)
	if err := f.Rename(t.Context(), "x", "new dir"); err == nil {
		t.Fatal("rename onto a folder worked")
	}
	if _, err := os.Stat(c.Local("/app/new dir/x")); !os.IsNotExist(err) {
		t.Fatal("moved into the folder")
	}
	if err := f.Rename(t.Context(), "missing", "y"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
}

func TestRenameRefusesRootAndOutside(t *testing.T) {
	h, _, _ := newHost(t)
	f := containerFS{h}
	if err := f.Rename(t.Context(), ".", "x"); !errors.Is(err, workspace.ErrIsRoot) {
		t.Fatalf("root: %v", err)
	}
	if err := f.Rename(t.Context(), "x", "/app"); !errors.Is(err, workspace.ErrIsRoot) {
		t.Fatalf("onto root: %v", err)
	}
	if err := f.Rename(t.Context(), "x", "../y"); !errors.Is(err, workspace.ErrOutsideRoot) {
		t.Fatalf("outside: %v", err)
	}
}

func TestRenameWithoutShell(t *testing.T) {
	h, _, c := newHost(t)
	c.WriteFile(t, "/app/a", nil, 0o644)
	noShell(h)
	if err := (containerFS{h}).Rename(t.Context(), "a", "b"); !errors.Is(err, ErrNoShell) {
		t.Fatalf("err = %v", err)
	}
}
