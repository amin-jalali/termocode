package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/tests"
)

// Coverage of test_debug.go: the entry points that start a debug session for
// one test, with the fake nvim-dap table so nothing real starts.

func TestCovTestDebugEntryPoints(t *testing.T) {
	m, dir := goRepoModel(t)
	fakeBin(t, map[string]string{"dlv": "exit 0", "python3": "exit 0"})
	fakeDapLua(t, &m, "", "[]", "")
	file := filepath.Join(dir, "main_test.go")

	if m.debugTest("", "") != nil {
		t.Error("needs a file and a name")
	}
	// a Go test file works before discovery
	if m.debugTest(file, "TestThing") == nil {
		t.Error("start returns a command")
	}
	if got := dapCalls(t, &m); got == "" {
		t.Error("dap.run not called")
	}
	m.dapSessionActive = true
	if m.debugTest(file, "TestThing") == nil {
		t.Error("running session toasts")
	}
	m.dapSessionActive = false
	// a framework without a debug mapping toasts
	m.testView = newTestsState()
	m.testView.tree = &tests.Tree{Root: dir, FW: tests.FrameworkJest}
	if m.debugTest(filepath.Join(dir, "a.test.js"), "x") == nil {
		t.Error("jest toasts")
	}
	// pytest builds a node id with the project python
	if _, err := testDebugConfig(tests.FrameworkPytest, dir, filepath.Join(dir, "t", "test_a.py"), "test_a"); err != nil {
		t.Error(err)
	}
	_ = os.MkdirAll(filepath.Join(dir, ".venv", "bin"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".venv", "bin", "python"), []byte("#!/bin/sh\n"), 0o755)
	cfg, _ := testDebugConfig(tests.FrameworkPytest, dir, filepath.Join(dir, "test_a.py"), "test_a")
	if cfg.Raw["python"] != filepath.Join(dir, ".venv", "bin", "python") {
		t.Errorf("python = %v", cfg.Raw["python"])
	}
	if c, err := testDebugConfig(tests.FrameworkPytest, "relative-root", "/abs/file.py", "x"); err != nil || c.Name == "" {
		t.Errorf("rel fail: %v", err)
	}
	if _, err := testDebugConfig(tests.FrameworkGo, dir, file, "TestA/sub"); err != nil {
		t.Error(err)
	}
	// rows
	m.testView = newTestsState()
	m.testView.tree = &tests.Tree{Root: dir, FW: tests.FrameworkGo}
	c := &tests.Case{Name: "TestThing", File: file}
	if m.debugTestRow(testRow{kind: testRowCase, c: c}) == nil || m.debugTestRow(testRow{kind: testRowMessage, c: c}) == nil {
		t.Error("case rows start")
	}
	if m.debugTestRow(testRow{kind: testRowPackage}) == nil {
		t.Error("package row toasts")
	}
	// at the cursor: Go function, not a test, python file via the tree
	openAt(t, &m, file, 9)
	if m.debugTestAtCursor() == nil {
		t.Error("inside a test")
	}
	openAt(t, &m, file, 4)
	if m.debugTestAtCursor() == nil {
		t.Error("not in a test: toast")
	}
	py := filepath.Join(dir, "test_a.py")
	_ = os.WriteFile(py, []byte("def test_a():\n    pass\n"), 0o644)
	m.testView.tree = &tests.Tree{Root: dir, FW: tests.FrameworkPytest, Packages: []*tests.Package{{Dir: dir, Files: []*tests.File{{Path: py, Cases: []*tests.Case{{Name: "test_a", File: py, Line: 1}}}}}}}
	openAt(t, &m, py, 2)
	if m.debugTestAtCursor() == nil {
		t.Error("python test at cursor")
	}
	m.cursorLine = 0
	if m.debugTestAtCursor() == nil {
		t.Error("no test above: toast")
	}
	var e Model
	if e.debugTestAtCursor() != nil {
		t.Error("no file")
	}
	// no discovered tree and not a _test.go: detect from the workspace
	m.testView = nil
	fw, _ := m.testFrameworkFor(filepath.Join(dir, "x.py"))
	_ = fw
}
