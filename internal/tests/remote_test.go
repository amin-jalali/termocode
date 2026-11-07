package tests

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestRemoteArgvKeepsTheCommandPositional(t *testing.T) {
	c := Command{Args: []string{"pytest", "-v", "tests/a b.py::t"}, Env: []string{"CI=true", "NO_COLOR=1"}}
	argv := RemoteArgv(c)
	if argv[0] != "sh" || argv[1] != "-c" || argv[3] != "sh" {
		t.Fatalf("argv = %q", argv)
	}
	if strings.Join(argv[4:], "|") != "pytest|-v|tests/a b.py::t" {
		t.Fatalf("args = %q", argv[4:])
	}
	script := argv[2]
	for _, want := range []string{`env 'CI=true' 'NO_COLOR=1' "$@" 2>&1`, ExitMarker + "$?", "python3 -m pytest"} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "%s") || strings.Contains(script, "a b.py") {
		t.Fatalf("script = %q", script)
	}
}

func TestParseExitMarker(t *testing.T) {
	if n, ok := ParseExitMarker(ExitMarker + "3"); !ok || n != 3 {
		t.Fatalf("got %d %v", n, ok)
	}
	for _, bad := range []string{"", "exit 3", ExitMarker + "x"} {
		if _, ok := ParseExitMarker(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestExtractJSONReportFindsTheJestObject(t *testing.T) {
	out := []byte("PASS src/a.test.js\n  {not json\n{\"numTotalTests\":1,\"testResults\":[]}\nDone\n")
	if got := string(ExtractJSONReport(out)); got != `{"numTotalTests":1,"testResults":[]}` {
		t.Fatalf("report = %q", got)
	}
	if ExtractJSONReport([]byte("{\"a\":1}\n")) != nil {
		t.Fatal("an object without testResults is not the report")
	}
}

func TestBuildCommandWithoutReportFile(t *testing.T) {
	tree := &Tree{Root: "/app", FW: FrameworkPytest}
	c, err := BuildCommand(tree, Scope{Kind: ScopeAll}, "")
	if err != nil || strings.Join(c.Args, " ") != "pytest -v -o junit_family=xunit1" || c.ReportOnStdout {
		t.Fatalf("pytest = %q %v %v", c.Args, c.ReportOnStdout, err)
	}
	tree.FW = FrameworkJest
	c, _ = BuildCommand(tree, Scope{Kind: ScopeAll}, "")
	if strings.Contains(strings.Join(c.Args, " "), "outputFile") || !c.ReportOnStdout {
		t.Fatalf("jest = %q %v", c.Args, c.ReportOnStdout)
	}
}

// memFS is a tests.FS over a MapFS rooted at /app.
type memFS struct{ m fstest.MapFS }

func (f memFS) name(p string) string {
	r := strings.TrimPrefix(strings.TrimPrefix(p, "/app"), "/")
	if r == "" {
		return "."
	}
	return r
}
func (f memFS) Stat(_ context.Context, p string) (fs.FileInfo, error) { return fs.Stat(f.m, f.name(p)) }
func (f memFS) ReadDir(_ context.Context, p string) ([]fs.DirEntry, error) {
	return fs.ReadDir(f.m, f.name(p))
}
func (f memFS) ReadFile(_ context.Context, p string) ([]byte, error) {
	return fs.ReadFile(f.m, f.name(p))
}

func TestDiscoverFSMatchesTheLocalRules(t *testing.T) {
	f := memFS{fstest.MapFS{
		"package.json":           {Data: []byte(`{"devDependencies":{"vitest":"1"}}`)},
		"src/a.test.ts":          {Data: []byte("test('adds', () => {})\n")},
		".hidden/b.test.ts":      {Data: []byte("test('no', () => {})\n")},
		"node_modules/c.test.ts": {Data: []byte("test('no', () => {})\n")},
	}}
	ctx := context.Background()
	fw := DetectFS(ctx, f, "/app")
	if fw != FrameworkVitest {
		t.Fatalf("fw = %v", fw)
	}
	pkgs, err := DiscoverFS(ctx, f, "/app", fw)
	if err != nil || len(pkgs) != 1 || pkgs[0].Name != "src" || pkgs[0].Files[0].Path != "/app/src/a.test.ts" {
		t.Fatalf("pkgs = %+v err %v", pkgs, err)
	}
}

func TestDiscoverFSStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := memFS{fstest.MapFS{"go.mod": {Data: []byte("module m\n")}}}
	if _, err := DiscoverFS(ctx, f, "/app", FrameworkGo); err == nil {
		t.Fatal("a cancelled walk gave no error")
	}
}
