package fetch

import (
	"archive/tar"
	"archive/zip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractTarGzKeepsLinksInsideDest(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out")
	tp := writeTarGz(t, dir, []tarEntry{
		{name: "bin/tool-1.0", typ: tar.TypeReg, body: "x", mode: 0o755},
		{name: "bin/tool", typ: tar.TypeSymlink, link: "tool-1.0"},
		{name: "skip/me", typ: tar.TypeReg, body: "y"},
		{name: "fifo", typ: tar.TypeFifo},
	})
	skip := func(n string) string {
		if strings.HasPrefix(n, "skip/") {
			return ""
		}
		return n
	}
	if err := ExtractTarGz(tp, dest, skip); err != nil {
		t.Fatal(err)
	}
	link, err := os.Readlink(filepath.Join(dest, "bin/tool"))
	if err != nil || link != "tool-1.0" {
		t.Fatalf("link = %q, %v", link, err)
	}
	if _, err := os.Lstat(filepath.Join(dest, "skip")); err == nil {
		t.Error("skipped entry was written")
	}
	if _, err := os.Lstat(filepath.Join(dest, "fifo")); err == nil {
		t.Error("a fifo entry was created")
	}
	// Extracting again replaces the old link instead of failing.
	if err := ExtractTarGz(tp, dest, skip); err != nil {
		t.Fatalf("second extract: %v", err)
	}
}

func TestExtractTarGzRefusesEscapes(t *testing.T) {
	outside := t.TempDir()
	cases := map[string][]tarEntry{
		"dot-dot name":    {{name: "../evil", typ: tar.TypeReg, body: "x"}},
		"dot-dot link":    {{name: "l", typ: tar.TypeSymlink, link: "../../etc"}},
		"absolute link":   {{name: "l", typ: tar.TypeSymlink, link: outside}},
		"dir behind link": {{name: "d", typ: tar.TypeSymlink, link: "."}, {name: "d/e", typ: tar.TypeSymlink, link: ".."}, {name: "d/e/x", typ: tar.TypeDir}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			tp := writeTarGz(t, dir, entries)
			err := ExtractTarGz(tp, filepath.Join(dir, "out"), nil)
			if err == nil || !strings.Contains(err.Error(), "escapes destination") {
				t.Fatalf("err = %v, want escape error", err)
			}
		})
	}
}

// A link the user already has in dest (say to a shared dir) is not a way
// for the archive to write outside dest.
func TestExtractTarGzRefusesWritingThroughExistingLink(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dest, "shared")); err != nil {
		t.Fatal(err)
	}
	tp := writeTarGz(t, dir, []tarEntry{{name: "shared/x", typ: tar.TypeReg, body: "x"}})
	if err := ExtractTarGz(tp, dest, nil); err == nil || !strings.Contains(err.Error(), "escapes destination") {
		t.Fatalf("err = %v, want escape error", err)
	}
	if _, err := os.Lstat(filepath.Join(outside, "x")); err == nil {
		t.Fatal("file written outside dest")
	}
}

func TestExtractTarGzAbsoluteLinkInsideDestIsKept(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out")
	tp := writeTarGz(t, dir, []tarEntry{{name: "l", typ: tar.TypeSymlink, link: filepath.Join(dest, "x")}})
	if err := ExtractTarGz(tp, dest, nil); err != nil {
		t.Fatal(err)
	}
}

func TestExtractTarGzBadInput(t *testing.T) {
	dir := t.TempDir()
	if err := ExtractTarGz(filepath.Join(dir, "missing.tgz"), dir, nil); err == nil {
		t.Error("missing archive: want error")
	}
	notGz := filepath.Join(dir, "plain.tgz")
	if err := os.WriteFile(notGz, []byte("not gzip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ExtractTarGz(notGz, dir, nil); err == nil {
		t.Error("not gzip: want error")
	}
	// A cut-off download: the gzip header is fine, the tar data is not.
	full := writeTarGz(t, dir, []tarEntry{{name: "big", typ: tar.TypeReg, body: strings.Repeat("z", 64<<10)}})
	b, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	cut := filepath.Join(dir, "cut.tgz")
	if err := os.WriteFile(cut, b[:len(b)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ExtractTarGz(cut, filepath.Join(dir, "out"), nil); err == nil {
		t.Error("cut-off archive: want error")
	}
}

func TestExtractTarGzTargetNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
	dir := t.TempDir()
	tp := writeTarGz(t, dir, []tarEntry{
		{name: "ro", typ: tar.TypeDir, mode: 0o755},
		{name: "ro/sub", typ: tar.TypeDir},
	})
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dest, "ro"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dest, "ro"), 0o755) })
	if err := ExtractTarGz(tp, dest, nil); err == nil {
		t.Error("dir in a read-only dir: want error")
	}
	if err := ExtractTarGz(tp, filepath.Join(dest, "ro", "new"), nil); err == nil {
		t.Error("dest below a read-only dir: want error")
	}
	for name, e := range map[string]tarEntry{
		"file":           {name: "ro/f", typ: tar.TypeReg, body: "x"},
		"file in subdir": {name: "ro/sub/f", typ: tar.TypeReg, body: "x"},
		"link":           {name: "ro/l", typ: tar.TypeSymlink, link: "f"},
		"link in subdir": {name: "ro/sub/l", typ: tar.TypeSymlink, link: "f"},
	} {
		tp := writeTarGz(t, t.TempDir(), []tarEntry{e})
		if err := ExtractTarGz(tp, dest, nil); err == nil {
			t.Errorf("%s in a read-only dir: want error", name)
		}
	}
}

