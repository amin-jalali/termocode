package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type tarEntry struct {
	name, body, link string
	typ              byte
}

// makeTarGz writes a .tar.gz with entries into dir and returns its path.
func makeTarGz(t *testing.T, dir string, entries []tarEntry) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Typeflag: typ, Mode: 0o755, Linkname: e.link}
		if typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
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
	p := filepath.Join(dir, "a.tar.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const binDir = "termocode-v0.2.0-linux-amd64"

func TestExtractBinary(t *testing.T) {
	dir := t.TempDir()
	tgz := makeTarGz(t, dir, []tarEntry{
		{name: binDir + "/", typ: tar.TypeDir},
		{name: binDir + "/termocode", body: "#!/bin/sh\necho hi\n"},
		{name: binDir + "/README.md", body: "readme"},
		{name: binDir + "/LICENSE", body: "mit"},
	})
	dest := filepath.Join(dir, "out")
	if err := ExtractBinary(tgz, binDir, dest, 1<<20); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "#!/bin/sh\necho hi\n" {
		t.Fatalf("got %q %v", got, err)
	}
	// Only the binary was written.
	if ents, _ := os.ReadDir(dir); len(ents) != 2 {
		t.Fatalf("extra files: %v", ents)
	}
}

func TestExtractBinaryRefuses(t *testing.T) {
	bin := binDir + "/termocode"
	for label, c := range map[string]struct {
		entries []tarEntry
		max     int64
	}{
		"dot-dot entry":  {entries: []tarEntry{{name: binDir + "/../x", body: "evil"}, {name: bin, body: "ok"}}},
		"dot-dot leaf":   {entries: []tarEntry{{name: "../x", body: "evil"}, {name: bin, body: "ok"}}},
		"absolute path":  {entries: []tarEntry{{name: "/etc/passwd", body: "evil"}, {name: bin, body: "ok"}}},
		"symlink binary": {entries: []tarEntry{{name: bin, typ: tar.TypeSymlink, link: "/bin/sh"}}},
		"hardlink":       {entries: []tarEntry{{name: bin, typ: tar.TypeLink, link: "/bin/sh"}}},
		"huge binary":    {entries: []tarEntry{{name: bin, body: strings.Repeat("x", 2048)}}, max: 1024},
		"no binary":      {entries: []tarEntry{{name: binDir + "/README.md", body: "x"}}},
		"wrong folder":   {entries: []tarEntry{{name: "other/termocode", body: "x"}}},
		"twice":          {entries: []tarEntry{{name: bin, body: "a"}, {name: bin, body: "b"}}},
	} {
		dir := t.TempDir()
		tgz := makeTarGz(t, dir, c.entries)
		max := c.max
		if max == 0 {
			max = 1 << 20
		}
		dest := filepath.Join(dir, "out")
		if err := ExtractBinary(tgz, binDir, dest, max); err == nil {
			t.Errorf("%s: want an error", label)
		}
		if _, err := os.Stat(dest); err == nil {
			t.Errorf("%s: a refused archive left %s", label, dest)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "x")); err == nil {
			t.Errorf("%s: wrote outside", label)
		}
	}
}

func TestExtractBinaryNotGzip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.tar.gz")
	_ = os.WriteFile(p, []byte("not gzip"), 0o600)
	if err := ExtractBinary(p, binDir, filepath.Join(dir, "out"), 1<<20); err == nil {
		t.Error("want an error")
	}
}
