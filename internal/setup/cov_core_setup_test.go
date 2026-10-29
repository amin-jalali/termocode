package setup

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/nerdterm"
	"github.com/amin-jalali/termocode/internal/termimg"
)

// covCaptureStdout runs fn and returns what it printed to os.Stdout.
func covCaptureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	defer func() { os.Stdout = old }()
	fn()
	_ = w.Close()
	os.Stdout = old
	return <-done
}

// covTool writes an executable shell script name into dir.
func covTool(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// covSetupEnv gives Run a machine with nothing on it: an empty PATH dir
// (returned, tests add fake tools to it), a temp HOME / tools dir, no
// COLORTERM, a fake fc-list (nerd says whether a Nerd Font is there) and
// an unknown terminal.
func covSetupEnv(t *testing.T, nerd bool) string {
	t.Helper()
	bin := t.TempDir()
	home := t.TempDir()
	t.Setenv("PATH", bin)
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv(lspinstall.ToolsDirEnv, filepath.Join(home, "tools"))
	t.Setenv("COLORTERM", "")
	t.Setenv("TERM", "xterm")
	oldFC, oldDirs, oldTerm := fcList, fontDirs, termEnv
	t.Cleanup(func() { fcList, fontDirs, termEnv = oldFC, oldDirs, oldTerm })
	out := "DejaVu Sans Mono\n"
	if nerd {
		out = "JetBrainsMono Nerd Font,JetBrainsMono NF\n" + out
	}
	fcList = func() (string, error) { return out, nil }
	fontDir := filepath.Join(home, "fonts")
	fontDirs = func() []string { return []string{fontDir} }
	return bin
}

// covFakeHTTP answers every request of http.DefaultClient with status
// and body (the font download).
type covFakeHTTP struct {
	status int
	body   []byte
	urls   []string
}

func (f *covFakeHTTP) RoundTrip(r *http.Request) (*http.Response, error) {
	f.urls = append(f.urls, r.URL.String())
	return &http.Response{
		StatusCode:    f.status,
		Body:          io.NopCloser(bytes.NewReader(f.body)),
		ContentLength: int64(len(f.body)),
		Header:        http.Header{},
		Request:       r,
	}, nil
}

func covUseHTTP(t *testing.T, f *covFakeHTTP) {
	t.Helper()
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = f
	t.Cleanup(func() { http.DefaultClient.Transport = old })
}

// covFontZip is a JetBrainsMono-like release zip: fonts in a folder and
// a readme that must not be copied.
func covFontZip(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, data := range map[string]string{
		"fonts/JetBrainsMonoNerdFont-Regular.ttf": "ttf-bytes",
		"fonts/JetBrainsMonoNerdFont-Bold.otf":    "otf-bytes",
		"README.md":                               "readme",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(data))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestCovCoreSetupFlags(t *testing.T) {
	covSetupEnv(t, true)
	cases := []struct {
		args []string
		code int
		want []string
	}{
		{[]string{"--install"}, 2, []string{"usage: termocode setup --install <name>"}},
		{[]string{"--install", "no-such-tool"}, 2, []string{`unknown tool "no-such-tool"; known:`, " gopls"}},
		{[]string{"--test-colors"}, 0, []string{"\x1b[48;2;255;0;0m", "If you see distinct bands"}},
		{[]string{"--test-bg"}, 0, []string{"Editor    #1C1C1C", "\x1b[48;2;28;28;28m", "lacks 24-bit color"}},
	}
	for _, c := range cases {
		var code int
		out := covCaptureStdout(t, func() { code = Run(c.args) })
		if code != c.code {
			t.Errorf("Run(%q) = %d, want %d\n%s", c.args, code, c.code, out)
		}
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("Run(%q) output misses %q:\n%s", c.args, w, out)
			}
		}
	}
}

