package scripts

// Tests for install.sh against a fake GitHub release served from a local
// httptest server (TERMOCODE_RELEASE_URL). No network, no real HOME.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

type fakeRelease struct {
	t      *testing.T
	root   string // <root>/<tag>/{tarballs,checksums.txt}
	mu     sync.Mutex
	latest string
	srv    *httptest.Server
}

func newFakeRelease(t *testing.T) *fakeRelease {
	t.Helper()
	f := &fakeRelease{t: t, root: t.TempDir()}
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		tag := f.latest
		f.mu.Unlock()
		http.Redirect(w, r, "/releases/tag/"+tag, http.StatusFound)
	})
	mux.HandleFunc("/releases/tag/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("release page"))
	})
	mux.Handle("/releases/download/", http.StripPrefix("/releases/download/", http.FileServer(http.Dir(f.root))))
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// publish adds release tag with tarballs for this OS (amd64 + arm64).
// When badSum is set, checksums.txt lists a wrong hash.
func (f *fakeRelease) publish(tag string, badSum bool) {
	f.t.Helper()
	dir := filepath.Join(f.root, tag)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	var sums strings.Builder
	for _, arch := range []string{"amd64", "arm64"} {
		name := fmt.Sprintf("termocode-%s-%s-%s", tag, runtime.GOOS, arch)
		data := tarball(f.t, name, "#!/bin/sh\necho termocode "+tag+"\n")
		if err := os.WriteFile(filepath.Join(dir, name+".tar.gz"), data, 0o644); err != nil {
			f.t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if badSum {
			sum[0] ^= 0xff
		}
		fmt.Fprintf(&sums, "%x  %s.tar.gz\n", sum, name)
	}
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(sums.String()), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeRelease) setLatest(tag string) {
	f.mu.Lock()
	f.latest = tag
	f.mu.Unlock()
}

func tarball(t *testing.T, name, script string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := []struct {
		path string
		mode int64
		body string
	}{
		{name + "/termocode", 0o755, script},
		{name + "/README.md", 0o644, "readme\n"},
	}
	if err := tw.WriteHeader(&tar.Header{Name: name + "/", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
		t.Fatal(err)
	}
	for _, fl := range files {
		if err := tw.WriteHeader(&tar.Header{Name: fl.path, Mode: fl.mode, Size: int64(len(fl.body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(fl.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type installEnv struct {
	t       *testing.T
	home    string
	fakeBin string
	rel     *fakeRelease
}

func newInstallEnv(t *testing.T) *installEnv {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("install.sh supports Linux and macOS only")
	}
	for _, tool := range []string{"sh", "tar"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found", tool)
		}
	}
	_, curl := exec.LookPath("curl")
	_, wget := exec.LookPath("wget")
	if curl != nil && wget != nil {
		t.Skip("need curl or wget")
	}
	e := &installEnv{t: t, home: t.TempDir(), fakeBin: t.TempDir(), rel: newFakeRelease(t)}
	e.fakeNvim("v0.11.4")
	return e
}

// fakeNvim puts an `nvim` that reports version v first on PATH.
func (e *installEnv) fakeNvim(v string) {
	e.t.Helper()
	script := "#!/bin/sh\necho 'NVIM " + v + "'\necho 'Build type: Release'\n"
	if err := os.WriteFile(filepath.Join(e.fakeBin, "nvim"), []byte(script), 0o755); err != nil {
		e.t.Fatal(err)
	}
}

func (e *installEnv) bin() string { return filepath.Join(e.home, ".local", "bin", "termocode") }

// run runs install.sh with args and extra env; it returns the combined
// output and whether the script succeeded.
func (e *installEnv) run(extraEnv []string, args ...string) (string, bool) {
	e.t.Helper()
	cmd := exec.Command("sh", append([]string{"install.sh"}, args...)...)
	env := []string{
		"HOME=" + e.home,
		"PATH=" + e.fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"SHELL=/bin/bash",
		"TERMOCODE_RELEASE_URL=" + e.rel.srv.URL + "/releases",
	}
	cmd.Env = append(env, extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

func (e *installEnv) installedVersion() string {
	e.t.Helper()
	out, err := exec.Command(e.bin(), "--version").Output()
	if err != nil {
		e.t.Fatalf("installed binary does not run: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func mustContain(t *testing.T, out, want string) {
	t.Helper()
	if !strings.Contains(out, want) {
		t.Fatalf("output lacks %q:\n%s", want, out)
	}
}

func TestInstallUpgradeUninstall(t *testing.T) {
	e := newInstallEnv(t)
	e.rel.publish("v0.1.0", false)
	e.rel.publish("v0.2.0", false)
	e.rel.setLatest("v0.2.0")

	// A pinned version, without the leading "v".
	out, ok := e.run([]string{"TERMOCODE_VERSION=0.1.0"})
	if !ok {
		t.Fatalf("install failed:\n%s", out)
	}
	mustContain(t, out, "Installed termocode v0.1.0")
	mustContain(t, out, "is not on your PATH")
	mustContain(t, out, "Found nvim 0.11")
	mustContain(t, out, "termocode setup")
	if got := e.installedVersion(); got != "termocode v0.1.0" {
		t.Fatalf("installed %q", got)
	}

	// Upgrade to the latest release.
	out, ok = e.run(nil)
	if !ok {
		t.Fatalf("upgrade failed:\n%s", out)
	}
	mustContain(t, out, "Upgraded termocode v0.1.0 -> v0.2.0")
	if got := e.installedVersion(); got != "termocode v0.2.0" {
		t.Fatalf("after upgrade: %q", got)
	}

	// Again: nothing to do.
	out, ok = e.run(nil)
	if !ok {
		t.Fatalf("second run failed:\n%s", out)
	}
	mustContain(t, out, "v0.2.0 is already installed")

	// --force reinstalls.
	out, ok = e.run(nil, "--force")
	if !ok {
		t.Fatalf("--force failed:\n%s", out)
	}
	mustContain(t, out, "Upgraded termocode v0.2.0 -> v0.2.0")

	// Uninstall, twice.
	out, ok = e.run(nil, "--uninstall")
	if !ok {
		t.Fatalf("uninstall failed:\n%s", out)
	}
	mustContain(t, out, "Removed")
	if _, err := os.Stat(e.bin()); !os.IsNotExist(err) {
		t.Fatalf("binary still there after --uninstall: %v", err)
	}
	out, ok = e.run(nil, "--uninstall")
	if !ok {
		t.Fatalf("second uninstall failed:\n%s", out)
	}
	mustContain(t, out, "not installed")
}

func TestInstallChecksumMismatchKeepsOldBinary(t *testing.T) {
	e := newInstallEnv(t)
	e.rel.publish("v0.1.0", false)
	e.rel.publish("v0.3.0", true)
	if out, ok := e.run(nil, "--version", "v0.1.0"); !ok {
		t.Fatalf("install failed:\n%s", out)
	}

	out, ok := e.run(nil, "--version", "v0.3.0")
	if ok {
		t.Fatalf("install with a bad checksum succeeded:\n%s", out)
	}
	mustContain(t, out, "checksum mismatch")
	if got := e.installedVersion(); got != "termocode v0.1.0" {
		t.Fatalf("bad download replaced the binary: %q", got)
	}
}

func TestInstallUnknownVersionFails(t *testing.T) {
	e := newInstallEnv(t)
	out, ok := e.run(nil, "--version", "v9.9.9")
	if ok {
		t.Fatalf("install of a missing release succeeded:\n%s", out)
	}
	mustContain(t, out, "download failed")
	if _, err := os.Stat(e.bin()); !os.IsNotExist(err) {
		t.Fatal("a binary was installed")
	}
}

func TestInstallPrefixOnPathAndOldNvim(t *testing.T) {
	e := newInstallEnv(t)
	e.rel.publish("v0.1.0", false)
	e.rel.setLatest("v0.1.0")
	e.fakeNvim("v0.9.5")
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")

	cmdPath := binDir + string(os.PathListSeparator) + e.fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")
	out, ok := e.run([]string{"PREFIX=" + prefix, "PATH=" + cmdPath})
	if !ok {
		t.Fatalf("install failed:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(binDir, "termocode")); err != nil {
		t.Fatalf("not installed under PREFIX: %v\n%s", err, out)
	}
	if strings.Contains(out, "is not on your PATH") {
		t.Fatalf("PATH hint shown although %s is on PATH:\n%s", binDir, out)
	}
	mustContain(t, out, "Your nvim is 0.9")
}

func TestInstallTermuxPointsToBuildFromSource(t *testing.T) {
	// Termux reports Linux, but the glibc release binary does not run on
	// Android: say how to build it instead of installing a broken binary.
	e := newInstallEnv(t)
	e.rel.publish("v0.1.0", false)
	e.rel.setLatest("v0.1.0")
	out, ok := e.run([]string{"TERMUX_VERSION=0.118.0"})
	if ok {
		t.Fatalf("install on Termux succeeded:\n%s", out)
	}
	mustContain(t, out, "Termux")
	mustContain(t, out, "go install")
	if _, err := os.Stat(e.bin()); !os.IsNotExist(err) {
		t.Fatal("a binary was installed")
	}
}

func TestInstallUnknownOption(t *testing.T) {
	e := newInstallEnv(t)
	out, ok := e.run(nil, "--bogus")
	if ok {
		t.Fatalf("unknown option accepted:\n%s", out)
	}
	mustContain(t, out, "unknown option")
}
