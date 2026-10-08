package fetch

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadAndProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("hello world"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	dest := filepath.Join(dir, "out")
	var last int64
	if err := Download(context.Background(), srv.URL+"/ok", dest, func(done, _ int64) { last = done }); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dest)
	if string(b) != "hello world" || last != 11 {
		t.Fatalf("got %q last=%d", b, last)
	}
	if err := Download(context.Background(), srv.URL+"/missing", dest, nil); err == nil {
		t.Fatal("want error on 404")
	}
}

func TestExtractZipFlattenAndSlip(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "a.zip")
	f, _ := os.Create(zp)
	zw := zip.NewWriter(f)
	for _, n := range []string{"fonts/A.ttf", "README.md", "x/B.OTF"} {
		w, _ := zw.Create(n)
		_, _ = w.Write([]byte(n))
	}
	_ = zw.Close()
	_ = f.Close()

	out := filepath.Join(dir, "out")
	if err := ExtractZip(zp, out, FlattenSuffix(".ttf", ".otf")); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"A.ttf", "B.OTF"} {
		if _, err := os.Stat(filepath.Join(out, n)); err != nil {
			t.Errorf("missing %s", n)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "README.md")); err == nil {
		t.Error("README should be filtered")
	}

	// zip-slip
	evil := filepath.Join(dir, "evil.zip")
	f, _ = os.Create(evil)
	zw = zip.NewWriter(f)
	w, _ := zw.Create("../../escape.txt")
	_, _ = w.Write([]byte("x"))
	_ = zw.Close()
	_ = f.Close()
	if err := ExtractZip(evil, out, nil); err == nil {
		t.Fatal("want zip-slip error")
	}
}

func TestExtractTarGzKeepsMode(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "a.tar.gz")
	f, _ := os.Create(tp)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "pkg/", Typeflag: tar.TypeDir, Mode: 0o755})
	body := []byte("#!/bin/sh\n")
	_ = tw.WriteHeader(&tar.Header{Name: "pkg/bin/tool", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()

	out := filepath.Join(dir, "out")
	if err := ExtractTarGz(tp, out, nil); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(out, "pkg/bin/tool"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o100 == 0 {
		t.Errorf("exec bit lost: %v", st.Mode())
	}
}

func TestGunzip(t *testing.T) {
	dir := t.TempDir()
	gp := filepath.Join(dir, "x.gz")
	f, _ := os.Create(gp)
	gz := gzip.NewWriter(f)
	_, _ = gz.Write([]byte("bin"))
	_ = gz.Close()
	_ = f.Close()
	dest := filepath.Join(dir, "sub", "x")
	if err := Gunzip(gp, dest, 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dest)
	if string(b) != "bin" {
		t.Fatalf("got %q", b)
	}
}
