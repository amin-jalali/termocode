package containerhost

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
)

func names(ents []fs.DirEntry) string {
	var s []string
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() {
			n += "/"
		}
		if e.Type()&fs.ModeSymlink != 0 {
			n += "@"
		}
		s = append(s, n)
	}
	return strings.Join(s, "|")
}

func TestReadDir(t *testing.T) {
	h, _, c := newHost(t)
	c.WriteFile(t, "/app/b.txt", []byte("bb"), 0o644)
	c.WriteFile(t, "/app/.env", nil, 0o600)
	c.WriteFile(t, "/app/..x", nil, 0o600)
	c.Mkdir(t, "/app/a dir")
	_ = os.Symlink("b.txt", c.Local("/app/link"))
	ents, err := containerFS{h}.ReadDir(t.Context(), ".")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(ents), "..x|.env|a dir/|b.txt|link@"; got != want {
		t.Fatalf("ReadDir = %q, want %q", got, want)
	}
	fi, err := ents[3].Info()
	if err != nil || fi.Size() != 2 || fi.Name() != "b.txt" {
		t.Fatalf("Info = %v, %v", fi, err)
	}
	ents, err = containerFS{h}.ReadDir(t.Context(), "a dir")
	if err != nil || len(ents) != 0 {
		t.Fatalf("empty dir = %v, %v", ents, err)
	}
}

func TestReadDirErrors(t *testing.T) {
	h, _, c := newHost(t)
	c.WriteFile(t, "/app/f", nil, 0o644)
	if _, err := (containerFS{h}).ReadDir(t.Context(), "nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := (containerFS{h}).ReadDir(t.Context(), "f"); err == nil {
		t.Fatal("ReadDir of a file worked")
	}
}

func TestReadDirWithoutShellUsesTheTar(t *testing.T) {
	h, d, c := newHost(t)
	c.WriteFile(t, "/app/b.txt", []byte("bb"), 0o644)
	c.WriteFile(t, "/app/sub/deep.txt", nil, 0o644)
	c.WriteFile(t, "/app/.hidden", nil, 0o644)
	c.NoShell()
	h.mu.Lock()
	h.caps.HasShell = false
	h.mu.Unlock()
	before := len(c.Execs())
	ents, err := containerFS{h}.ReadDir(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(ents), ".hidden|b.txt|sub/"; got != want {
		t.Fatalf("ReadDir = %q, want %q", got, want)
	}
	if fi, _ := ents[1].Info(); fi.Size() != 2 {
		t.Fatalf("Info = %v", fi)
	}
	if len(c.Execs()) != before || d.OpenStreams() != 0 {
		t.Fatal("exec used or stream left open")
	}
	if _, err := (containerFS{h}).ReadDir(t.Context(), "b.txt"); err == nil {
		t.Fatal("ReadDir of a file worked")
	}
}
