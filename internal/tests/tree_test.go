package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func goTree() *Tree {
	return &Tree{Root: "/w", FW: FrameworkGo, Packages: []*Package{
		{Name: "example.com/demo/calc", Dir: "/w/calc", Files: []*File{{Path: "/w/calc/calc_test.go", Rel: "calc/calc_test.go", Cases: []*Case{
			{Name: "TestAdd", File: "/w/calc/calc_test.go", Line: 7},
			{Name: "TestAddFails", File: "/w/calc/calc_test.go", Line: 13},
			{Name: "TestTable", File: "/w/calc/calc_test.go", Line: 20},
			{Name: "TestSkipped", File: "/w/calc/calc_test.go", Line: 25},
		}}}},
		{Name: "example.com/demo/broken", Dir: "/w/broken", Files: []*File{{Path: "/w/broken/broken_test.go", Cases: []*Case{
			{Name: "TestX", File: "/w/broken/broken_test.go", Line: 5},
		}}}},
	}}
}

// TS-04: a full recorded run settles every case and tallies the counts.
func TestTreeApplyGoRun(t *testing.T) {
	tr := goTree()
	tr.MarkRunning(nil)
	for _, ev := range feed(t, NewLineParser(FrameworkGo), "go_test.json") {
		tr.Apply(ev)
	}
	tr.FinishRun()
	c := tr.Counts()
	if c.Passed != 1 || c.Failed != 2 || c.Skipped != 1 || c.Total != 5 {
		t.Fatalf("counts = %+v", c)
	}
	fails := tr.Packages[0].Files[0].Cases[1]
	if f, l := fails.Location(); f != "/w/calc/calc_test.go" || l != 16 {
		t.Fatalf("failure location = %s:%d", f, l)
	}
	broken := tr.Packages[1]
	if !strings.Contains(broken.Error, "undefined") || broken.ErrFile != "/w/broken/broken_test.go" || broken.ErrLine != 5 {
		t.Fatalf("broken pkg = %q %s:%d", broken.Error, broken.ErrFile, broken.ErrLine)
	}
	// Never reported → back to pending.
	if broken.Files[0].Cases[0].Status != StatusPending {
		t.Fatalf("TestX = %v, want pending", broken.Files[0].Cases[0].Status)
	}
}

// TS-05/06: a scoped run only resets the cases in scope.
func TestMarkRunningScope(t *testing.T) {
	tr := goTree()
	cs := tr.Packages[0].Files[0].Cases
	cs[0].Status, cs[1].Status = StatusPassed, StatusFailed
	tr.MarkRunning(Scope{Kind: ScopeCases, Cases: []*Case{cs[1]}}.Includes)
	if cs[0].Status != StatusPassed || cs[1].Status != StatusRunning {
		t.Fatalf("statuses = %v / %v", cs[0].Status, cs[1].Status)
	}
}

func TestTreeApplyPytest(t *testing.T) {
	root := t.TempDir()
	write(t, root, "pyproject.toml", "")
	write(t, root, "tests/test_math.py", "def test_add():\n    pass\n\ndef test_div():\n    pass\n\nclass TestOps:\n    def test_mul(self):\n        pass\n\ndef test_param(a, b):\n    pass\n")
	pkgs, err := Discover(root, FrameworkPytest)
	if err != nil {
		t.Fatal(err)
	}
	tr := &Tree{Root: root, FW: FrameworkPytest, Packages: pkgs}
	tr.MarkRunning(nil)
	for _, ev := range feed(t, NewLineParser(FrameworkPytest), "pytest_v.txt") {
		tr.Apply(ev)
	}
	data, _ := os.ReadFile("testdata/pytest_junit.xml")
	evs, err := ParseJUnitXML(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		tr.Apply(ev)
	}
	tr.FinishRun()
	byName := map[string]*Case{}
	tr.Each(func(_ *Package, _ *File, c *Case) { byName[c.Name] = c })
	if byName["test_param"].Status != StatusFailed {
		t.Fatalf("parametrised case: one failing param must fail the case, got %v", byName["test_param"].Status)
	}
	if byName["TestOps::test_mul"].Status != StatusPassed {
		t.Fatalf("class method = %v", byName["TestOps::test_mul"].Status)
	}
	div := byName["test_div"]
	if f, l := div.Location(); f != filepath.Join(root, "tests/test_math.py") || l != 7 || div.Message != "assert 1.0 == 2" {
		t.Fatalf("test_div = %s:%d %q", f, l, div.Message)
	}
	// test_later is only in the junit report: added dynamically as skipped.
	if c := byName["test_later"]; c == nil || c.Status != StatusSkipped {
		t.Fatalf("test_later = %+v", c)
	}
}

