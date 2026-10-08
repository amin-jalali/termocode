package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/tests"
)

// Glue D + E (debug this test) and D + F (install hint) tests.

func TestGoTestRunPattern(t *testing.T) {
	cases := map[string]string{
		"TestParse":       "^TestParse$",
		"TestParse/empty": "^TestParse$/^empty$",
		"ExampleHello":    "^ExampleHello$",
		"TestA/b/c":       "^TestA$/^b$/^c$",
	}
	for in, want := range cases {
		if got := goTestRunPattern(in); got != want {
			t.Errorf("goTestRunPattern(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTestDebugConfigGo(t *testing.T) {
	cfg, err := testDebugConfig(tests.FrameworkGo, "/ws", "/ws/pkg/a_test.go", "TestA")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "Debug Test: TestA" || !cfg.Debuggable() || dapTypeLang(cfg.Type) != "go" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.Raw["mode"] != "test" || cfg.Raw["program"] != "/ws/pkg" {
		t.Errorf("raw = %v", cfg.Raw)
	}
	if want := []any{"-test.run", "^TestA$"}; !reflect.DeepEqual(cfg.Raw["args"], want) {
		t.Errorf("args = %v, want %v", cfg.Raw["args"], want)
	}
}

func TestTestDebugConfigPytest(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".venv", "bin"), 0o755)
	venvPy := filepath.Join(root, ".venv", "bin", "python")
	_ = os.WriteFile(venvPy, []byte("#!/bin/sh\n"), 0o755)
	file := filepath.Join(root, "tests", "test_x.py")
	cfg, err := testDebugConfig(tests.FrameworkPytest, root, file, "TestCls::test_y")
	if err != nil {
		t.Fatal(err)
	}
	if dapTypeLang(cfg.Type) != "python" || cfg.Raw["module"] != "pytest" {
		t.Fatalf("cfg = %+v", cfg)
	}
	args, _ := cfg.Raw["args"].([]any)
	if len(args) == 0 || args[0] != "tests/test_x.py::TestCls::test_y" {
		t.Errorf("args = %v", args)
	}
	if cfg.Raw["python"] != venvPy {
		t.Errorf("python = %v, want project venv %s", cfg.Raw["python"], venvPy)
	}
	if cfg.Raw["cwd"] != root {
		t.Errorf("cwd = %v", cfg.Raw["cwd"])
	}
}

func TestTestDebugConfigUnsupported(t *testing.T) {
	for _, fw := range []tests.Framework{tests.FrameworkCargo, tests.FrameworkJest, tests.FrameworkVitest, tests.FrameworkNone} {
		if _, err := testDebugConfig(fw, "/ws", "/ws/a", "x"); err == nil {
			t.Errorf("%v: want error", fw)
		}
	}
}

func TestDebugTestRowNeedsCase(t *testing.T) {
	m := Model{}
	// No nvim: nothing starts, nothing panics.
	if cmd := m.debugTestRow(testRow{kind: testRowCase, c: &tests.Case{Name: "TestA", File: "/ws/a_test.go"}}); cmd != nil {
		t.Error("no nvim: want nil cmd")
	}
	// A file row is not a test: a toast explains.
	if cmd := m.debugTestRow(testRow{kind: testRowFile}); cmd == nil {
		t.Error("file row: want a toast")
	}
}

func TestDebugTestCodeActionGroup(t *testing.T) {
	a := nvim.CodeAction{Title: "Debug Test: TestA", Kind: synthKindTestDebug}
	if g := codeActionGroup(a); g != "Test" {
		t.Errorf("group = %q, want Test", g)
	}
}
