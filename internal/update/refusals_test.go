package update

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestApplyNothingStaged(t *testing.T) {
	if err := (Staged{}).Apply(); err == nil {
		t.Fatal("empty Staged: want an error")
	}
}

// The staged file vanished (a temp cleaner, a second termocode): Apply
// fails and leaves the installed binary and no backup behind.
func TestApplyStagedFileGone(t *testing.T) {
	exe := installedBinary(t)
	st := Staged{Tag: "v0.2.0", Exe: exe, NewPath: filepath.Join(filepath.Dir(exe), ".termocode-new-gone")}
	if err := st.Apply(); err == nil {
		t.Fatal("missing staged file: want an error")
	}
	if !strings.Contains(read(t, exe), "v0.1.0") {
		t.Fatal("installed binary changed")
	}
	if CanRollback(exe) {
		t.Fatal("a backup was left after a failed update")
	}
	onlyFile(t, exe)
}

// When the old binary cannot even be copied, Apply gives up before it
// touches anything and drops the staged file.
func TestApplyCannotKeepOldBinary(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "termocode")
	if err := os.Mkdir(exe, 0o755); err != nil { // neither linkable nor copyable
		t.Fatal(err)
	}
	staged := filepath.Join(dir, ".termocode-new-1")
	if err := os.WriteFile(staged, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := Staged{Exe: exe, NewPath: staged}.Apply()
	if err == nil || !strings.Contains(err.Error(), "could not keep the old binary") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(staged); err == nil {
		t.Fatal("staged file kept after a failed Apply")
	}
	if _, err := os.Lstat(OldPath(exe)); err == nil {
		t.Fatal("half-written backup left behind")
	}
}

func TestCopyFileKeepsModeAndRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("binary"), 0o751); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst")
	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	if read(t, dst) != "binary" {
		t.Fatal("content differs")
	}
	if st, _ := os.Stat(dst); st.Mode().Perm() != 0o751 {
		t.Fatalf("mode %v, want 0751", st.Mode())
	}
	if err := copyFile(src, dst); err == nil {
		t.Fatal("existing dst: want an error")
	}
	if err := copyFile(filepath.Join(dir, "missing"), filepath.Join(dir, "d2")); err == nil {
		t.Fatal("missing src: want an error")
	}
	if os.Geteuid() != 0 {
		locked := filepath.Join(dir, "locked")
		if err := os.WriteFile(locked, []byte("x"), 0o000); err != nil {
			t.Fatal(err)
		}
		if err := copyFile(locked, filepath.Join(dir, "d3")); err == nil {
			t.Fatal("unreadable src: want an error")
		}
	}
}

func TestRollbackRefusesLinkedBackup(t *testing.T) {
	exe := installedBinary(t)
	other := filepath.Join(t.TempDir(), "evil")
	if err := os.WriteFile(other, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, OldPath(exe)); err != nil {
		t.Fatal(err)
	}
	if CanRollback(exe) {
		t.Fatal("a symlinked termocode.old must not be offered")
	}
	if err := Rollback(exe); err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(read(t, exe), "v0.1.0") {
		t.Fatal("installed binary changed")
	}
}

// roundTrip is a fake transport: no network.
type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLatestDefaultsToGitHub(t *testing.T) {
	var asked string
	c := Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		asked = r.URL.String()
		if r.Header.Get("User-Agent") != "termocode/dev" {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(sampleRelease)), Request: r}, nil
	})}}
	if _, err := c.Latest(context.Background()); err != nil {
		t.Fatal(err)
	}
	if asked != LatestURL {
		t.Fatalf("asked %q, want %q", asked, LatestURL)
	}
}

func TestClientRefusesBadAddresses(t *testing.T) {
	c := Client{} // the default http client; nothing here reaches it
	for _, u := range []string{"http://example.invalid/x", "ftp://x/y", "https://bad host/", "%zz"} {
		if err := c.Download(context.Background(), u, filepath.Join(t.TempDir(), "f"), 10); err == nil {
			t.Errorf("%q: want an error", u)
		}
	}
	// The default client is used when none is given: a closed local
	// port fails fast.
	ln := httptest.NewUnstartedServer(nil)
	addr := ln.Listener.Addr().String()
	ln.Listener.Close()
	c.LatestURL = "https://" + addr + "/latest"
	if _, err := c.Latest(context.Background()); err == nil {
		t.Fatal("closed port: want an error")
	}
}

