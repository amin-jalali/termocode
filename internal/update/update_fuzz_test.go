package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzParseRelease reads any GitHub answer. An accepted release is a
// published, non-pre-release version tag.
func FuzzParseRelease(f *testing.F) {
	for _, s := range []string{
		`{"tag_name":"v0.2.0","name":"v0.2.0","html_url":"https://x","assets":[{"name":"termocode-v0.2.0-linux-amd64.tar.gz","size":10,"browser_download_url":"https://x/a"},{"name":"checksums.txt","size":1,"browser_download_url":"https://x/c"}]}`,
		`{"tag_name":"v1.0.0-rc.1","prerelease":true}`,
		`{"tag_name":"v1","draft":true}`,
		`{"tag_name":"latest"}`,
		`[]`, ``,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		rel, err := ParseRelease(bytes.NewReader(b))
		if err != nil {
			return
		}
		if rel.Draft || rel.Prerelease || !looksLikeTag(rel.Tag) {
			t.Fatalf("accepted %+v", rel)
		}
		_, _, _ = PickAsset(rel, "linux", "amd64")
	})
}

// FuzzVersionOrder compares any two versions: a version is never newer
// than itself, and two versions are never both newer than each other.
func FuzzVersionOrder(f *testing.F) {
	for _, p := range [][2]string{
		{"v0.2.0", "v0.10.0"}, {"v1.0.0-rc.1", "v1.0.0"}, {"v1.0.0-alpha", "v1.0.0-alpha.1"},
		{"v1.0.0-1", "v1.0.0-x"}, {"v0.0.0-20261029080000-abcdefabcdef", "v1"}, {"1.2.3+meta", "1.2.3"},
	} {
		f.Add(p[0], p[1])
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		if Newer(a, a) {
			t.Fatalf("%q is newer than itself", a)
		}
		if Newer(a, b) && Newer(b, a) {
			t.Fatalf("%q and %q are both newer than each other", a, b)
		}
	})
}

// FuzzChecksums reads any checksums.txt. A listed sum is 64 characters,
// and a file whose real sum is listed always verifies.
func FuzzChecksums(f *testing.F) {
	f.Add([]byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  termocode.tar.gz\n"), "termocode.tar.gz", []byte("x"))
	f.Add([]byte("abc *a\n\n  \nZZ  b"), "a", []byte(""))
	f.Fuzz(func(t *testing.T, sums []byte, name string, body []byte) {
		if w := wantSum(sums, name); w != "" && len(w) != sha256.Size*2 {
			t.Fatalf("sum %q has the wrong length", w)
		}
		if name == "" || strings.ContainsAny(name, " \t\r\n\v\f\x85\xa0") || strings.HasPrefix(name, "*") {
			return
		}
		p := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(p, body, 0o600); err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(body)
		line := []byte(strings.ToUpper(hexString(h[:])) + "  " + name + "\n")
		if err := VerifyChecksum(p, name, line); err != nil {
			t.Fatalf("own sum does not verify: %v", err)
		}
	})
}

func hexString(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&15])
	}
	return string(out)
}

// FuzzExtractBinary gzips any tar stream and extracts the binary from
// it. Success writes only dest, within the size limit; failure leaves no
// dest behind.
func FuzzExtractBinary(f *testing.F) {
	tarOf := func(entries ...tarEntry) []byte {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		for _, e := range entries {
			typ := e.typ
			if typ == 0 {
				typ = tar.TypeReg
			}
			h := &tar.Header{Name: e.name, Typeflag: typ, Mode: 0o755, Linkname: e.link}
			if typ == tar.TypeReg {
				h.Size = int64(len(e.body))
			}
			_ = tw.WriteHeader(h)
			if typ == tar.TypeReg {
				_, _ = tw.Write([]byte(e.body))
			}
		}
		_ = tw.Close()
		return buf.Bytes()
	}
	bin := binDir + "/termocode"
	f.Add(tarOf(tarEntry{name: binDir + "/", typ: tar.TypeDir}, tarEntry{name: bin, body: "#!/bin/sh\necho hi\n"}, tarEntry{name: binDir + "/README.md", body: "r"}))
	f.Add(tarOf(tarEntry{name: "../x", body: "evil"}, tarEntry{name: bin, body: "ok"}))
	f.Add(tarOf(tarEntry{name: bin, body: "ok"}, tarEntry{name: "/etc/passwd", body: "evil"}))
	f.Add(tarOf(tarEntry{name: bin, body: "a"}, tarEntry{name: "./" + bin, body: "b"}))
	f.Add(tarOf(tarEntry{name: bin, typ: tar.TypeSymlink, link: "/bin/sh"}))
	f.Add(tarOf(tarEntry{name: bin, body: strings.Repeat("x", 100)}))
	f.Add([]byte("not a tar"))
	const max = 64
	f.Fuzz(func(t *testing.T, raw []byte) {
		dir := t.TempDir()
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, _ = gz.Write(raw)
		_ = gz.Close()
		tgz := filepath.Join(dir, "a.tar.gz")
		if err := os.WriteFile(tgz, buf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(dir, "out")
		err := ExtractBinary(tgz, binDir, dest, max)
		ents, _ := os.ReadDir(dir)
		st, statErr := os.Stat(dest)
		if err != nil {
			if statErr == nil {
				t.Fatalf("failed (%v) but left %s", err, dest)
			}
			if len(ents) != 1 {
				t.Fatalf("failed (%v) but wrote %v", err, ents)
			}
			return
		}
		if statErr != nil || !st.Mode().IsRegular() || st.Size() > max {
			t.Fatalf("dest after success: %v %v", st, statErr)
		}
		if len(ents) != 2 {
			t.Fatalf("extra files: %v", ents)
		}
	})
}

// FuzzLoadState reads any update.json without a panic; a state that
// loads saves and loads back the same.
func FuzzLoadState(f *testing.F) {
	f.Add([]byte(`{"last_check":"2026-10-29T08:00:00+02:00","latest":"v0.3.0","skipped":"v0.2.9"}`))
	f.Add([]byte(`{"last_check":"not a time"}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, b []byte) {
		dir := t.TempDir()
		p := filepath.Join(dir, "update.json")
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		s := LoadState(p)
		_ = s.Available("v0.1.0")
		_ = s.ShouldToast("v0.1.0")
		p2 := filepath.Join(dir, "again.json")
		if err := SaveState(p2, s); err != nil {
			t.Fatal(err)
		}
		s2 := LoadState(p2)
		if !s2.LastCheck.Equal(s.LastCheck) || s2.Latest != s.Latest || s2.NotesURL != s.NotesURL || s2.Skipped != s.Skipped || s2.Notified != s.Notified {
			t.Fatalf("state round trip: %+v -> %+v", s, s2)
		}
	})
}
