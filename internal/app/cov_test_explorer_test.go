package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/statusbar"
	"github.com/amin-jalali/termocode/internal/tests"
)

// Coverage of test_explorer.go: keys, mouse, rendering, the TEST RESULTS tab
// and palette commands, on a real nvim model whose tree is a small fixture.

// explorerModel is a real-nvim model showing the fixture tree, after one
// recorded run so some tests failed. The files exist on disk so opening
// them works.
func explorerModel(t *testing.T) Model {
	t.Helper()
	dir := t.TempDir()
	m := newTestModel(t)
	if m.nvim == nil {
		t.Skip("no nvim")
	}
	f := testsModel()
	m.w, m.h = 120, 40
	m.showExp, m.explorerWidth, m.focus = true, defaultExplorerWidth, FocusExplorer
	m.testView = f.testView
	m.activity.SetActive(activity.ViewTests)
	replayGoRun(t, &m)
	t.Chdir(dir)
	for _, p := range m.testView.tree.Packages {
		p.Dir = filepath.Join(dir, filepath.Base(p.Dir))
		_ = os.MkdirAll(p.Dir, 0o755)
		for _, fl := range p.Files {
			fl.Path = filepath.Join(p.Dir, filepath.Base(fl.Path))
			_ = os.WriteFile(fl.Path, []byte("package x\n\nfunc a() {}\n"), 0o644)
			for _, c := range fl.Cases {
				c.File = fl.Path
			}
		}
	}
	m.testView.tree.Root = dir
	return m
}