func TestCovCoreSetupInstallGopls(t *testing.T) {
	cases := []struct {
		name string
		goSh string // body of the fake `go`; "" = no go on PATH
		code int
		want []string
	}{
		{"no go", "", 1, []string{"? skipped: gopls needs go"}},
		{"offline", `echo "dial tcp: lookup proxy.golang.org: no such host" >&2; exit 1`, 1, []string{"✗ offline: could not reach the network to install gopls"}},
		{"fails", `echo "compile error in gopls" >&2; exit 3`, 1, []string{"✗ install failed:", "compile error in gopls"}},
		{"works", `/bin/mkdir -p "$GOBIN" && printf '#!/bin/sh\n' > "$GOBIN/gopls" && /bin/chmod +x "$GOBIN/gopls"`, 0, []string{"→ installing gopls (Go) (go install golang.org/x/tools/gopls@latest)", "✓ installed (termocode adds"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bin := covSetupEnv(t, true)
			if c.goSh != "" {
				covTool(t, bin, "go", c.goSh)
			}
			var code int
			out := covCaptureStdout(t, func() { code = Run([]string{"--install", "gopls"}) })
			if code != c.code {
				t.Errorf("code = %d, want %d\n%s", code, c.code, out)
			}
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("output misses %q:\n%s", w, out)
				}
			}
			tools, _ := lspinstall.BinDir()
			_, err := os.Stat(filepath.Join(tools, "gopls"))
			if (c.code == 0) != (err == nil) {
				t.Errorf("shim exists = %v after code %d", err == nil, c.code)
			}
		})
	}
}

func TestCovCoreSetupRunBareMachine(t *testing.T) {
	covSetupEnv(t, true)
	var code int
	out := covCaptureStdout(t, func() { code = Run(nil) })
	if code != 0 {
		t.Fatalf("code = %d\n%s", code, out)
	}
	for _, w := range []string{
		"== termocode setup ==",
		"✗ neovim not found.",
		"Install: sudo apt install neovim",
		`? terminal: COLORTERM not set (TERM="xterm")`,
		"✓ Nerd Font already installed",
		"== language servers ==",
		"? skipped: gopls needs go",
		"== debug adapters ==",
		"missing →",
		"Install any of these with `termocode setup --install <name>`",
		"== final step ==",
		`Configure your terminal to use "JetBrainsMono Nerd Font".`,
		"gnome-terminal:",
		"After that, run `termocode` to launch.",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("output misses %q:\n%s", w, out)
		}
	}
}

func TestCovCoreSetupRunReportsInstalledTools(t *testing.T) {
	bin := covSetupEnv(t, true)
	covTool(t, bin, "nvim", "exit 0")
	t.Setenv("COLORTERM", "truecolor")
	// gopls managed (a shim in the tools bin dir), clangd from the system.
	tools, _ := lspinstall.BinDir()
	if err := os.MkdirAll(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	covTool(t, tools, "gopls", "exit 0")
	covTool(t, bin, "clangd", "exit 0")
	// The terminal itself draws with a Nerd Font.
	nerdFont := "JetBrainsMono Nerd Font 12"
	fakeGNOME(t, &nerdFont)
	out := covCaptureStdout(t, func() { _ = Run(nil) })
	for _, w := range []string{
		"✓ neovim: " + filepath.Join(bin, "nvim"),
		"✓ terminal: truecolor advertised (TERM=xterm)",
		"✓ gopls (Go) installed (managed: " + filepath.Join(tools, "gopls") + ")",
		"installed (" + filepath.Join(bin, "clangd") + ")",
		"Your terminal already draws Nerd Font icons.",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("output misses %q:\n%s", w, out)
		}
	}
}

