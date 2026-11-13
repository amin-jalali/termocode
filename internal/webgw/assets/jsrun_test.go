package assets

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/amin-jalali/termocode/internal/webgw/assets/manifest"
)

// runJS bundles a small TypeScript harness against the real sources in
// src/ (as if it were a file in src/) and runs it with node, so a test
// checks what the page's code does, not only what it says. stubs swap
// a module by its import path (as the sources write it, like
// "../session") for test code. The harness throws to fail; what it
// prints is returned. CI builds the page without node; when node is
// not on PATH the test is skipped.
func runJS(t *testing.T, harness string, stubs map[string]string) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed: the page's code is not run here")
	}
	abs, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	vendorDir := filepath.Join(abs, "vendor")
	pkgs, err := manifest.LoadVendor(vendorDir)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, p := range pkgs {
		for spec, file := range p.Imports {
			files[spec] = filepath.Join(vendorDir, p.Dir, file)
		}
	}
	plugin := api.Plugin{Name: "test-resolve", Setup: func(b api.PluginBuild) {
		b.OnResolve(api.OnResolveOptions{Filter: `.*`}, func(a api.OnResolveArgs) (api.OnResolveResult, error) {
			if _, ok := stubs[a.Path]; ok {
				return api.OnResolveResult{Path: a.Path, Namespace: "stub"}, nil
			}
			if strings.HasPrefix(a.Path, ".") || a.Kind == api.ResolveEntryPoint {
				return api.OnResolveResult{}, nil
			}
			if f, ok := files[a.Path]; ok {
				return api.OnResolveResult{Path: f}, nil
			}
			return api.OnResolveResult{}, errors.New("not vendored: " + a.Path)
		})
		b.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "stub"}, func(a api.OnLoadArgs) (api.OnLoadResult, error) {
			src := stubs[a.Path]
			return api.OnLoadResult{Contents: &src, Loader: api.LoaderTS, ResolveDir: filepath.Join(abs, "src", "lib")}, nil
		})
	}}
	res := api.Build(api.BuildOptions{
		Stdin:      &api.StdinOptions{Contents: harness, ResolveDir: filepath.Join(abs, "src", "lib"), Sourcefile: "harness.ts", Loader: api.LoaderTS},
		Bundle:     true,
		Write:      false,
		Format:     api.FormatESModule,
		Platform:   api.PlatformNeutral,
		Target:     api.ES2022,
		Loader:     map[string]api.Loader{".css": api.LoaderEmpty},
		LogLevel:   api.LogLevelSilent,
		Plugins:    []api.Plugin{plugin},
		Outfile:    "harness.mjs",
		MainFields: []string{"module", "main"},
	})
	if len(res.Errors) > 0 {
		t.Fatalf("bundle the harness:\n%s", strings.Join(api.FormatMessages(res.Errors, api.FormatMessagesOptions{Kind: api.ErrorMessage}), ""))
	}
	out := filepath.Join(t.TempDir(), "harness.mjs")
	if err := os.WriteFile(out, res.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, out)
	cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH")}
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the harness failed: %v\n%s", err, b)
	}
	return string(b)
}
