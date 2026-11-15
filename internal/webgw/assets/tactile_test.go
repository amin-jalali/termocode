package assets

import (
	"os"
	"os/exec"
	"path/filepath"
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
	if !strings.Contains(string(b), " passed") {
		t.Fatal("the keyboard tests printed no summary")
	}
}
