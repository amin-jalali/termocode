package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A Go project without go.mod (or with tests at its root) names the root
// package "." like every other framework, never an empty row.
func TestDiscoverGoRootPackageWithoutModule(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a_test.go"), []byte("package a\n\nfunc TestA(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "b_test.go"), []byte("package sub\n\nfunc TestB(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgs, err := Discover(root, FrameworkGo)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range pkgs {
		got = append(got, p.Name)
	}
	if strings.Join(got, ",") != ".,sub" {
		t.Fatalf("package names %q", got)
	}
	if GoImportPath("", "") != "." || GoImportPath("example.com/m", "") != "example.com/m" {
		t.Fatal("an empty folder is the root")
	}
	if GoImportPath("example.com/m", ".") != "example.com/m" || GoImportPath("example.com/m", "x/y") != "example.com/m/x/y" {
		t.Fatal("with a module the import path is used")
	}
}

func TestScopeIncludes(t *testing.T) {
	a := &Case{Name: "A", File: "/w/p/a_test.go"}
	b := &Case{Name: "B", File: "/w/q/b_test.go"}
	pkg := &Package{Files: []*File{{Path: "/w/p/a_test.go"}}}
	cases := []struct {
		s    Scope
		a, b bool
	}{
		{Scope{Kind: ScopeAll}, true, true},
		{Scope{Kind: ScopePackage, Package: pkg}, true, false},
		{Scope{Kind: ScopePackage}, false, false},
		{Scope{Kind: ScopeFile, File: &File{Path: "/w/q/b_test.go"}}, false, true},
		{Scope{Kind: ScopeFile}, false, false},
		{Scope{Kind: ScopeCases, Cases: []*Case{b}}, false, true},
	}
	for i, c := range cases {
		if c.s.Includes(a) != c.a || c.s.Includes(b) != c.b {
			t.Errorf("case %d: a=%v b=%v", i, c.s.Includes(a), c.s.Includes(b))
		}
	}
}

func TestCommandLabelQuotes(t *testing.T) {
	c := Command{Args: []string{"go", "test", "-run", "^(A|B)$", "", "it's", "./x"}}
	if got := c.Label(); got != `go test -run '^(A|B)$' '' 'it'\''s' ./x` {
		t.Fatalf("label %s", got)
	}
}

func TestBuildCommandEveryScope(t *testing.T) {
	root := "/w"
	file := &File{Path: "/w/pkg/a_test.go", Cases: []*Case{{Name: "TestA", File: "/w/pkg/a_test.go"}, {Name: "TestA", File: "/w/pkg/a_test.go"}, {Name: "TestB", File: "/w/pkg/a_test.go"}}}
	pkg := &Package{Dir: "/w/pkg", Files: []*File{file}}
	rootPkg := &Package{Dir: "/w"}
	for _, c := range []struct {
		fw    Framework
		scope Scope
		want  string
	}{
		{FrameworkGo, Scope{Kind: ScopeFile, File: file}, "go test -json -run ^(TestA|TestB)$ ./pkg"},
		{FrameworkGo, Scope{Kind: ScopePackage, Package: rootPkg}, "go test -json ."},
		{FrameworkPytest, Scope{Kind: ScopeAll}, "pytest -v -o junit_family=xunit1 --junitxml=/r"},
		{FrameworkPytest, Scope{Kind: ScopePackage, Package: pkg}, "pytest -v -o junit_family=xunit1 --junitxml=/r pkg"},
		{FrameworkPytest, Scope{Kind: ScopeFile, File: file}, "pytest -v -o junit_family=xunit1 --junitxml=/r pkg/a_test.go"},
		{FrameworkPytest, Scope{Kind: ScopeCases, Cases: file.Cases}, "pytest -v -o junit_family=xunit1 --junitxml=/r pkg/a_test.go::TestA pkg/a_test.go::TestB"},
		{FrameworkCargo, Scope{Kind: ScopeAll}, "cargo test --no-fail-fast"},
		{FrameworkCargo, Scope{Kind: ScopeFile, File: file}, "cargo test --no-fail-fast -- TestA TestB"},
		{FrameworkVitest, Scope{Kind: ScopePackage, Package: pkg}, "npx --no-install vitest run --reporter=default --reporter=json --outputFile=/r pkg"},
		{FrameworkJest, Scope{Kind: ScopeFile, File: file}, "npx --no-install jest --json --testLocationInResults --outputFile=/r pkg/a_test.go"},
	} {
		cmd, err := BuildCommand(&Tree{Root: root, FW: c.fw}, c.scope, "/r")
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(cmd.Args, " "); got != c.want {
			t.Errorf("%v %d: got %q, want %q", c.fw, c.scope.Kind, got, c.want)
		}
		if cmd.Dir != root || len(cmd.Env) == 0 {
			t.Errorf("dir %q env %v", cmd.Dir, cmd.Env)
		}
	}
	if _, err := BuildCommand(nil, Scope{}, ""); err != ErrNoFramework {
		t.Fatal("nil tree")
	}
}