func TestClientRedirects(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loop":
			http.Redirect(w, r, srv.URL+"/loop", http.StatusFound)
		case "/moved":
			http.Redirect(w, r, srv.URL+"/ok", http.StatusMovedPermanently)
		case "/ok":
			_, _ = w.Write([]byte("payload"))
		}
	}))
	defer srv.Close()
	c := Client{HTTP: srv.Client()}
	dir := t.TempDir()
	if err := c.Download(context.Background(), srv.URL+"/moved", filepath.Join(dir, "a"), 100); err != nil {
		t.Fatalf("https to https redirect: %v", err)
	}
	if read(t, filepath.Join(dir, "a")) != "payload" {
		t.Fatal("wrong body after redirect")
	}
	err := c.Download(context.Background(), srv.URL+"/loop", filepath.Join(dir, "b"), 100)
	if err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("redirect loop: err = %v", err)
	}
}

// A server that sends no length is still cut off at max bytes.
func TestDownloadWithoutLengthIsCapped(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("abc"))
		w.(http.Flusher).Flush() // chunked: no Content-Length
		_, _ = w.Write([]byte(strings.Repeat("x", 200)))
	}))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "f")
	err := Client{HTTP: srv.Client()}.Download(context.Background(), srv.URL, dest, 50)
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("err = %v, want too large", err)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("partial file left behind")
	}
	// A dest folder that does not exist is an error, not a panic.
	if err := (Client{HTTP: srv.Client()}).Download(context.Background(), srv.URL, filepath.Join(t.TempDir(), "no", "f"), 500); err == nil {
		t.Fatal("missing folder: want an error")
	}
}