func keyOf(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestCovExplorerKeysWalkTheTree(t *testing.T) {
	m := explorerModel(t)
	fakeBin(t, nil) // runs end at "not found on PATH"
	keys := []string{"down", "j", "pgdown", "up", "k", "pgup", "end", "G", "home", "g", "right", "l", "left", "h", "left", " ", " ",
		"enter", "down", "enter", "down", "down", "enter", "r", "d", "o", "R", "f", "u", "c", "x", "esc", "zzz"}
	for _, k := range keys {
		m, _ = m.handleTestsSidebarKey(keyOf(k))
		m.testView.running = false
		m.testView.discovering = false
		m.testView.pending = nil
	}
	// h on a leaf walks to its parent
	m.testView.collapsed = map[string]bool{}
	rows := m.testView.rows()
	for i, r := range rows {
		if r.kind == testRowCase {
			m.testView.cursor = i
			break
		}
	}
	m, _ = m.handleTestsSidebarKey(keyOf("h"))
	if rows[m.testView.cursor].kind == testRowCase {
		t.Error("h on a test should move to its parent")
	}
	// every row kind: run, open, debug
	for _, r := range m.testView.rows() {
		m.runTestRow(r)
		m.openTestRow(r)
		m.testView.running = false
	}
	m.testView.tree.Packages[0].Error = "build failed\nsecond line"
	m.testView.tree.Packages[0].ErrFile = m.testView.tree.Packages[0].Files[0].Path
	m.testView.tree.Packages[0].ErrLine = 2
	for _, r := range m.testView.rows() {
		if r.kind == testRowPkgError {
			m.runTestRow(r)
			m.openTestRow(r)
		}
	}
	empty := m
	empty.testView = newTestsState()
	empty.testView.tree = &tests.Tree{}
	empty, _ = empty.handleTestsSidebarKey(keyOf("enter"))
	empty, _ = empty.handleTestsSidebarKey(keyOf("r"))
	empty, _ = empty.handleTestsSidebarKey(keyOf("left"))
}

func TestCovExplorerMouse(t *testing.T) {
	m := explorerModel(t)
	fakeBin(t, nil)
	m, _ = m.handleTestsSidebarMouse(3, 8, tea.MouseWheelUp)
	m, _ = m.handleTestsSidebarMouse(3, 8, tea.MouseWheelDown)
	m, _ = m.handleTestsSidebarMouse(3, 8, tea.MouseMotion)
	// action bar buttons
	w := m.explorerWidth - 1
	for _, a := range testsActionLayout(false, true, w) {
		m, _ = m.handleTestsSidebarMouse(a.start, testsActionRow, tea.MouseLeft)
		m.testView.running = false
		m.testView.discovering = false
	}
	m, _ = m.handleTestsSidebarMouse(0, testsActionRow, tea.MouseLeft) // between buttons
	// a stop button while running
	m.testView.running = true
	for _, a := range testsActionLayout(true, false, w) {
		if a.id == "stop" {
			m, _ = m.handleTestsSidebarMouse(a.start, testsActionRow, tea.MouseLeft)
		}
	}
	// header / out of range rows
	m, _ = m.handleTestsSidebarMouse(3, 0, tea.MouseLeft)
	m, _ = m.handleTestsSidebarMouse(3, 39, tea.MouseLeft)
	// click each visible tree row on its label and on its run glyph
	for i := range m.testView.rows() {
		m, _ = m.handleTestsSidebarMouse(4, testsHeaderRows+i, tea.MouseLeft)
		m.testView.running = false
		m, _ = m.handleTestsSidebarMouse(w-1, testsHeaderRows+i, tea.MouseLeft)
		m.testView.running = false
	}
	if got := m.testsAction("nothing"); got != nil {
		t.Error("unknown action")
	}
	// layout drops buttons that do not fit
	if len(testsActionLayout(true, true, 12)) >= len(testsActionLayout(false, true, 60)) {
		t.Error("narrow bars show fewer buttons")
	}
	_ = testsActionLayout(false, false, 0)
}

func TestCovExplorerRender(t *testing.T) {
	m := explorerModel(t)
	st := m.testView
	for _, h := range []int{8, 12, 30} {
		for _, foc := range []bool{true, false} {
			m.focus = FocusEditor
			if foc {
				m.focus = FocusExplorer
			}
			for cur := range st.rows() {
				st.cursor = cur
				out := m.renderTestsSidebar(m.explorerWidth, h)
				if !strings.Contains(out, "TESTING") && h > 8 {
					t.Fatalf("no title in %q", out)
				}
			}
		}
	}
	// states
	st.err = "boom"
	st.tree.Packages = nil
	_ = m.renderTestsSidebar(40, 20)
	st.tree.Packages = nil
	st.tree.FW = tests.FrameworkNone
	_ = m.renderTestsSidebar(40, 20)
	st.err = ""
	st.runError = "it broke\nmore"
	_ = m.renderTestsSidebar(40, 20)
	st.runError = ""
	st.tree.FW = tests.FrameworkGo
	_ = m.renderTestsSidebar(40, 20) // no tests found
	st.discovering = true
	_ = m.renderTestsSidebar(40, 20)
	st.tree = nil
	_ = m.renderTestsSidebar(40, 20)
	st.running, st.started = true, time.Now()
	st.tree = &tests.Tree{FW: tests.FrameworkGo}
	_ = m.renderTestsSidebar(40, 20)
	st.running, st.hasRun, st.elapsed = false, true, time.Second
	_ = m.renderTestsSidebar(40, 20)
	for _, d := range []time.Duration{time.Microsecond, 50 * time.Millisecond, 2500 * time.Millisecond} {
		if formatTestDuration(d) == "" {
			t.Error("duration")
		}
	}
	for s := tests.StatusPending; s <= tests.StatusSkipped; s++ {
		if g, _ := testStatusGlyph(s); g == "" {
			t.Error("glyph")
		}
	}
	if testsFill(0) != "" || testsPkgLabel("/a", &tests.Package{Dir: "/a"}) != "a" || testsPkgLabel("/a", &tests.Package{Dir: "/a/b"}) != "b" {
		t.Error("labels")
	}
	if _, w := renderTestsCounts(tests.Counts{}, 0); w == 0 {
		t.Error("empty counts render the pending mark")
	}
	_, _ = renderTestsCounts(tests.Counts{Passed: 1, Failed: 2, Skipped: 3, Running: 4}, 0)
}

func TestCovExplorerResultsTab(t *testing.T) {
	m := explorerModel(t)
	st := m.testView
	var failed *tests.Case
	st.tree.Each(func(_ *tests.Package, _ *tests.File, c *tests.Case) {
		if c.Status == tests.StatusFailed && failed == nil {
			failed = c
		}
	})
	if failed == nil {
		t.Fatal("fixture has a failed test")
	}
	// log lines of every colour class
	st.log = []string{"$ go test", "--- FAIL: TestX", "ok pkg", "--- SKIP: x", "plain", "calc/calc_test.go:7: boom " + strings.Repeat("x", 200), "[done in 1s]", "panic: x", "E  assert", "t PASSED", "t SKIPPED", "a ... ignored"}
	for _, ln := range st.log {
		_ = testResultLineColor(ln)
	}
	for _, narrowed := range []string{"", failed.Key()} {
		st.resultsFor = narrowed
		for _, h := range []int{1, 2, 12} {
			if got := renderTestResultsTab(&m, 80, h); len(got) == 0 {
				t.Fatal("no rows")
			}
		}
	}
	failed.Output = nil
	failed.Message = "line1\nline2"
	lines, label := st.testResultsLines()
	if len(lines) != 2 || label != failed.Name {
		t.Errorf("lines %v label %q", lines, label)
	}
	failed.Message = ""
	if lines, _ := st.testResultsLines(); len(lines) != 1 || !strings.Contains(lines[0], "no output") {
		t.Errorf("lines %v", lines)
	}
	failed.Output = []string{"a", "b"}
	st.resultsFor = "unknown-key"
	if lines, label := st.testResultsLines(); label != "" || len(lines) != len(st.log) {
		t.Error("unknown key shows the whole log")
	}
	st.log = nil
	st.resultsFor = ""
	if got := renderTestResultsTab(&m, 80, 6); !strings.Contains(strings.Join(got, "\n"), "No test run yet") {
		t.Error("empty tab hint")
	}
	if s, e := testResultsHeaderLayout("a very long label " + strings.Repeat("y", 100), 40); s != 20 || e != 40 {
		t.Errorf("chip %d..%d", s, e)
	}

	// panel-kind callbacks: scroll, click, key
	m.showTestOutput(&testRow{c: failed})
	spec := panelKinds[panelKindTests]
	st.log = make([]string, 100)
	for i := range st.log {
		st.log[i] = "calc/calc_test.go:7: line"
	}
	st.resultsFor = failed.Key()
	spec.Scroll(&m, 3)
	spec.Scroll(&m, -3)
	for _, k := range []string{"up", "k", "down", "j", "pgup", "pgdown", "home", "g", "end", "G", "a", "x"} {
		_, _ = spec.Key(&m, keyOf(k))
	}
	_, _, w, _, _ := m.panelContentRect()
	st.resultsFor = failed.Key()
	spec.Click(&m, 0, w-1)
	st.resultsFor = failed.Key()
	spec.Click(&m, 0, 0)
	st.resultsFor = ""
	for row := 1; row < 8; row++ {
		spec.Click(&m, row, 3)
	}
	spec.Click(&m, 500, 3)
}

func TestCovExplorerResolveLocation(t *testing.T) {
	m := explorerModel(t)
	st := m.testView
	f := st.tree.Packages[0].Files[0].Path
	if st.resolveTestLocation(f) != f || st.resolveTestLocation("/nope/x.go") != "" {
		t.Error("absolute paths")
	}
	rel, _ := filepath.Rel(st.tree.Root, f)
	if st.resolveTestLocation(rel) != f {
		t.Error("relative to the root")
	}
	base := filepath.Base(f)
	if st.resolveTestLocation(base) != f {
		t.Error("relative to a package dir")
	}
	st.resultsFor = st.tree.Packages[0].Files[0].Cases[0].Key()
	if st.resolveTestLocation(base) != f || st.resolveTestLocation("zzz.go") != "" {
		t.Error("narrowed test dir / unknown")
	}
	st.tree = nil
	if st.resolveTestLocation("x.go") != "" {
		t.Error("no tree")
	}
}

func TestCovExplorerPaletteAndCodeActions(t *testing.T) {
	m := explorerModel(t)
	fakeBin(t, nil)
	file := m.testView.tree.Packages[0].Files[0].Path
	for _, id := range []string{"testing-focus", "testing-run-all", "testing-rerun-failed", "testing-run-file", "testing-run-cursor",
		"testing-stop", "testing-refresh", "testing-collapse", "testing-results", "testing-debug-cursor", "testing-run-file"} {
		if id == "testing-run-file" && !strings.HasSuffix(m.editor.Path(), "x") {
			_ = m.nvim.ExecLua(`vim.cmd('edit ` + file + `')`)
		}
		if _, ok := m.dispatchTestingPalette(id); !ok {
			t.Errorf("%s not handled", id)
		}
		m.testView.running, m.testView.discovering, m.testView.pending = false, false, nil
	}
	if _, ok := m.dispatchTestingPalette("nope"); ok {
		t.Error("unknown id")
	}
	// synth tests
	for _, a := range []synthCodeAction{{Path: file, Command: "file"}, {Path: file}, {Path: file, FuncName: "TestAdd"}, {Path: "x.py", FuncName: "test_a"}} {
		m.runSynthTest(a)
		m.testView.running, m.testView.pending = false, nil
	}
	// non-Go code actions come from the tree
	pyDir := t.TempDir()
	py := filepath.Join(pyDir, "test_a.py")
	m.testView.tree.FW = tests.FrameworkPytest
	m.testView.tree.Packages = []*tests.Package{{Dir: pyDir, Files: []*tests.File{{Path: py, Cases: []*tests.Case{{Name: "test_a", File: py, Line: 3}}}}}}
	var got []string
	add := func(title, kind string, _ synthCodeAction) { got = append(got, title) }
	m.testCodeActions(py, 4, add)
	m.testCodeActions(py, 1, add)
	m.testCodeActions("", 1, add)
	m.testCodeActions(filepath.Join(pyDir, "other.py"), 1, add)
	m.testCodeActions("x_test.go", 1, add)
	if len(got) < 3 {
		t.Errorf("actions %v", got)
	}
	if testCaseAtLine(m.testView.tree.Packages[0].Files[0], 1) != nil {
		t.Error("nothing above line 1")
	}
}

func TestCovExplorerStatusChip(t *testing.T) {
	m := explorerModel(t)
	var s statusbar.State
	m.fillTestsStatus(&s)
	if !s.ShowTests || s.TestsFailed == 0 {
		t.Errorf("state %+v", s)
	}
	m.testView.hasRun = false
	s = statusbar.State{}
	m.fillTestsStatus(&s)
	if s.ShowTests {
		t.Error("no chip before a run")
	}
	_ = m.hitStatusTestsChip(0, 0)
	m.testsCollapseAll()
	(&Model{}).testsCollapseAll()
	m.testView.cursor = 99
	m.testsClampCursor()
	if m.ensureTestsDiscovered() != nil {
		t.Error("already discovered")
	}
	m.testView.discovered = false
	if m.ensureTestsDiscovered() == nil {
		t.Error("discovers once")
	}
	m.openTestLocation("", 1)
	m.openTestLocation(m.testView.tree.Packages[0].Files[0].Path, 0)
}