func TestFrameworkNamesAndCounts(t *testing.T) {
	want := map[Framework]string{FrameworkNone: "none", FrameworkGo: "go test", FrameworkPytest: "pytest", FrameworkCargo: "cargo test", FrameworkJest: "jest", FrameworkVitest: "vitest"}
	for fw, s := range want {
		if fw.String() != s {
			t.Errorf("%d = %q", fw, fw.String())
		}
	}
	f := &File{Cases: []*Case{{Status: StatusPassed}, {Status: StatusFailed}, {Status: StatusSkipped}, {Status: StatusRunning}, {Status: StatusPending}}}
	if c := CountFile(f); c != (Counts{Total: 5, Passed: 1, Failed: 1, Skipped: 1, Running: 1}) {
		t.Fatalf("file counts %+v", c)
	}
	if c := CountPackage(&Package{Files: []*File{f, f}}); c.Total != 10 || c.Failed != 2 {
		t.Fatalf("package counts %+v", c)
	}
	var nilTree *Tree
	if nilTree.Counts() != (Counts{}) {
		t.Fatal("nil tree counts")
	}
	nilTree.Each(func(*Package, *File, *Case) { t.Fatal("nil tree has cases") })
	nilTree.MarkRunning(nil)
	if p, f := nilTree.FindFile("/x"); p != nil || f != nil {
		t.Fatal("nil tree find")
	}
	if nilTree.Apply(Event{Kind: EventResult}) != nil {
		t.Fatal("nil tree apply")
	}
	c := &Case{File: "/a.go", Line: 3}
	if f, l := c.Location(); f != "/a.go" || l != 3 {
		t.Fatal("location")
	}
	c.FailFile, c.FailLine = "/b.go", 9
	if f, l := c.Location(); f != "/b.go" || l != 9 {
		t.Fatal("fail location")
	}
	if MergeStatus(StatusSkipped, StatusPending) != StatusSkipped {
		t.Fatal("pending never beats a result")
	}
}

func TestGoParserBuildFailureAndOddLines(t *testing.T) {
	p := NewLineParser(FrameworkGo)
	if evs := p.Line("   "); evs != nil {
		t.Fatal("blank line")
	}
	if evs := p.Line("{not json"); len(evs) != 1 || evs[0].Kind != EventOutput {
		t.Fatalf("broken JSON %+v", evs)
	}
	lines := []string{
		`{"ImportPath":"ex.com/m/b [ex.com/m/b.test]","Action":"build-output","Output":"# ex.com/m/b\n"}`,
		`{"ImportPath":"ex.com/m/b [ex.com/m/b.test]","Action":"build-output","Output":"b/b_test.go:4:2: undefined: x\n"}`,
		`{"ImportPath":"ex.com/m/b [ex.com/m/b.test]","Action":"build-fail"}`,
		`{"Action":"start","Package":"ex.com/m/b"}`,
		`{"Action":"output","Package":"ex.com/m/b","Output":"FAIL\tex.com/m/b [build failed]\n"}`,
		`{"Action":"fail","Package":"ex.com/m/b","FailedBuild":"ex.com/m/b [ex.com/m/b.test]"}`,
		`{"Action":"output","Package":"ex.com/m/c","Output":"panic: boom\n"}`,
		`{"Action":"fail","Package":"ex.com/m/c"}`,
		`{"Action":"fail","Package":"ex.com/m/d"}`,
		`{"Action":"pass","Package":"ex.com/m/e"}`,
		`{"Action":"skip","Package":"ex.com/m/e","Test":"TestS","Elapsed":0.5}`,
		`{"Action":"pause","Package":"ex.com/m/e","Test":"TestS"}`,
	}
	var pkgs []Event
	var res []Event
	for _, ln := range lines {
		for _, ev := range p.Line(ln) {
			switch ev.Kind {
			case EventPackage:
				pkgs = append(pkgs, ev)
			case EventResult:
				res = append(res, ev)
			}
		}
	}
	if p.Flush() != nil {
		t.Fatal("go flush")
	}
	if len(pkgs) != 3 {
		t.Fatalf("package events %+v", pkgs)
	}
	b := pkgs[0]
	if b.Package != "ex.com/m/b" || b.Status != StatusFailed || !strings.Contains(b.Message, "undefined: x") || b.FailFile != "b/b_test.go" || b.FailLine != 4 {
		t.Fatalf("build failure %+v", b)
	}
	if pkgs[1].Package != "ex.com/m/c" || pkgs[1].Message != "panic: boom" {
		t.Fatalf("setup failure %+v", pkgs[1])
	}
	if pkgs[2].Package != "ex.com/m/e" || pkgs[2].Status != StatusPassed {
		t.Fatalf("pass %+v", pkgs[2])
	}
	if len(res) != 1 || res[0].Status != StatusSkipped || res[0].Duration.Seconds() != 0.5 {
		t.Fatalf("results %+v", res)
	}
}

