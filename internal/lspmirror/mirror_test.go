package lspmirror

import (
	"archive/tar"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type entry struct {
	name string
	body string
	typ  byte
}

func tarOf(t *testing.T, entries ...entry) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		hd := &tar.Header{Name: e.name, Mode: 0o644, Typeflag: e.typ, Size: int64(len(e.body))}
		if e.typ == tar.TypeDir {
			hd.Size, hd.Mode = 0, 0o755
		}
		if e.typ == tar.TypeSymlink {
			hd.Size, hd.Linkname = 0, "/etc/passwd"
		}
		if err := tw.WriteHeader(hd); err != nil {
			t.Fatal(err)
		}
		if hd.Size > 0 {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf
}

func TestFillCopiesCodeAndSkipsHeavyFolders(t *testing.T) {
	m := New(t.TempDir(), "ws1", "/app")
	st, err := m.Fill(tarOf(t,
		entry{"app", "", tar.TypeDir},
		entry{"app/main.go", "package main\n", tar.TypeReg},
		entry{"app/pkg/a.go", "package pkg\n", tar.TypeReg},
		entry{"app/node_modules/x/index.js", "x", tar.TypeReg},
		entry{"app/web/node_modules/y.js", "y", tar.TypeReg},
		entry{"app/.git/objects/ab/cd", "obj", tar.TypeReg},
		entry{"app/.git/HEAD", "ref: main\n", tar.TypeReg},
		entry{"app/link", "", tar.TypeSymlink},
		entry{"app/../../escape.txt", "no", tar.TypeReg},
	))
	if err != nil {
		t.Fatal(err)
	}
	if st.Files != 3 {
		t.Errorf("files = %d, want 3 (main.go, pkg/a.go, .git/HEAD)", st.Files)
	}
	for _, rel := range []string{"main.go", "pkg/a.go", ".git/HEAD"} {
		if _, err := os.Stat(filepath.Join(m.Dir, rel)); err != nil {
			t.Errorf("%s missing: %v", rel, err)
		}
	}
	for _, rel := range []string{"node_modules", "web/node_modules", ".git/objects", "link"} {
		if _, err := os.Lstat(filepath.Join(m.Dir, rel)); err == nil {
			t.Errorf("%s must be left out", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(m.Dir)), "escape.txt")); err == nil {
		t.Error("a name with .. left the mirror")
	}
	fi, err := os.Stat(m.Dir)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("mirror folder mode = %v, %v; want 0700", fi.Mode().Perm(), err)
	}
}

func TestFillStopsAtCap(t *testing.T) {
	m := New(t.TempDir(), "ws", "/app")
	m.Cap = 10
	_, err := m.Fill(tarOf(t,
		entry{"app/a.txt", "123456", tar.TypeReg},
		entry{"app/b.txt", "123456", tar.TypeReg},
	))
	if !errors.Is(err, ErrCap) {
		t.Fatalf("Fill = %v, want ErrCap", err)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "a.txt")); err != nil {
		t.Error("what fit must stay")
	}
}

func TestLocalPutRemove(t *testing.T) {
	m := New(t.TempDir(), "ws", "/app")
	if err := m.Put("/app/x.go", []byte("x")); err != nil {
		t.Fatalf("Put before Fill = %v, want a no-op", err)
	}
	if _, err := os.Stat(m.Dir); err == nil {
		t.Fatal("Put must not create a mirror that was never filled")
	}
	if _, err := m.Fill(tarOf(t, entry{"app", "", tar.TypeDir})); err != nil {
		t.Fatal(err)
	}
	if p, ok := m.Local("/app/sub/x.go"); !ok || p != filepath.Join(m.Dir, "sub", "x.go") {
		t.Errorf("Local = %q, %v", p, ok)
	}
	for _, out := range []string{"/etc/passwd", "/apple/x", "/app/node_modules/x.js"} {
		if _, ok := m.Local(out); ok {
			t.Errorf("Local(%q) must be false", out)
		}
	}
	if err := m.Put("/app/sub/x.go", []byte("package sub\n")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(m.Dir, "sub", "x.go"))
	if err != nil || string(b) != "package sub\n" {
		t.Errorf("after Put: %q, %v", b, err)
	}
	if st := m.Stats(); st.Files != 1 || st.Bytes != 12 {
		t.Errorf("Stats = %+v", st)
	}
	if err := m.Remove("/app/sub/x.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "sub", "x.go")); err == nil {
		t.Error("Remove left the file")
	}
	if err := m.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.Dir); err == nil {
		t.Error("Clear left the folder")
	}
}

func TestPruneDropsOldestButKeepsCurrent(t *testing.T) {
	base := t.TempDir()
	mk := func(name string, size int, age time.Duration) string {
		d := filepath.Join(base, name)
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "f"), make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
		when := time.Now().Add(-age)
		_ = os.Chtimes(d, when, when)
		return d
	}
	oldest := mk("a", 100, 3*time.Hour)
	keep := mk("b", 100, 2*time.Hour)
	newest := mk("c", 100, time.Hour)
	if err := Prune(base, 150, keep); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldest); err == nil {
		t.Error("the oldest mirror must go")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("the current mirror must stay")
	}
	if _, err := os.Stat(newest); err == nil {
		t.Error("still over the limit: the next oldest (not kept) must go")
	}
}
