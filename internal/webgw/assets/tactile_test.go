package assets

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
)

// tactileDir is the Tactile Code keyboard package (web/packages/
// tactile-keyboard): framework-free TypeScript with its own tests.
const tactileDir = "../../../web/packages/tactile-keyboard"

// The package's own unit tests (test/all.ts) run in node: esbuild bundles
// them (no npm), node runs the bundle, any failing case fails this test.
// Skipped where node is not installed, like runJS.
func TestTactileKeyboardPackage(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed: the keyboard package's tests are not run here")
	}
	abs, err := filepath.Abs(tactileDir)
	if err != nil {
		t.Fatal(err)
	}
	res := api.Build(api.BuildOptions{
		EntryPoints: []string{filepath.Join(abs, "test", "all.ts")},
		Bundle:      true,
		Write:       false,
		Format:      api.FormatESModule,
		Platform:    api.PlatformNeutral,
		Target:      api.ES2022,
		Loader:      map[string]api.Loader{".css": api.LoaderText},
		LogLevel:    api.LogLevelSilent,
		Outfile:     "tactile-tests.mjs",
	})
	if len(res.Errors) > 0 {
		t.Fatalf("bundle the keyboard tests:\n%s", strings.Join(api.FormatMessages(res.Errors, api.FormatMessagesOptions{Kind: api.ErrorMessage}), ""))
	}
	out := filepath.Join(t.TempDir(), "tactile-tests.mjs")
	if err := os.WriteFile(out, res.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, out)
	cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH")}
	b, err := cmd.CombinedOutput()
	t.Logf("%s", b)
	if err != nil {
		t.Fatalf("keyboard package tests failed: %v", err)
	}
	m := regexp.MustCompile(`(\d+)/(\d+) passed`).FindStringSubmatch(string(b))
	if m == nil || m[2] == "0" || m[1] != m[2] {
		t.Fatalf("the keyboard tests did not all run and pass: %q", m)
	}
}

// tactileAbs is the package folder as an absolute path.
func tactileAbs() string {
	abs, _ := filepath.Abs(tactileDir)
	return abs
}

// The package follows the page's browser rules (lint_test.go) too: no
// eval, no network, no storage, no markup, no string timers. The SVG
// namespace is the only URL.
func TestTactileKeyboardFollowsTheBrowserRules(t *testing.T) {
	dir := filepath.Join(tactileDir, "src")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range ents {
		if ext := filepath.Ext(e.Name()); ext != ".ts" && ext != ".css" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		n++
		for _, bad := range lintSource("tactile-keyboard/"+e.Name(), namespaceURL.ReplaceAllString(string(b), "")) {
			t.Error(bad)
		}
	}
	if n < 8 {
		t.Fatalf("linted %d package files", n)
	}
}

// The keyboard is in tactile.js and tactile.css alone: the page loads
// both on first use (lib/tactile.ts), tactile.js imports nothing, and the
// page's first files, the workbench and the phone views do not carry it.
func TestTactileChunkHoldsTheKeyboard(t *testing.T) {
	js, css := readBuilt(t, "tactile.js"), readBuilt(t, "tactile.css")
	page, wb, home := readPage(t), readBuilt(t, "workbench.js"), readBuilt(t, "home.js")
	for _, want := range []string{"TRACKPAD", "tk-dock", "tk-pad-badge"} {
		if !strings.Contains(js, want) {
			t.Errorf("tactile.js has no %s", want)
		}
		if strings.Contains(page, want) || strings.Contains(wb, want) || strings.Contains(home, want) {
			t.Errorf("the page's files hold %s; it belongs in tactile.js", want)
		}
	}
	for _, want := range []string{".tk-kb", ".tk-face", ".tk-dock", ".tk-host", "--tk-dock-h"} {
		if !strings.Contains(css, want) {
			t.Errorf("tactile.css has no %s", want)
		}
	}
	for _, bad := range []string{"import ", "import(", "app-chunk"} {
		if strings.Contains(js, bad) {
			t.Errorf("tactile.js imports something (%q)", bad)
		}
	}
	if !strings.Contains(page, `import("/assets/tactile.js")`) || !strings.Contains(page, `"/assets/tactile.css"`) {
		t.Error("the page does not load /assets/tactile.js and its stylesheet")
	}
	if strings.Contains(readBuilt(t, "index.html"), "tactile") {
		t.Error("index.html names the keyboard: the page loads it on first use")
	}
}

// budgetTactile: the keyboard's code and stylesheet, gzipped.
const budgetTactile = 16 << 10

func TestTactileChunkBudget(t *testing.T) {
	n := gzSize(t, "tactile.js") + gzSize(t, "tactile.css")
	t.Logf("gzipped: tactile.js + tactile.css %d B", n)
	if n > budgetTactile {
		t.Errorf("the Tactile keyboard is %d B gzipped, budget %d B", n, budgetTactile)
	}
}