func TestOtherParsersOddInput(t *testing.T) {
	raw := NewLineParser(FrameworkJest)
	if evs := raw.Line("PASS a.test.js"); len(evs) != 1 || evs[0].Line != "PASS a.test.js" || raw.Flush() != nil {
		t.Fatal("raw parser")
	}
	py := NewLineParser(FrameworkPytest)
	evs := py.Line("[gw1] [ 50%] XFAIL tests/test_a.py::test_b[x-1]")
	if len(evs) != 2 || evs[1].Status != StatusSkipped || evs[1].Test != "test_b" || evs[1].File != "tests/test_a.py" {
		t.Fatalf("xdist line %+v", evs)
	}
	if evs := py.Line("tests/test_a.py::test_c XPASS"); evs[1].Status != StatusPassed {
		t.Fatalf("xpass %+v", evs)
	}
	if evs := py.Line("tests/test_a.py::test_d ERROR"); evs[1].Status != StatusFailed {
		t.Fatalf("error %+v", evs)
	}
	if py.Flush() != nil {
		t.Fatal("pytest flush")
	}
	if _, err := ParseJUnitXML([]byte("<nope")); err == nil {
		t.Fatal("broken XML")
	}
	evs, err := ParseJUnitXML([]byte(`<testsuite><testcase classname="t.test_a" name="test_e" time="x"><error message="">Traceback
t/test_a.py:7: in test_e
E boom</error></testcase></testsuite>`))
	if err != nil || len(evs) != 1 || evs[0].Status != StatusFailed || evs[0].FailLine != 7 || !strings.HasPrefix(evs[0].Message, "Traceback") {
		t.Fatalf("bare testsuite %+v %v", evs, err)
	}
	if _, err := ParseJestJSON([]byte("{")); err == nil {
		t.Fatal("broken jest JSON")
	}
	evs, err = ParseJestJSON([]byte(`{"testResults":[
		{"name":"/w/a.test.js","status":"failed","message":"\u001b[31mSyntaxError\u001b[0m","assertionResults":[]},
		{"name":"/w/b.test.js","assertionResults":[
			{"title":"t1","status":"failed","failureMessages":["Error: no\n    at Object.<anonymous> (/w/node_modules/x.js:1:1)\n    at helper (/w/lib.js:4:2)"],"location":{"line":9}},
			{"title":"t2","status":"failed","location":{"line":12}},
			{"title":"t3","status":"todo"}]}]}`))
	if err != nil || len(evs) != 4 {
		t.Fatalf("jest %+v %v", evs, err)
	}
	if evs[0].Kind != EventPackage || evs[0].Message != "SyntaxError" {
		t.Fatalf("file failure %+v", evs[0])
	}
	if evs[1].FailFile != "/w/lib.js" || evs[1].FailLine != 4 || evs[1].Message != "Error: no" {
		t.Fatalf("stack outside node_modules %+v", evs[1])
	}
	if evs[2].FailFile != "/w/b.test.js" || evs[2].FailLine != 12 {
		t.Fatalf("location fallback %+v", evs[2])
	}
	if evs[3].Status != StatusSkipped {
		t.Fatalf("todo %+v", evs[3])
	}
}