func TestCovCoreSetupRunInstallsFont(t *testing.T) {
	bin := covSetupEnv(t, false)
	covTool(t, bin, "fc-cache", `echo "$@" > "$HOME/fc-cache.args"`)
	fake := &covFakeHTTP{status: http.StatusOK, body: covFontZip(t)}
	covUseHTTP(t, fake)
	// A GNOME Terminal with a plain font: the steps name the profile.
	plain := "Ubuntu Mono 13"
	fakeGNOME(t, &plain)

	var code int
	out := covCaptureStdout(t, func() { code = Run(nil) })
	if code != 0 {
		t.Fatalf("code = %d\n%s", code, out)
	}
	home := os.Getenv("HOME")
	fontDir := filepath.Join(home, ".local/share/fonts/JetBrainsMonoNF")
	for _, w := range []string{
		"✗ Nerd Font not detected.",
		"Installing JetBrainsMono Nerd Font ...",
		"downloading... 100%",
		"extracting...",
		"refreshing font cache...",
		"✓ installed to " + fontDir,
		`Set your terminal font to "JetBrainsMono Nerd Font" (steps above)`,
	} {
		if !strings.Contains(out, w) {
			t.Errorf("output misses %q:\n%s", w, out)
		}
	}
	if len(fake.urls) != 1 || fake.urls[0] != jbmURL {
		t.Errorf("downloads = %q, want only %s", fake.urls, jbmURL)
	}
	for _, f := range []string{"JetBrainsMonoNerdFont-Regular.ttf", "JetBrainsMonoNerdFont-Bold.otf"} {
		if _, err := os.Stat(filepath.Join(fontDir, f)); err != nil {
			t.Errorf("font %s not installed: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(fontDir, "README.md")); err == nil {
		t.Error("README.md was copied into the font folder")
	}
	args, _ := os.ReadFile(filepath.Join(home, "fc-cache.args"))
	if got := strings.TrimSpace(string(args)); got != "-f "+filepath.Join(home, ".local/share/fonts") {
		t.Errorf("fc-cache args = %q", got)
	}
}

func TestCovCoreSetupFontInstallErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    func(*testing.T) []byte
		fcCache string
		want    string
	}{
		{"http 404", http.StatusNotFound, func(*testing.T) []byte { return []byte("gone") }, "exit 0", "font install failed: download: http 404"},
		{"not a zip", http.StatusOK, func(*testing.T) []byte { return []byte("not a zip") }, "exit 0", "font install failed: extract:"},
		{"fc-cache fails", http.StatusOK, covFontZip, "exit 1", "font install failed: fc-cache:"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bin := covSetupEnv(t, false)
			covTool(t, bin, "fc-cache", c.fcCache)
			covUseHTTP(t, &covFakeHTTP{status: c.status, body: c.body(t)})
			var code int
			out := covCaptureStdout(t, func() { code = Run(nil) })
			if code != 1 {
				t.Errorf("code = %d, want 1", code)
			}
			if !strings.Contains(out, c.want) {
				t.Errorf("output misses %q:\n%s", c.want, out)
			}
			if strings.Contains(out, "== language servers ==") {
				t.Errorf("setup went on after the font failed:\n%s", out)
			}
		})
	}
}

