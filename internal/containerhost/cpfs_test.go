package containerhost

import (
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/amin-jalali/termocode/internal/workspace"
)

func TestReadFile(t *testing.T) {
	h, d, c := newHost(t)
	c.WriteFile(t, "/app/src/main.go", []byte("package main\n"), 0o644)
	f := containerFS{h}
	for _, p := range []string{"src/main.go", "/app/src/main.go"} {
		data, err := f.ReadFile(t.Context(), p)
		if err != nil || string(data) != "package main\n" {
			t.Fatalf("ReadFile(%q) = %q, %v", p, data, err)
		}
	}
	if d.OpenStreams() != 0 {
		t.Fatal("a stream was left open")
	}
	last := d.Calls()[len(d.Calls())-1].Args
	if last[0] != "cp" || last[1] != "-L" || last[2] != "api-1:/app/src/main.go" || last[3] != "-" {
		t.Fatalf("cp argv = %q", last)
	}
}

func TestReadFileFollowsSymlink(t *testing.T) {
	h, _, c := newHost(t)
	c.WriteFile(t, "/app/real.txt", []byte("real"), 0o644)
	if err := os.Symlink("real.txt", c.Local("/app/link.txt")); err != nil {
		t.Skip("no symlinks:", err)
	}
	data, err := containerFS{h}.ReadFile(t.Context(), "link.txt")
	if err != nil || string(data) != "real" {
		t.Fatalf("ReadFile(link) = %q, %v", data, err)
	}
}

func TestReadFileErrors(t *testing.T) {
	h, d, c := newHost(t)
	c.Mkdir(t, "/app/dir")
	f := containerFS{h}
	if _, err := f.ReadFile(t.Context(), "missing.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := f.ReadFile(t.Context(), "dir"); !errors.Is(err, errIsDir) {
		t.Fatalf("dir: %v", err)
	}
	if _, err := f.ReadFile(t.Context(), "../etc/passwd"); !errors.Is(err, workspace.ErrOutsideRoot) {
		t.Fatalf("outside: %v", err)
	}
	if d.OpenStreams() != 0 {
		t.Fatal("a stream was left open")
	}
}
