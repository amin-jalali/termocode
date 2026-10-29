package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeRelease serves one release of a shell-script "termocode" that
// prints say for --version. sums returns checksums.txt for the tarball.
type fakeRelease struct {
	srv     *httptest.Server
	tarball []byte
	sums    func(tarball []byte) string
}

func newFakeRelease(t *testing.T, tag, say string) *fakeRelease {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script binaries")
	}
	dir := t.TempDir()
	bd := BinaryDir(tag, runtime.GOOS, runtime.GOARCH)
	tgz := makeTarGz(t, dir, []tarEntry{
		{name: bd + "/termocode", body: "#!/bin/sh\necho '" + say + "'\n"},
		{name: bd + "/README.md", body: "readme"},
	})
	data, err := os.ReadFile(tgz)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRelease{tarball: data}
	f.sums = func(b []byte) string { return sha(b) + "  " + TarballName(tag, runtime.GOOS, runtime.GOARCH) + "\n" }
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			rel := map[string]any{"tag_name": tag, "html_url": f.srv.URL + "/notes", "assets": []map[string]any{
				{"name": TarballName(tag, runtime.GOOS, runtime.GOARCH), "size": len(f.tarball), "browser_download_url": f.srv.URL + "/tgz"},
				{"name": ChecksumsName, "size": 1, "browser_download_url": f.srv.URL + "/sums"},
			}}
			_ = json.NewEncoder(w).Encode(rel)
		case "/tgz":
			_, _ = w.Write(f.tarball)
		case "/sums":
			_, _ = w.Write([]byte(f.sums(f.tarball)))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRelease) client() Client {
	return Client{HTTP: f.srv.Client(), LatestURL: f.srv.URL + "/latest", Version: "v0.1.0"}
}

// installedBinary writes an "old" termocode into a fresh bin folder.
func installedBinary(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "termocode")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho 'termocode v0.1.0'\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	return exe
}

func prepare(t *testing.T, f *fakeRelease, exe string) (Staged, error) {
	t.Helper()
	c := f.client()
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return c.Prepare(context.Background(), rel, exe, runtime.GOOS, runtime.GOARCH)
}

func onlyFile(t *testing.T, exe string) {
	t.Helper()
	ents, _ := os.ReadDir(filepath.Dir(exe))
	if len(ents) != 1 {
		var names []string
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Fatalf("bin folder has %v, want only termocode", names)
	}
}

func TestPrepare(t *testing.T) {
	f := newFakeRelease(t, "v0.2.0", "termocode v0.2.0")
	exe := installedBinary(t)
	st, err := prepare(t, f, exe)
	if err != nil {
		t.Fatal(err)
	}
	if st.Tag != "v0.2.0" || st.Exe != exe || filepath.Dir(st.NewPath) != filepath.Dir(exe) || st.Size != int64(len(f.tarball)) {
		t.Fatalf("staged %+v", st)
	}
	info, err := os.Stat(st.NewPath)
	if err != nil || info.Mode().Perm() != 0o750 {
		t.Fatalf("new binary mode %v %v, want 0750 like the old one", info.Mode(), err)
	}
	// The old binary is untouched until Apply.
	if b, _ := os.ReadFile(exe); !strings.Contains(string(b), "v0.1.0") {
		t.Fatal("old binary changed")
	}
	st.Discard()
	onlyFile(t, exe)
}

func TestPrepareChecksumMismatch(t *testing.T) {
	f := newFakeRelease(t, "v0.2.0", "termocode v0.2.0")
	f.sums = func([]byte) string {
		return sha([]byte("other")) + "  " + TarballName("v0.2.0", runtime.GOOS, runtime.GOARCH) + "\n"
	}
	exe := installedBinary(t)
	if _, err := prepare(t, f, exe); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v, want a checksum mismatch", err)
	}
	onlyFile(t, exe)
}

func TestPrepareMissingChecksumLine(t *testing.T) {
	f := newFakeRelease(t, "v0.2.0", "termocode v0.2.0")
	f.sums = func(b []byte) string { return sha(b) + "  termocode-v0.2.0-plan9-amd64.tar.gz\n" }
	exe := installedBinary(t)
	if _, err := prepare(t, f, exe); err == nil || !strings.Contains(err.Error(), "not listed") {
		t.Fatalf("err = %v, want not listed", err)
	}
	onlyFile(t, exe)
}

func TestPrepareSanityCheck(t *testing.T) {
	f := newFakeRelease(t, "v0.2.0", "something else")
	exe := installedBinary(t)
	if _, err := prepare(t, f, exe); err == nil {
		t.Fatal("a binary that does not say its version: want an error")
	}
	onlyFile(t, exe)
}

func TestPrepareNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	f := newFakeRelease(t, "v0.2.0", "termocode v0.2.0")
	exe := installedBinary(t)
	dir := filepath.Dir(exe)
	_ = os.Chmod(dir, 0o555)
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if _, err := prepare(t, f, exe); err == nil {
		t.Fatal("read-only folder: want an error")
	}
	onlyFile(t, exe)
}
