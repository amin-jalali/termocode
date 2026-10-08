package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/tests"
)

// ── Debug this test (glue D + E) ─────────────────────────────────────────
//
// `d` on a test row in the TESTS tree, the Ctrl+. "Debug Test: X" code
// action and the "Testing: Debug Test at Cursor" palette entry build a
// launch config for one test and start it through the Run & Debug path
// (debugStartConfig): the same adapter gate, Run view hint, Debug Console
// and breakpoints as any other session.
//
//	Go     → delve, mode "test", program = the package dir,
//	         args -test.run ^Name$ (subtests: ^Parent$/^sub$)
//	pytest → debugpy, module "pytest", args = the node id (file::name)
//
// Other runners (cargo, jest, vitest) have no debug mapping yet: a toast
// says so instead of guessing.

var errNoTestDebug = errors.New("debugging tests works for Go and pytest only")

// goTestRunPattern anchors each level of a (sub)test name for -test.run.
func goTestRunPattern(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		parts[i] = "^" + p + "$"
	}
	return strings.Join(parts, "/")
}

// testPython picks the interpreter that runs pytest under debugpy: the
// project's virtualenv when there is one, else python3 / python on PATH.
func testPython(root string) string {
	for _, rel := range []string{".venv/bin/python", "venv/bin/python"} {
		p := filepath.Join(root, rel)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return pythonExecutable()
}

// testDebugConfig builds the launch config that debugs one test. Pure
// apart from testPython's stat; root is the test tree's root.
func testDebugConfig(fw tests.Framework, root, file, name string) (launchConfig, error) {
	title := "Debug Test: " + name
	switch fw {
	case tests.FrameworkGo:
		dir := filepath.Dir(file)
		args := []any{"-test.run", goTestRunPattern(name)}
		raw := map[string]any{
			"name": title, "type": "delve", "request": "launch",
			"mode": "test", "program": dir, "args": args,
		}
		return launchConfig{Name: title, Type: "delve", Request: "launch", Program: dir,
			Source: "test", Raw: raw}, nil
	case tests.FrameworkPytest:
		rel, err := filepath.Rel(root, file)
		if err != nil {
			rel = file
		}
		nodeID := filepath.ToSlash(rel) + "::" + name
		raw := map[string]any{
			"name": title, "type": "python", "request": "launch",
			"module": "pytest", "args": []any{nodeID, "-q"},
			"cwd": root, "console": "internalConsole", "justMyCode": false,
		}
		if py := testPython(root); py != "" {
			raw["python"] = py
		}
		return launchConfig{Name: title, Type: "python", Request: "launch", Cwd: root,
			Source: "test", Raw: raw}, nil
	}
	return launchConfig{}, errNoTestDebug
}

// testFrameworkFor is the framework that owns file: the discovered tree's
// when there is one, else Go for *_test.go (the Go code action works
// before discovery).
func (m *Model) testFrameworkFor(file string) (tests.Framework, string) {
	if st := m.testView; st != nil && st.tree != nil && st.tree.FW != tests.FrameworkNone {
		return st.tree.FW, st.tree.Root
	}
	if strings.HasSuffix(file, "_test.go") {
		return tests.FrameworkGo, workspaceRoot()
	}
	return tests.Detect(workspaceRoot()), workspaceRoot()
}

// debugTest starts a debug session for the test name declared in file.
func (m *Model) debugTest(file, name string) tea.Cmd {
	if m.nvim == nil || file == "" || name == "" {
		return nil
	}
	if m.dapSessionActive {
		return m.debugToast("a session is running — stop it first (Shift+Alt+F5)")
	}
	if abs, err := filepath.Abs(file); err == nil {
		file = abs
	}
	fw, root := m.testFrameworkFor(file)
	cfg, err := testDebugConfig(fw, root, file, name)
	if err != nil {
		return m.debugToast(err.Error())
	}
	return m.debugStartConfig(cfg)
}

// debugTestRow is `d` in the TESTS tree.
func (m *Model) debugTestRow(r testRow) tea.Cmd {
	switch r.kind {
	case testRowCase, testRowMessage:
		return m.debugTest(r.c.File, r.c.Name)
	}
	return m.debugToast("select a test to debug")
}

// debugTestAtCursor is the palette entry: the test around the cursor.
func (m *Model) debugTestAtCursor() tea.Cmd {
	path := m.editor.Path()
	if path == "" {
		return nil
	}
	if strings.HasSuffix(path, ".go") {
		if fn := m.enclosingGoFuncName(m.cursorLine); isGoTestFunc(fn) {
			return m.debugTest(path, fn)
		}
		return m.debugToast("cursor is not inside a test function")
	}
	if st := m.testView; st != nil && st.tree != nil {
		abs, _ := filepath.Abs(path)
		if _, f := st.tree.FindFile(abs); f != nil {
			if c := testCaseAtLine(f, m.cursorLine); c != nil {
				return m.debugTest(c.File, c.Name)
			}
		}
	}
	return m.debugToast("no test at the cursor — open the Testing view to discover tests")
}