func TestExtractZipDirsModesAndErrors(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "a.zip")
	f, err := os.Create(zp)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	if _, err := zw.Create("empty/"); err != nil {
		t.Fatal(err)
	}
	h := &zip.FileHeader{Name: "noperm.txt"}
	h.SetMode(0)
	w, err := zw.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("hi"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	out := filepath.Join(dir, "out")
	if err := ExtractZip(zp, out, nil); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(filepath.Join(out, "empty")); err != nil || !st.IsDir() {
		t.Errorf("empty dir not made: %v", err)
	}
	// An entry with no mode bits still lands as a readable file.
	if st, err := os.Stat(filepath.Join(out, "noperm.txt")); err != nil || st.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, %v; want 0644", st, err)
	}

	if err := ExtractZip(filepath.Join(dir, "missing.zip"), out, nil); err == nil {
		t.Error("missing zip: want error")
	}
	if os.Geteuid() != 0 {
		ro := filepath.Join(dir, "ro")
		if err := os.Mkdir(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
		if err := ExtractZip(zp, filepath.Join(ro, "x"), nil); err == nil {
			t.Error("read-only target: want error")
		}
		if err := ExtractZip(zp, ro, func(n string) string { return strings.TrimSuffix(n, "/") + ".d/" }); err == nil {
			t.Error("dir entry in read-only target: want error")
		}
	}
}

// A damaged download: the zip index is fine but a file's bytes are not.
func TestExtractZipCorruptEntry(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "a.zip")
	f, err := os.Create(zp)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "f.txt", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("PAYLOAD-PAYLOAD"))
	_ = zw.Close()
	_ = f.Close()
	b, err := os.ReadFile(zp)
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(b), "PAYLOAD")
	b[i] = 'X'
	if err := os.WriteFile(zp, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ExtractZip(zp, filepath.Join(dir, "out"), nil); err == nil {
		t.Error("bad checksum: want error")
	}
}

func TestExtractZipUnknownCompression(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "a.zip")
	f, err := os.Create(zp)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.CreateRaw(&zip.FileHeader{Name: "f.txt", Method: 99})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("??"))
	_ = zw.Close()
	_ = f.Close()
	if err := ExtractZip(zp, filepath.Join(dir, "out"), nil); err == nil {
		t.Error("unknown compression: want error")
	}
}

func TestGunzipErrors(t *testing.T) {
	dir := t.TempDir()
	if err := Gunzip(filepath.Join(dir, "missing.gz"), filepath.Join(dir, "x"), 0o644); err == nil {
		t.Error("missing file: want error")
	}
	p := filepath.Join(dir, "plain.gz")
	if err := os.WriteFile(p, []byte("not gzip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Gunzip(p, filepath.Join(dir, "x"), 0o644); err == nil {
		t.Error("not gzip: want error")
	}
	// A gzip stream that stops early fails while copying.
	full := writeTarGz(t, dir, []tarEntry{{name: "big", typ: tar.TypeReg, body: strings.Repeat("q", 64<<10)}})
	b, _ := os.ReadFile(full)
	cut := filepath.Join(dir, "cut.gz")
	if err := os.WriteFile(cut, b[:len(b)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Gunzip(cut, filepath.Join(dir, "y"), 0o644); err == nil {
		t.Error("cut-off gzip: want error")
	}
}

func TestDownloadErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("body"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	if err := Download(context.Background(), "://bad url", filepath.Join(dir, "a"), nil); err == nil {
		t.Error("bad url: want error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Download(ctx, srv.URL, filepath.Join(dir, "b"), nil); err == nil {
		t.Error("cancelled: want error")
	}
	if err := Download(context.Background(), srv.URL, filepath.Join(dir, "no", "such", "dir", "c"), nil); err == nil {
		t.Error("dest dir missing: want error")
	}
}