func TestVerifyChecksumOddInput(t *testing.T) {
	dir := t.TempDir()
	name := "termocode.tar.gz"
	sums := []byte("garbage line with many fields\n" + sha([]byte("x")) + "  " + name + "\n")
	if err := VerifyChecksum(filepath.Join(dir, "missing"), name, sums); err == nil {
		t.Error("missing file: want an error")
	}
	if err := VerifyChecksum(dir, name, sums); err == nil {
		t.Error("a folder instead of a file: want an error")
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyChecksum(p, name, sums); err != nil {
		t.Errorf("a valid line after a broken one: %v", err)
	}
}

func TestExtractBinaryBadFiles(t *testing.T) {
	dir := t.TempDir()
	if err := ExtractBinary(filepath.Join(dir, "missing"), "x", filepath.Join(dir, "out"), 10); err == nil {
		t.Error("missing tarball: want an error")
	}
	// A cut-off download: gzip header fine, tar body broken.
	full := makeTarGz(t, dir, []tarEntry{{name: "x/termocode", body: strings.Repeat("b", 64<<10)}})
	b, _ := os.ReadFile(full)
	cut := filepath.Join(dir, "cut.tgz")
	if err := os.WriteFile(cut, b[:len(b)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ExtractBinary(cut, "x", filepath.Join(dir, "out"), 1<<20); err == nil {
		t.Error("cut-off tarball: want an error")
	}
	if _, err := os.Stat(filepath.Join(dir, "out")); err == nil {
		t.Error("partial binary left behind")
	}
	// The binary's folder is gone: writing fails cleanly.
	if err := ExtractBinary(full, "x", filepath.Join(dir, "no", "out"), 1<<20); err == nil {
		t.Error("missing folder: want an error")
	}
}

func TestDetectNilOwner(t *testing.T) {
	exe := installedBinary(t)
	in := Detect(exe, Env{Getenv: envOf(nil)})
	if in.Method != MethodSelf {
		t.Fatalf("method %v, want self", in.Method)
	}
}

func TestSameDirAndReadOnlyCommand(t *testing.T) {
	if !sameDir("/no/such/a/../b", "/no/such/b") {
		t.Error("missing dirs compare by clean path")
	}
	if sameDir("/no/such/a", "/no/such/b") {
		t.Error("different missing dirs are not the same")
	}
	if got := readOnlyCommand("/opt/termocode"); got != InstallScript {
		t.Errorf("non-bin dir: %q", got)
	}
	if got := readOnlyCommand("/opt/tc/bin"); !strings.Contains(got, "PREFIX=/opt/tc ") {
		t.Errorf("bin dir: %q", got)
	}
}

// fakePacman puts a pacman script that prints out on PATH.
func fakePacman(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script")
	}
	dir := t.TempDir()
	if script != "" {
		if err := os.WriteFile(filepath.Join(dir, "pacman"), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func TestPacmanOwner(t *testing.T) {
	t.Run("home folder is never asked", func(t *testing.T) {
		fakePacman(t, `echo should-not-run; exit 1`)
		if pkg, ok := PacmanOwner("/home/u/.local/bin/termocode"); ok {
			t.Fatalf("got %q", pkg)
		}
	})
	t.Run("no pacman", func(t *testing.T) {
		fakePacman(t, "")
		if _, ok := PacmanOwner("/usr/bin/termocode"); ok {
			t.Fatal("want no owner")
		}
	})
	t.Run("owned", func(t *testing.T) {
		fakePacman(t, `[ "$1" = -Qqo ] && [ "$2" = /usr/bin/termocode ] && echo termocode-bin`)
		if pkg, ok := PacmanOwner("/usr/bin/termocode"); !ok || pkg != "termocode-bin" {
			t.Fatalf("got %q %v", pkg, ok)
		}
	})
	for name, script := range map[string]string{
		"not owned":   `echo "error: No package owns /usr/bin/termocode" >&2; exit 1`,
		"empty":       `exit 0`,
		"odd answer":  `echo "two words"`,
		"many owners": `printf 'a\nb\n'`,
	} {
		t.Run(name, func(t *testing.T) {
			fakePacman(t, script)
			if pkg, ok := PacmanOwner("/opt/termocode/termocode"); ok {
				t.Fatalf("got %q", pkg)
			}
		})
	}
}

func TestPrepareRefusals(t *testing.T) {
	t.Run("no asset for this system", func(t *testing.T) {
		f := newFakeRelease(t, "v0.2.0", "termocode v0.2.0")
		exe := installedBinary(t)
		rel, err := f.client().Latest(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.client().Prepare(context.Background(), rel, exe, "plan9", "mips"); err == nil {
			t.Fatal("want an error")
		}
		onlyFile(t, exe)
	})
	t.Run("installed binary gone", func(t *testing.T) {
		f := newFakeRelease(t, "v0.2.0", "termocode v0.2.0")
		if _, err := prepare(t, f, filepath.Join(t.TempDir(), "termocode")); err == nil {
			t.Fatal("want an error")
		}
	})
	t.Run("no temp folder", func(t *testing.T) {
		f := newFakeRelease(t, "v0.2.0", "termocode v0.2.0")
		exe := installedBinary(t)
		c := f.client()
		rel, err := c.Latest(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
		if _, err := c.Prepare(context.Background(), rel, exe, runtime.GOOS, runtime.GOARCH); err == nil {
			t.Fatal("want an error")
		}
		onlyFile(t, exe)
	})
	for name, path := range map[string]string{"tarball missing": "/tgz", "checksums missing": "/sums"} {
		t.Run(name, func(t *testing.T) {
			f := newFakeRelease(t, "v0.2.0", "termocode v0.2.0")
			c := f.client()
			rel, err := c.Latest(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for i := range rel.Assets {
				if strings.HasSuffix(rel.Assets[i].URL, path) {
					rel.Assets[i].URL += "-gone"
				}
			}
			exe := installedBinary(t)
			if _, err := c.Prepare(context.Background(), rel, exe, runtime.GOOS, runtime.GOARCH); err == nil || !strings.Contains(err.Error(), "404") {
				t.Fatalf("err = %v, want HTTP 404", err)
			}
			onlyFile(t, exe)
		})
	}
	t.Run("checksums file too large", func(t *testing.T) {
		f := newFakeRelease(t, "v0.2.0", "termocode v0.2.0")
		f.sums = func([]byte) string { return strings.Repeat("#", maxChecksums+1) }
		exe := installedBinary(t)
		if _, err := prepare(t, f, exe); err == nil || !strings.Contains(err.Error(), "too large") {
			t.Fatalf("err = %v", err)
		}
		onlyFile(t, exe)
	})
	t.Run("binary does not start", func(t *testing.T) {
		f := newFakeRelease(t, "v0.2.0", "termocode v0.2.0")
		bd := BinaryDir("v0.2.0", runtime.GOOS, runtime.GOARCH)
		tgz := makeTarGz(t, t.TempDir(), []tarEntry{{name: bd + "/termocode", body: "#!/bin/sh\nexit 3\n"}})
		b, err := os.ReadFile(tgz)
		if err != nil {
			t.Fatal(err)
		}
		f.tarball = b
		exe := installedBinary(t)
		if _, err := prepare(t, f, exe); err == nil || !strings.Contains(err.Error(), "does not run here") {
			t.Fatalf("err = %v", err)
		}
		onlyFile(t, exe)
	})
	t.Run("tarball without the binary", func(t *testing.T) {
		f := newFakeRelease(t, "v0.2.0", "termocode v0.2.0")
		tgz := makeTarGz(t, t.TempDir(), []tarEntry{{name: "other/README.md", body: "hi"}})
		b, err := os.ReadFile(tgz)
		if err != nil {
			t.Fatal(err)
		}
		f.tarball = b
		exe := installedBinary(t)
		if _, err := prepare(t, f, exe); err == nil || !strings.Contains(err.Error(), "has no") {
			t.Fatalf("err = %v", err)
		}
		onlyFile(t, exe)
	})
}

func TestStateEdgeCases(t *testing.T) {
	if s := LoadState(""); s != (State{}) {
		t.Errorf("no path: %+v", s)
	}
	if err := SaveState("", State{Latest: "v1.0.0"}); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("no path: err = %v", err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveState(filepath.Join(file, "termocode", "update.json"), State{}); err == nil {
		t.Error("parent is a file: want an error")
	}
	// The state path is taken by a folder: the temp file is cleaned up.
	taken := filepath.Join(dir, "st", "update.json")
	if err := os.MkdirAll(filepath.Join(taken, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SaveState(taken, State{Latest: "v1.0.0"}); err == nil {
		t.Error("path is a folder: want an error")
	}
	ents, _ := os.ReadDir(filepath.Dir(taken))
	if len(ents) != 1 {
		t.Errorf("temp file left: %d entries", len(ents))
	}
	if os.Geteuid() != 0 {
		ro := filepath.Join(dir, "ro")
		if err := os.Mkdir(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
		if err := SaveState(filepath.Join(ro, "update.json"), State{}); err == nil {
			t.Error("read-only folder: want an error")
		}
	}
}

func TestVersionOrdering(t *testing.T) {
	for _, c := range []struct {
		cur, latest string
		want        bool
	}{
		{"v1.0.0+build.5", "v1.0.1", true},
		{"v1.0.0", "v1.0.0+other", false},
		{"v1.0.0-alpha", "v1.0.0-alpha.1", true},
		{"v1.0.0-alpha.1", "v1.0.0-alpha", false},
		{"v1.0.0-alpha.1", "v1.0.0-alpha.beta", true}, // words above numbers
		{"v1.0.0-alpha.beta", "v1.0.0-alpha.1", false},
		{"v1.0.0-rc.2", "v1.0.0-rc.10", true},
		{"v1.0.0-rc.1", "v1.0.0-rc.1", false},
		{"v1.2.3.4", "v9.0.0", false},
		{"v1.0.0-", "v9.0.0", false},
		{"v1.-1.0", "v9.0.0", false},
	} {
		if got := Newer(c.cur, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.cur, c.latest, got, c.want)
		}
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func TestParseReleaseReadError(t *testing.T) {
	if _, err := ParseRelease(failReader{}); err == nil || !strings.Contains(err.Error(), "reset") {
		t.Fatalf("err = %v", err)
	}
}