func TestTreeApplyCargo(t *testing.T) {
	tr := &Tree{Root: "/w", FW: FrameworkCargo, Packages: []*Package{
		{Name: "src", Dir: "/w/src", Files: []*File{{Path: "/w/src/lib.rs", Cases: []*Case{
			{Name: "tests::it_adds", File: "/w/src/lib.rs"}, {Name: "tests::it_fails", File: "/w/src/lib.rs"}, {Name: "tests::slow_one", File: "/w/src/lib.rs"},
		}}}},
		{Name: "tests", Dir: "/w/tests", Files: []*File{{Path: "/w/tests/integration.rs", Cases: []*Case{{Name: "smoke", File: "/w/tests/integration.rs"}}}}},
	}}
	tr.MarkRunning(nil)
	for _, ev := range feed(t, NewLineParser(FrameworkCargo), "cargo_test.txt") {
		tr.Apply(ev)
	}
	c := tr.Counts()
	if c.Passed != 2 || c.Failed != 1 || c.Skipped != 1 {
		t.Fatalf("counts = %+v", c)
	}
	f := tr.Packages[0].Files[0].Cases[1]
	if file, line := f.Location(); file != "/w/src/lib.rs" || line != 17 {
		t.Fatalf("it_fails location = %s:%d", file, line)
	}
}

func TestTreeApplyJest(t *testing.T) {
	tr := &Tree{Root: "/work/app", FW: FrameworkJest, Packages: []*Package{
		{Name: "src", Dir: "/work/app/src", Files: []*File{
			{Path: "/work/app/src/math.test.js", Rel: "src/math.test.js", Cases: []*Case{
				{Name: "math › adds", File: "/work/app/src/math.test.js"},
				{Name: "math › nested › subtracts", File: "/work/app/src/math.test.js"},
			}},
			{Path: "/work/app/src/broken.test.js", Rel: "src/broken.test.js", Cases: []*Case{{Name: "x", File: "/work/app/src/broken.test.js"}}},
		}},
	}}
	data, _ := os.ReadFile("testdata/jest.json")
	evs, err := ParseJestJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	tr.MarkRunning(nil)
	for _, ev := range evs {
		tr.Apply(ev)
	}
	tr.FinishRun()
	c := tr.Counts()
	if c.Passed != 2 || c.Failed != 1 || c.Skipped != 1 {
		t.Fatalf("counts = %+v", c)
	}
	if !strings.HasPrefix(tr.Packages[0].Error, "src/broken.test.js: ") {
		t.Fatalf("suite error = %q", tr.Packages[0].Error)
	}
}

func TestBuildCommand(t *testing.T) {
	tr := goTree()
	calc := tr.Packages[0]
	cs := calc.Files[0].Cases
	check := func(name string, scope Scope, want string) {
		t.Helper()
		cmd, err := BuildCommand(tr, scope, "")
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(cmd.Args, " "); got != want {
			t.Errorf("%s: got %q, want %q", name, got, want)
		}
	}
	check("all", Scope{Kind: ScopeAll}, "go test -json ./...")
	check("pkg", Scope{Kind: ScopePackage, Package: calc}, "go test -json ./calc")
	check("case", Scope{Kind: ScopeCases, Cases: cs[1:2]}, "go test -json -run ^TestAddFails$ ./calc")
	check("failed", Scope{Kind: ScopeCases, Cases: []*Case{cs[1], cs[2], tr.Packages[1].Files[0].Cases[0]}},
		"go test -json -run ^(TestAddFails|TestTable|TestX)$ ./broken ./calc")

	py := &Tree{Root: "/w", FW: FrameworkPytest}
	cmd, _ := BuildCommand(py, Scope{Kind: ScopeCases, Cases: []*Case{{Name: "TestOps::test_mul", File: "/w/tests/test_a.py"}}}, "/tmp/r.xml")
	if got := strings.Join(cmd.Args, " "); got != "pytest -v -o junit_family=xunit1 --junitxml=/tmp/r.xml tests/test_a.py::TestOps::test_mul" || cmd.Report != "/tmp/r.xml" {
		t.Errorf("pytest: %q", got)
	}
	js := &Tree{Root: "/w", FW: FrameworkJest}
	cmd, _ = BuildCommand(js, Scope{Kind: ScopeCases, Cases: []*Case{{Name: "math › adds (x)", File: "/w/a.test.js"}}}, "/tmp/r.json")
	if got := strings.Join(cmd.Args, " "); !strings.HasSuffix(got, `-t ^math adds \(x\)$ a.test.js`) {
		t.Errorf("jest: %q", got)
	}
	rs := &Tree{Root: "/w", FW: FrameworkCargo}
	cmd, _ = BuildCommand(rs, Scope{Kind: ScopeCases, Cases: []*Case{{Name: "tests::it_fails"}}}, "")
	if got := strings.Join(cmd.Args, " "); got != "cargo test --no-fail-fast -- tests::it_fails" {
		t.Errorf("cargo: %q", got)
	}
	if _, err := BuildCommand(&Tree{}, Scope{}, ""); err != ErrNoFramework {
		t.Errorf("no framework: err = %v", err)
	}
}
