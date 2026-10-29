package fetch

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

type tarEntry struct {
	name, link, body string
	typ              byte
	mode             int64
}

// writeTarGz builds a .tar.gz from entries in a fresh file under dir.
func writeTarGz(t *testing.T, dir string, entries []tarEntry) string {
	t.Helper()
	p := filepath.Join(dir, "a.tar.gz")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		h := &tar.Header{Name: e.name, Typeflag: e.typ, Linkname: e.link, Mode: mode, Size: int64(len(e.body))}
		if e.typ != tar.TypeReg {
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// A link to "." followed by a link "through" it to ".." looks inside dest
// when checked by name only, but on disk it points at dest's parent. A
// later file entry must not land outside dest.
func TestExtractTarGzRefusesChainedSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out")
	tp := writeTarGz(t, dir, []tarEntry{
		{name: "d", typ: tar.TypeSymlink, link: "."},
		{name: "d/e", typ: tar.TypeSymlink, link: ".."},
		{name: "d/e/escaped.txt", typ: tar.TypeReg, body: "x"},
	})
	err := ExtractTarGz(tp, dest, nil)
	if _, statErr := os.Lstat(filepath.Join(dir, "escaped.txt")); statErr == nil {
		t.Fatal("a file was written outside dest")
	}
	if err == nil {
		t.Fatal("want an error for a link that escapes dest")
	}
}