func TestCovCoreInstallNerdFontNilProgress(t *testing.T) {
	bin := covSetupEnv(t, false)
	covTool(t, bin, "fc-cache", "exit 0")
	covUseHTTP(t, &covFakeHTTP{status: http.StatusOK, body: covFontZip(t)})
	// A stale cached "found" answer must be dropped by the install.
	writeNerdCache(NerdFontInfo{Found: true, Stamp: fontStamp()})
	dest, err := InstallNerdFont(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(dest, filepath.Join(".local", "share", "fonts", "JetBrainsMonoNF")) {
		t.Errorf("dest = %q", dest)
	}
	if _, ok := readNerdCache(); ok {
		t.Error("the Nerd Font cache survived the install")
	}
}

func TestCovCoreInstallNerdFontNoHome(t *testing.T) {
	covSetupEnv(t, false)
	t.Setenv("HOME", "")
	if _, err := InstallNerdFont(context.Background(), func(string) {}); err == nil {
		t.Error("install without a home folder did not fail")
	}
	// A file where the font folder must go.
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".local"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallNerdFont(context.Background(), nil); err == nil {
		t.Error("install into a blocked folder did not fail")
	}
}

func TestCovCoreNerdCacheWithoutStateDir(t *testing.T) {
	covSetupEnv(t, true)
	// No XDG_STATE_HOME: the cache goes under HOME/.local/state.
	t.Setenv("XDG_STATE_HOME", "")
	home := os.Getenv("HOME")
	if got, want := nerdCachePath(), filepath.Join(home, ".local", "state", "termocode", "nerdfont.json"); got != want {
		t.Errorf("nerdCachePath = %q, want %q", got, want)
	}
	// No HOME either: no cache at all, detection still answers.
	t.Setenv("HOME", "")
	if p := nerdCachePath(); p != "" {
		t.Errorf("nerdCachePath without HOME = %q", p)
	}
	if !DetectNerdFont(false).Found {
		t.Error("detection without a cache path lost the font")
	}
	if _, ok := readNerdCache(); ok {
		t.Error("readNerdCache without a path said ok")
	}
	InvalidateNerdFontCache() // no path: must not panic
	if p := termFontCachePath(); p != "" {
		t.Errorf("termFontCachePath without HOME = %q", p)
	}
	if got := DetectTerminalFont(true); got.Nerd {
		t.Errorf("unknown terminal said Nerd: %+v", got)
	}
}

func TestCovCoreNerdCacheUnwritable(t *testing.T) {
	covSetupEnv(t, true)
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	// A file where the termocode state folder must go.
	if err := os.WriteFile(filepath.Join(state, "termocode"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	info := DetectNerdFont(false)
	if !info.Found || info.Cached {
		t.Errorf("info = %+v, want a fresh found answer", info)
	}
	if again := DetectNerdFont(false); again.Cached {
		t.Error("an answer was cached in a blocked folder")
	}
}

func TestCovCoreDefaultFontDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	got := strings.Join(defaultFontDirs(), "\n")
	for _, w := range []string{
		"/usr/share/fonts",
		filepath.Join(home, ".cache", "fontconfig"),
		filepath.Join(home, ".local", "share", "fonts"),
		filepath.Join(home, ".fonts"),
	} {
		if !strings.Contains(got, w) {
			t.Errorf("defaultFontDirs misses %q:\n%s", w, got)
		}
	}
	t.Setenv("XDG_CACHE_HOME", "/xc")
	t.Setenv("XDG_DATA_HOME", "/xd")
	got = strings.Join(defaultFontDirs(), "\n")
	if !strings.Contains(got, "/xc/fontconfig") || !strings.Contains(got, "/xd/fonts") {
		t.Errorf("XDG dirs ignored:\n%s", got)
	}
	// No HOME: only the system folders.
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	if d := defaultFontDirs(); len(d) != 3 {
		t.Errorf("without HOME: %q", d)
	}
}

func TestCovCoreHasNerdFontFresh(t *testing.T) {
	covSetupEnv(t, false)
	if hasNerdFont() {
		t.Error("hasNerdFont with no Nerd Font")
	}
	fcList = func() (string, error) { return "", errors.New("no fc-list") }
	if hasNerdFont() {
		t.Error("hasNerdFont when fc-list fails")
	}
}

func TestCovCoreDoctorUnknownTerminalAndSecrets(t *testing.T) {
	covSetupEnv(t, false)
	oldLook := lookPath
	t.Cleanup(func() { lookPath = oldLook })
	lookPath = func(bin string) (string, error) {
		if bin == "nvim" {
			return "/bin/nvim", nil
		}
		return "", errors.New("not found")
	}
	oldRun := runOutput
	t.Cleanup(func() { runOutput = oldRun })
	runOutput = func(string, ...string) (string, error) { return "", errors.New("nvim crashed") }
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	for backend, want := range map[string]string{
		"keyring": "keyring (Secret Service)",
		"age":     "encrypted file",
		"plain":   "plain — values are stored without encryption",
		"":        "not configured",
	} {
		_ = os.MkdirAll(filepath.Join(cfg, "termocode"), 0o755)
		_ = os.WriteFile(filepath.Join(cfg, "termocode", "secrets.json"), []byte(`{"backend":"`+backend+`"}`), 0o644)
		var secret, nvim Check
		for _, c := range Doctor() {
			switch c.Name {
			case "Secret store":
				secret = c
			case "neovim (editor engine)":
				nvim = c
			}
		}
		if !strings.Contains(secret.Detail, want) {
			t.Errorf("backend %q: detail %q, want %q", backend, secret.Detail, want)
		}
		if nvim.Status != OK || nvim.Detail != "/bin/nvim" {
			t.Errorf("nvim with a failing --version: %+v", nvim)
		}
	}
	// No config dir at all.
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if c := secretStoreCheck(); !strings.Contains(c.Detail, "choose a store in Settings") {
		t.Errorf("no config dir: %q", c.Detail)
	}
}

func TestCovCoreNerdFontCheckIcons(t *testing.T) {
	found := NerdFontInfo{Found: true}
	if c := NerdFontCheck(found, ""); c.Detail != "found via fc-list" || c.Status != OK {
		t.Errorf("found, no family: %+v", c)
	}
	if c := NerdFontCheck(found, "nerd"); !strings.Contains(c.Detail, "icons: nerd — if icons show as boxes") {
		t.Errorf("nerd icons: %q", c.Detail)
	}
	if c := NerdFontCheck(found, "unicode"); !strings.Contains(c.Detail, "icons: unicode — set icons to nerd") {
		t.Errorf("found + unicode: %q", c.Detail)
	}
	if c := NerdFontCheck(NerdFontInfo{}, "unicode"); !strings.Contains(c.Detail, "set icons to ascii") || c.Status != Warn {
		t.Errorf("missing + unicode: %+v", c)
	}
}

func TestCovCoreGraphicsCheckTmux(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
	t.Setenv("TERM", "screen")
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("TERM_PROGRAM", "")
	c := GraphicsCheck(termimg.Caps{}, false)
	if !strings.Contains(c.Detail, "allow-passthrough on") {
		t.Errorf("tmux hint missing: %q", c.Detail)
	}
}

func TestCovCoreTerminalFontKnownNotNerd(t *testing.T) {
	plain := "Ubuntu Mono 13"
	fakeGNOME(t, &plain)
	info := DetectTerminalFont(true)
	if !info.Known() || info.Nerd {
		t.Fatalf("info = %+v", info)
	}
	c := TerminalFontCheck(info, "JetBrainsMono Nerd Font")
	if c.Status != Warn || !strings.Contains(c.Detail, "for Nerd icons:") {
		t.Errorf("check = %+v", c)
	}
	var b bytes.Buffer
	printTerminalFont(&b, info, "JetBrainsMono Nerd Font")
	if !strings.Contains(b.String(), "? terminal font:") || !strings.Contains(b.String(), "Icons stay Unicode") {
		t.Errorf("printTerminalFont:\n%s", b.String())
	}
	// Unknown terminal: no fix steps.
	b.Reset()
	printTerminalFont(&b, nerdterm.Info{}, "X")
	if strings.Count(b.String(), "\n") != 2 {
		t.Errorf("unknown terminal printed steps:\n%s", b.String())
	}
}

func TestCovCoreGnomeProfileErrors(t *testing.T) {
	ctx := context.Background()
	broken := func(context.Context, ...string) (string, error) { return "", errors.New("no dbus") }
	if _, err := FindGnomeProfile(ctx, broken, "x"); err == nil {
		t.Error("FindGnomeProfile without gsettings did not fail")
	}
	if _, err := PlanGnomeProfile(ctx, broken, "F", 0, nil); err == nil {
		t.Error("PlanGnomeProfile without gsettings did not fail")
	}
	// A real random id when no id maker is given; size 0 means 12.
	f := newFakeDconf()
	p, err := PlanGnomeProfile(ctx, f.run, "F", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Font != "F 12" || len(p.UUID) != 36 || strings.Count(p.UUID, "-") != 4 {
		t.Errorf("plan = %+v", p)
	}
	if err := ApplyGnomeProfile(ctx, f.run, GnomeProfilePlan{Name: "x"}); err == nil || !strings.Contains(err.Error(), "no profile id") {
		t.Errorf("apply without id: %v", err)
	}
	// The list read-back fails after the sets.
	reads := 0
	flaky := func(c context.Context, args ...string) (string, error) {
		if args[0] == "get" && args[2] == "list" {
			reads++
			if reads > 0 {
				return "", errors.New("dbus gone")
			}
		}
		return f.run(c, args...)
	}
	if err := ApplyGnomeProfile(ctx, flaky, p); err == nil {
		t.Error("apply with a failing read-back did not fail")
	}
	for _, tc := range []struct {
		in   string
		want int
	}{{"Mono", 0}, {"Mono x", 0}, {"Mono 0", 0}, {"Mono 99", 0}, {"Mono 14", 14}} {
		if got := FontSize(tc.in); got != tc.want {
			t.Errorf("FontSize(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