func TestTreeApplyOddEvents(t *testing.T) {
	tr := &Tree{Root: "/w", FW: FrameworkGo, Packages: []*Package{
		{Name: "m/p", Dir: "/w/p", Files: []*File{{Path: "/w/p/a_test.go", Cases: []*Case{{Name: "TestA", File: "/w/p/a_test.go"}}}}},
		{Name: "m/empty", Dir: "/w/empty"},
	}}
	if tr.Apply(Event{Kind: EventOutput, Line: "x"}) != nil {
		t.Fatal("output updates no case")
	}
	if tr.Apply(Event{Kind: EventKind(9)}) != nil {
		t.Fatal("unknown kind")
	}
	// A test the discovery missed (generated) joins the first file.
	c := tr.Apply(Event{Kind: EventResult, Package: "m/p", Test: "TestGen", Status: StatusPassed, Message: "ok"})
	if c == nil || c.File != "/w/p/a_test.go" || len(tr.Packages[0].Files[0].Cases) != 2 || c.Message != "ok" {
		t.Fatalf("generated test %+v", c)
	}
	if tr.Apply(Event{Kind: EventResult, Package: "m/empty", Test: "TestX"}) != nil {
		t.Fatal("package without files")
	}
	if tr.Apply(Event{Kind: EventResult, Package: "m/none", Test: "TestX"}) != nil {
		t.Fatal("unknown package")
	}
	// Package errors: set with a location, then cleared by a pass.
	tr.Apply(Event{Kind: EventPackage, Package: "m/p", Status: StatusFailed, Message: "build failed", FailFile: "p/a_test.go", FailLine: 3})
	if p := tr.Packages[0]; p.Error != "build failed" || p.ErrFile != "/w/p/a_test.go" || p.ErrLine != 3 {
		t.Fatalf("package error %+v", p)
	}
	tr.Apply(Event{Kind: EventPackage, Package: "m/p", Status: StatusPassed})
	if tr.Packages[0].Error != "" {
		t.Fatal("pass did not clear the error")
	}
	tr.Apply(Event{Kind: EventPackage, Package: "m/none", Status: StatusFailed})
	tr.Apply(Event{Kind: EventPackage, File: "p/a_test.go", Status: StatusFailed, Message: "syntax"})
	if tr.Packages[0].Error != "p/a_test.go: syntax" {
		t.Fatalf("file error %q", tr.Packages[0].Error)
	}
	tr.Apply(Event{Kind: EventPackage, File: "p/a_test.go", Status: StatusPassed})
	if tr.Packages[0].Error != "" {
		t.Fatal("file pass did not clear")
	}

	none := &Tree{FW: FrameworkNone}
	if none.Apply(Event{Kind: EventResult, Test: "x"}) != nil {
		t.Fatal("no framework")
	}
	py := &Tree{Root: "/w", FW: FrameworkPytest, Packages: []*Package{{Files: []*File{{Path: "/w/t/test_a.py", Rel: "t/test_a.py"}}}}}
	if py.Apply(Event{Kind: EventResult, Class: "other.mod", Test: "x"}) != nil {
		t.Fatal("unknown class without a file")
	}
	if c := py.Apply(Event{Kind: EventResult, Class: "other.mod", File: "t/test_a.py", Test: "test_f"}); c == nil || c.Name != "test_f" {
		t.Fatalf("class miss falls back to file: %+v", c)
	}
	if py.Apply(Event{Kind: EventResult, File: "t/test_a.py"}) != nil {
		t.Fatal("no test name")
	}
}

func TestDiscoverSkipsJunk(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "// c\nmodule \"ex.com/m\"\n")
	write("a/a_test.go", "package a\nfunc TestA(t *testing.T) {}\n")
	write("a/empty_test.go", "package a\n")
	write("testdata/x_test.go", "package x\nfunc TestNo(t *testing.T) {}\n")
	write(".hidden/h_test.go", "package h\nfunc TestNo(t *testing.T) {}\n")
	write("vendor/v/v_test.go", "package v\nfunc TestNo(t *testing.T) {}\n")
	write("big/big_test.go", "package big\nfunc TestNo(t *testing.T) {}\n"+strings.Repeat("//\n", maxFileBytes/3+1))
	pkgs, err := Discover(root, FrameworkGo)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || pkgs[0].Name != "ex.com/m/a" || len(pkgs[0].Files) != 1 {
		t.Fatalf("pkgs %+v", pkgs)
	}
	if p, _ := Discover(root, FrameworkNone); p != nil {
		t.Fatal("no framework, no packages")
	}
	if GoModulePath(t.TempDir()) != "" {
		t.Fatal("no go.mod")
	}
	if ParseFile(FrameworkNone, "x", "y") != nil {
		t.Fatal("no framework parser")
	}
	// JS: files in __tests__ count; other .js files do not.
	if !IsTestFile(FrameworkJest, "/w/src/__tests__/util.js") || IsTestFile(FrameworkJest, "/w/src/util.js") {
		t.Fatal("__tests__ rule")
	}
}
