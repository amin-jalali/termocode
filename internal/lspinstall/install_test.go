package lspinstall

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// fakePath writes an executable shell script named name into a fresh
// folder and puts only that folder (plus the shell's own tools) on PATH.
func fakePath(t *testing.T, scripts map[string]string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fakes need a POSIX shell")
	}
	bin := t.TempDir()
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
}

func toolsRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(ToolsDirEnv, root)
	return root
}

var fakeGoTool = Tool{Name: "fakels", Bin: "fakels", InstallKind: KindGo, Spec: "example.com/fakels", Version: "1.2.3"}

func TestInstallGoToolWritesShimAndVersion(t *testing.T) {
	root := toolsRoot(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-secret")
	fakePath(t, map[string]string{"go": `echo "go $*"
[ -n "$ANTHROPIC_API_KEY" ] && echo "LEAKED KEY"
mkdir -p "$GOBIN" && printf '#!/bin/sh\necho fake\n' > "$GOBIN/fakels" && chmod +x "$GOBIN/fakels"`})
	var lines []string
	out, err := Install(context.Background(), fakeGoTool, func(s string) { lines = append(lines, s) })
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "go install example.com/fakels@1.2.3") {
		t.Fatalf("output:\n%s", out)
	}
	if strings.Contains(out, "LEAKED KEY") {
		t.Fatal("installer saw an API key")
	}
	if len(lines) != 2 || lines[0] != "[1/1] go install example.com/fakels@1.2.3" || lines[1] != "done" {
		t.Fatalf("progress %q", lines)
	}
	st := StatusOf(fakeGoTool)
	if st.State != Managed || st.Version != "1.2.3" || st.Path != filepath.Join(root, "bin", "fakels") {
		t.Fatalf("status %+v", st)
	}
	shim, _ := os.ReadFile(st.Path)
	if !strings.Contains(string(shim), filepath.Join(root, "fakels", "bin", "fakels")) {
		t.Fatalf("shim:\n%s", shim)
	}
	// Reinstall starts from a clean folder.
	stale := filepath.Join(root, "fakels", "stale.txt")
	if err := os.WriteFile(stale, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(context.Background(), fakeGoTool, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("old files kept on reinstall")
	}
}

func TestInstallOfflineIsClassifiedAndCleansUp(t *testing.T) {
	root := toolsRoot(t)
	fakePath(t, map[string]string{"go": `echo 'dial tcp: lookup proxy.golang.org: no such host' >&2; exit 1`})
	out, err := Install(context.Background(), fakeGoTool, nil)
	var off *OfflineError
	if !errors.As(err, &off) || off.Tool != "fakels" {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "no such host") {
		t.Fatalf("output lost: %q", out)
	}
	if _, err := os.Stat(filepath.Join(root, "fakels")); !os.IsNotExist(err) {
		t.Fatal("tool folder left after a failed install")
	}
	if Available(fakeGoTool) {
		t.Fatal("failed install counts as available")
	}
}

func TestInstallMissingToolchainNeverRuns(t *testing.T) {
	toolsRoot(t)
	t.Setenv("PATH", t.TempDir()) // no npm, no node
	tl := Tool{Name: "fakets", Bin: "fakets", InstallKind: KindNpm, Spec: "fakets", Version: "latest", Needs: []string{"node"}}
	_, err := Install(context.Background(), tl, nil)
	var mt *MissingToolchainError
	if !errors.As(err, &mt) || mt.Bin != "npm" || !strings.Contains(err.Error(), "fakets needs npm") {
		t.Fatalf("err = %v", err)
	}
	// npm there but node missing.
	fakePath(t, map[string]string{"npm": "exit 0"})
	t.Setenv("PATH", strings.SplitN(os.Getenv("PATH"), string(os.PathListSeparator), 2)[0])
	_, err = Install(context.Background(), tl, nil)
	if !errors.As(err, &mt) || mt.Bin != "node" {
		t.Fatalf("err = %v", err)
	}
}

func TestInstallUnknownKindAndNoExec(t *testing.T) {
	toolsRoot(t)
	if _, err := Install(context.Background(), Tool{Name: "x", Bin: "x", InstallKind: InstallKind(99)}, nil); err == nil ||
		!strings.Contains(err.Error(), "unknown install kind") {
		t.Fatalf("err = %v", err)
	}
	if InstallKind(99).String() != "unknown" {
		t.Fatal("unknown kind name")
	}
}

func TestInstallRustupComponent(t *testing.T) {
	root := toolsRoot(t)
	target := filepath.Join(t.TempDir(), "rust-analyzer")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	tl := Tool{Name: "fake_ra", Bin: "fake-ra", InstallKind: KindRustup, Spec: "rust-analyzer"}
	fakePath(t, map[string]string{"rustup": `case "$1" in component) echo added;; which) echo '` + target + `';; esac`})
	if _, err := Install(context.Background(), tl, nil); err != nil {
		t.Fatal(err)
	}
	shim, err := os.ReadFile(filepath.Join(root, "bin", "fake-ra"))
	if err != nil || !strings.Contains(string(shim), target) {
		t.Fatalf("shim %q err %v", shim, err)
	}

	fakePath(t, map[string]string{"rustup": `case "$1" in which) ;; esac`})
	if _, err := Install(context.Background(), tl, nil); err == nil || !strings.Contains(err.Error(), "returned nothing") {
		t.Fatalf("empty which: %v", err)
	}
	fakePath(t, map[string]string{"rustup": `case "$1" in which) echo 'no such component' >&2; exit 1;; esac`})
	out, err := Install(context.Background(), tl, nil)
	if err == nil || !strings.Contains(out, "no such component") {
		t.Fatalf("failed which: %v %q", err, out)
	}
}

func TestInstallNoToolsFolder(t *testing.T) {
	t.Setenv(ToolsDirEnv, "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "")
	if runtime.GOOS == "windows" || runtime.GOOS == "plan9" {
		t.Skip("home lookup differs")
	}
	if _, err := ToolsDir(); err == nil {
		t.Fatal("want an error with no HOME")
	}
	if _, err := Install(context.Background(), fakeGoTool, nil); err == nil {
		t.Fatal("install without a tools folder")
	}
	if err := Uninstall(fakeGoTool); err == nil {
		t.Fatal("uninstall without a tools folder")
	}
	if err := SaveAsked(map[string]bool{"go": true}); err == nil {
		t.Fatal("save asked without a tools folder")
	}
	if len(LoadAsked()) != 0 {
		t.Fatal("load asked without a tools folder")
	}
	if StatusOf(fakeGoTool).State == Managed {
		t.Fatal("managed without a tools folder")
	}
	env := EnvWithToolsBin()
	if len(env) != len(os.Environ()) {
		t.Fatal("env changed without a tools folder")
	}
}

func TestToolsDirFromXDG(t *testing.T) {
	t.Setenv(ToolsDirEnv, "")
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	if got, _ := ToolsDir(); got != filepath.Join(xdg, "termocode", "tools") {
		t.Fatalf("got %q", got)
	}
	home := t.TempDir()
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", home)
	if got, _ := ToolsDir(); got != filepath.Join(home, ".local", "share", "termocode", "tools") {
		t.Fatalf("got %q", got)
	}
	found := false
	for _, kv := range EnvWithToolsBin() {
		if strings.HasPrefix(kv, "PATH="+filepath.Join(home, ".local", "share", "termocode", "tools", "bin")) {
			found = true
		}
	}
	if !found {
		t.Fatal("tools/bin not first on PATH")
	}
}

func TestPrependPathEmptyAndDropSecrets(t *testing.T) {
	if got := PrependPath([]string{"PATH="}, "/t"); got[0] != "PATH=/t" {
		t.Fatalf("got %v", got)
	}
	got := DropSecretEnv([]string{"OPENAI_API_KEY=x", "TERMOCODE_DB_PASSWORD=y", "TERMOCODE_THEME=dark", "HOME=/h"})
	if strings.Join(got, ",") != "TERMOCODE_THEME=dark,HOME=/h" {
		t.Fatalf("got %v", got)
	}
}

func TestAskedCorruptFileIsEmpty(t *testing.T) {
	root := toolsRoot(t)
	if err := os.WriteFile(filepath.Join(root, askedFile), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadAsked(); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	// A folder where the root should be: save fails.
	block := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(block, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ToolsDirEnv, filepath.Join(block, "tools"))
	if err := SaveAsked(map[string]bool{"go": true, "py": false}); err == nil {
		t.Fatal("save into a file path must fail")
	}
}

func TestUninstallKeepsGoingWhenShimMissing(t *testing.T) {
	root := toolsRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "fakels"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(fakeGoTool); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "fakels")); !os.IsNotExist(err) {
		t.Fatal("tool folder kept")
	}
	// A shim path that is a non-empty folder can't be removed.
	if err := os.MkdirAll(filepath.Join(root, "bin", "fakels", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(fakeGoTool); err == nil {
		t.Fatal("want an error when the shim can't be removed")
	}
}

func TestWriteShimErrors(t *testing.T) {
	block := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(block, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteShim(filepath.Join(block, "bin"), "x", "x", []string{"/x"}); err == nil {
		t.Fatal("shim under a file must fail")
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "x.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteShim(dir, "x", "x", []string{"/x"}); err == nil {
		t.Fatal("shim temp blocked by a folder must fail")
	}
}

func TestAnyAvailable(t *testing.T) {
	toolsRoot(t)
	fakePath(t, map[string]string{"present-ls": "exit 0"})
	if AnyAvailable([]Tool{{Name: "a", Bin: "absent-ls"}}) {
		t.Fatal("nothing is installed")
	}
	if !AnyAvailable([]Tool{{Name: "a", Bin: "absent-ls"}, {Name: "b", Bin: "present-ls"}}) {
		t.Fatal("present-ls is on PATH")
	}
}

// ── downloads ──────────────────────────────────────────────────────────

func serve(t *testing.T, body []byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing":
			http.NotFound(w, r)
		case "/partial":
			w.Header().Set("Content-Length", "100000")
			w.Write(body[:min(len(body), 10)])
		default:
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.Write(body)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func zipBytes(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(name)
	w.Write([]byte(body))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func gzBytes(body string) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write([]byte(body))
	zw.Close()
	return buf.Bytes()
}

func TestDownloadStepArchives(t *testing.T) {
	big := strings.Repeat("x", 200000)
	cases := []struct {
		archive string
		body    []byte
		dest    func(dir string) string
		check   string // file under dir that must hold the payload
	}{
		{"raw", []byte(big), func(d string) string { return filepath.Join(d, "tool") }, "tool"},
		{"gz", gzBytes("gz-payload"), func(d string) string { return filepath.Join(d, "tool") }, "tool"},
		{"zip", zipBytes(t, "bin/tool", "zip-payload"), func(d string) string { return d }, "bin/tool"},
	}
	for _, c := range cases {
		t.Run(c.archive, func(t *testing.T) {
			url := serve(t, c.body)
			dir := t.TempDir()
			var said []string
			step := Step{Kind: StepDownload, URL: url + "/asset", Archive: c.archive, Dest: c.dest(dir)}
			if err := downloadStep(context.Background(), step, dir, func(s string) { said = append(said, s) }); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(c.check))); err != nil {
				t.Fatalf("payload missing: %v", err)
			}
			if said[len(said)-1] != "extracting" || !strings.HasPrefix(said[0], "downloading ") {
				t.Fatalf("progress %q", said)
			}
			left, _ := filepath.Glob(filepath.Join(dir, ".download-*"))
			if len(left) != 0 {
				t.Fatalf("temp file left: %v", left)
			}
		})
	}
}

func TestDownloadStepFailures(t *testing.T) {
	url := serve(t, []byte("0123456789abcdef"))
	dir := t.TempDir()
	dest := filepath.Join(dir, "tool")
	for _, path := range []string{"/missing", "/partial"} {
		err := downloadStep(context.Background(), Step{URL: url + path, Archive: "raw", Dest: dest}, dir, func(string) {})
		if err == nil {
			t.Fatalf("%s: want an error", path)
		}
		if _, err := os.Stat(dest); !os.IsNotExist(err) {
			t.Fatalf("%s: half a tool was written", path)
		}
	}
	// A broken archive fails at extract time.
	err := downloadStep(context.Background(), Step{URL: url + "/x", Archive: "tar.gz", Dest: dir}, dir, func(string) {})
	if err == nil {
		t.Fatal("bad tar.gz must fail")
	}
	// The download folder is gone.
	if err := downloadStep(context.Background(), Step{URL: url + "/x", Archive: "raw", Dest: dest}, filepath.Join(dir, "nope"), func(string) {}); err == nil {
		t.Fatal("missing folder must fail")
	}
	// Offline: nothing listens.
	srv := httptest.NewServer(http.NotFoundHandler())
	dead := srv.URL
	srv.Close()
	err = downloadStep(context.Background(), Step{URL: dead + "/x", Archive: "raw", Dest: dest}, dir, func(string) {})
	var off *OfflineError
	if !errors.As(ClassifyFailure("t", "", err), &off) {
		t.Fatalf("closed server not seen as offline: %v", err)
	}
}

func TestBuildPlanUnsupportedPlatforms(t *testing.T) {
	tl := Tool{Name: "rel", Bin: "rel", InstallKind: KindGitHubRelease, Spec: "o/r", Version: "1.0",
		Tag: "v{version}", Asset: "rel-{os}-{arch}.tar.gz",
		OS: map[string]string{"linux": "linux"}, Arch: map[string]string{"amd64": "x64"}}
	var un *UnsupportedError
	if _, err := BuildPlan(tl, "/d", "darwin", "amd64"); !errors.As(err, &un) || un.Platform != "darwin/amd64" {
		t.Fatalf("os not in map: %v", err)
	}
	if _, err := BuildPlan(tl, "/d", "linux", "riscv64"); !errors.As(err, &un) {
		t.Fatalf("arch not in map: %v", err)
	}
	p, err := BuildPlan(tl, "/d", "linux", "amd64")
	if err != nil || p.Steps[0].URL != "https://github.com/o/r/releases/download/v1.0/rel-linux-x64.tar.gz" || p.Steps[0].Dest != "/d" {
		t.Fatalf("plan %+v %v", p, err)
	}
	tl.Fallback = "use your package manager"
	tl.Platforms = []string{"linux/arm64"}
	if _, err := BuildPlan(tl, "/d", "linux", "amd64"); !errors.As(err, &un) || !strings.Contains(err.Error(), "use your package manager") {
		t.Fatalf("not whitelisted: %v", err)
	}
	if _, err := ReleaseURL(fakeGoTool, "linux", "amd64"); err == nil {
		t.Fatal("go tool has no release URL")
	}
}

func TestPlanDetails(t *testing.T) {
	for _, c := range []struct{ name, want string }{
		{"a.TGZ", "tar.gz"}, {"a.gz", "gz"}, {"a.zip", "zip"}, {"a", "raw"},
	} {
		if got := archiveType(c.name); got != c.want {
			t.Errorf("archiveType(%q) = %q", c.name, got)
		}
	}
	for _, bin := range []string{"go", "npm", "python3", "cargo", "weird"} {
		if HostHint(bin) == "" {
			t.Errorf("no hint for %s", bin)
		}
	}
	cargo := Tool{Name: "c", Bin: "c", InstallKind: KindCargo, Spec: "crate", Version: "0.3.0", Extra: []string{"--locked"}}
	p, _ := BuildPlan(cargo, "/d", "linux", "amd64")
	if got := strings.Join(p.Steps[0].Argv, " "); got != "cargo install --root /d crate --version 0.3.0 --locked" {
		t.Fatalf("cargo argv %q", got)
	}
	if p.Exec[0] != filepath.Join("/d", "bin", "c") {
		t.Fatalf("cargo exec %v", p.Exec)
	}
	pip := Tool{Name: "p", Bin: "p", InstallKind: KindPip, Spec: "pkg", Version: "latest"}
	p, _ = BuildPlan(pip, "/d", "darwin", "arm64")
	if !strings.HasSuffix(p.Steps[1].Label, "pip install pkg") || p.Exec[0] != filepath.Join("/d", "venv", "bin", "p") {
		t.Fatalf("pip plan %+v", p)
	}
	raw := Tool{Name: "r", Bin: "r", InstallKind: KindGitHubRelease, Spec: "o/r", Version: "2", Tag: "{version}", Asset: "r-bin"}
	p, _ = BuildPlan(raw, "/d", "linux", "amd64")
	if p.Steps[0].Archive != "raw" || p.Steps[0].Dest != filepath.Join("/d", "r") || p.Exec[0] != filepath.Join("/d", "r") {
		t.Fatalf("raw plan %+v", p)
	}
	if (Tool{Name: "n"}).DisplayName() != "n" {
		t.Fatal("display name without label")
	}
	if defaultExec(Tool{InstallKind: KindRustup}, "/d") != nil {
		t.Fatal("rustup has no default exec")
	}
	if ClassifyFailure("x", "", &UnsupportedError{Tool: "x"}) == nil || ClassifyFailure("x", "", &OfflineError{Tool: "x"}) == nil {
		t.Fatal("typed errors pass through")
	}
	if (&OfflineError{Tool: "x", Err: errors.New("e")}).Unwrap() == nil {
		t.Fatal("unwrap")
	}
}

func TestFiletypesAndKinds(t *testing.T) {
	fts := Filetypes(CategoryLSP)
	if len(fts) == 0 {
		t.Fatal("no filetypes")
	}
	for i := 1; i < len(fts); i++ {
		if fts[i-1] >= fts[i] {
			t.Fatalf("not sorted or duplicate: %v", fts)
		}
	}
	names := map[string]bool{}
	for k := KindGo; k <= KindGitHubRelease; k++ {
		names[k.String()] = true
	}
	if len(names) != 6 || names["unknown"] {
		t.Fatalf("kind names %v", names)
	}
}
