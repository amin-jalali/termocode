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

// radialDir is the radial menu package (web/packages/radial-menu): the
// mobocode floating button and its radial menu, framework-free
// TypeScript with its own tests.
const radialDir = "../../../web/packages/radial-menu"

// The package's own unit tests (test/all.ts) run in node: esbuild bundles
// them (no npm), node runs the bundle, any failing case fails this test.
// Skipped where node is not installed, like runJS.
func TestRadialMenuPackage(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed: the radial menu package's tests are not run here")
	}
	abs, err := filepath.Abs(radialDir)
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
		Outfile:     "radial-tests.mjs",
	})
	if len(res.Errors) > 0 {
		t.Fatalf("bundle the radial menu tests:\n%s", strings.Join(api.FormatMessages(res.Errors, api.FormatMessagesOptions{Kind: api.ErrorMessage}), ""))
	}
	out := filepath.Join(t.TempDir(), "radial-tests.mjs")
	if err := os.WriteFile(out, res.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, out)
	cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH")}
	b, err := cmd.CombinedOutput()
	t.Logf("%s", b)
	if err != nil {
		t.Fatalf("radial menu package tests failed: %v", err)
	}
	// "N/N passed" with N > 0: a bundle that drops the test files (a
	// package.json sideEffects list without them) would pass with none.
	m := regexp.MustCompile(`(\d+)/(\d+) passed`).FindStringSubmatch(string(b))
	if m == nil || m[2] == "0" || m[1] != m[2] {
		t.Fatalf("the radial menu tests printed no passing summary (%v)", m)
	}
}
