package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/tests"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// Coverage of test_runner.go: a real discovery walk over a temp Go module and
// real runs whose `go` is a script that prints canned `go test -json`.

func goTestProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	_ = os.WriteFile("go.mod", []byte("module example.com/demo\n\ngo 1.22\n"), 0o644)
	_ = os.WriteFile("a_test.go", []byte("package demo\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n\nfunc TestB(t *testing.T) {}\n"), 0o644)
	return dir
}

func runnerModel() Model {
	m := Model{w: 120, h: 30, toast: toast.New(), testView: newTestsState()}
	return m
}

// drain runs a Cmd chain (the streaming reader) and returns every message.
func drainTestCmd(t *testing.T, cmd tea.Cmd, m *Model) {
	t.Helper()
	for cmd != nil {
		switch msg := cmd().(type) {
		case testLinesMsg:
			cmd = m.applyTestLines(msg)
		case testDoneMsg:
			m.finishTestRun(msg)
			return
		default:
			return
		}
	}
}

func TestCovRunnerDiscoverAndPending(t *testing.T) {
	goTestProject(t)
	m := runnerModel()
	msg := discoverTestsCmd(m.workspace())().(testsDiscoveredMsg)
	if msg.err != nil || msg.tree == nil || msg.tree.FW != tests.FrameworkGo {
		t.Fatalf("discover: %+v", msg)
	}
	// no framework
	t.Chdir(t.TempDir())
	none := discoverTestsCmd(m.workspace())().(testsDiscoveredMsg)
	if !errors.Is(none.err, tests.ErrNoFramework) {
		t.Fatalf("err = %v", none.err)
	}
	// no workspace root
	if r := discoverTestsCmd(workspace.Workspace{})().(testsDiscoveredMsg); r.err == nil {
		t.Error("no root must fail")
	}
	// refresh: not twice at once, nil state is fine
	if (&Model{}).refreshTests() != nil {
		t.Error("no state")
	}
	if m.refreshTests() == nil || m.refreshTests() != nil {
		t.Error("second refresh while discovering must be a no-op")
	}
	// A request queued before discovery runs once the tree arrives.
	goTestProject(t)
	m2 := runnerModel()
	cmd := m2.runTestsRequest(testsRunRequest{all: true})
	if cmd == nil || m2.testView.pending == nil {
		t.Fatal("request must wait for discovery")
	}
	fakeBin(t, map[string]string{"go": `echo '{"Action":"run","Package":"example.com/demo","Test":"TestA"}'; echo '{"Action":"pass","Package":"example.com/demo","Test":"TestA","Elapsed":0}'`})
	found := cmd().(testsDiscoveredMsg)
	run := m2.applyTestsDiscovered(found)
	if run == nil || m2.testView.pending != nil || !m2.testView.running {
		t.Fatal("pending request must start")
	}
	drainTestCmd(t, run, &m2)
	if m2.testView.running || !m2.testView.hasRun || m2.testView.tree.Counts().Passed != 1 {
		t.Fatalf("after run: running=%v counts=%+v", m2.testView.running, m2.testView.tree.Counts())
	}
	// A second discovery keeps results (carry over) and records an error.
	m2.applyTestsDiscovered(testsDiscoveredMsg{tree: found.tree, err: errors.New("walk failed")})
	if m2.testView.err != "walk failed" {
		t.Errorf("err = %q", m2.testView.err)
	}
}

func TestCovRunnerRequestResolution(t *testing.T) {
	m := testsModel()
	st := m.testView
	st.tree.FW = tests.FrameworkNone
	if _, p := m.tryTestsRequest(testsRunRequest{all: true}); p == nil {
		t.Error("no framework is a problem")
	}
	if cmd := m.runTestsRequest(testsRunRequest{all: true}); cmd == nil {
		t.Error("problem must toast")
	}
	st.tree.FW = tests.FrameworkGo
	fakeBin(t, nil) // nothing can start: requests that resolve end in the PATH problem
	for name, req := range map[string]testsRunRequest{
		"all":     {all: true},
		"pkg":     {pkgDir: "/w/calc"},
		"pkg new": {pkgDir: "/w/other"},
		"file":    {file: "/w/calc/calc_test.go"},
		"line":    {file: "/w/calc/calc_test.go", atLine: 14},
		"name":    {name: "TestAdd"},
		"part":    {name: "Add"},
		"keys":    {file: "/w/calc/calc_test.go", caseKey: []string{st.tree.Packages[0].Files[0].Cases[0].Key()}},
		"goFunc":  {file: "/w/x_test.go", caseKey: []string{"nope"}, goFunc: "TestNew"},
	} {
		if _, p := m.tryTestsRequest(req); p != nil {
			t.Errorf("%s: unexpected problem %v", name, p.msg)
		}
		if st.runError == "" || !strings.Contains(st.runError, "not found on PATH") {
			t.Errorf("%s: runError %q", name, st.runError)
		}
		st.runError = ""
	}
	for name, req := range map[string]testsRunRequest{
		"no failed":  {failed: true},
		"no name":    {name: "Zzz"},
		"no file":    {file: "/w/none_test.go"},
		"no key":     {caseKey: []string{"zzz"}},
		"fail file":  {file: "/w/calc/calc_test.go", caseKey: nil, atLine: 0, name: "", failed: true},
		"key nofunc": {file: "/w/calc/calc_test.go", caseKey: []string{"zzz"}},
	} {
		if _, p := m.tryTestsRequest(req); p == nil {
			t.Errorf("%s: want a problem", name)
		}
	}
	// failed tests are rerun
	st.tree.Packages[0].Files[0].Cases[1].Status = tests.StatusFailed
	if _, p := m.tryTestsRequest(testsRunRequest{failed: true}); p != nil {
		t.Errorf("failed rerun: %v", p.msg)
	}
	// nil state
	if cmd, p := (&Model{}).tryTestsRequest(testsRunRequest{}); cmd != nil || p != nil {
		t.Error("nil state does nothing")
	}
	if len(testsByName(st.tree, "TestAdd")) != 1 {
		t.Error("exact match wins over partial")
	}
}

func TestCovRunnerRealRunFailsAndStops(t *testing.T) {
	goTestProject(t)
	m := runnerModel()
	m.testView.discovered = true
	pk, _ := tests.Discover(".", tests.FrameworkGo)
	root, _ := os.Getwd()
	m.testView.tree = &tests.Tree{Root: root, FW: tests.FrameworkGo, Packages: pk}
	fakeBin(t, map[string]string{"go": `echo '{"Action":"run","Package":"example.com/demo","Test":"TestA"}'; echo '{"Action":"output","Package":"example.com/demo","Test":"TestA","Output":"boom\n"}'; echo '{"Action":"fail","Package":"example.com/demo","Test":"TestA","Elapsed":0}'; echo plain text; exit 1`})
	cmd := m.runTestsCmd()
	if cmd == nil {
		t.Fatal("no run")
	}
	drainTestCmd(t, cmd, &m)
	c := m.testView.tree.Counts()
	if c.Failed != 1 || m.testView.running {
		t.Fatalf("counts %+v running %v", c, m.testView.running)
	}
	if !strings.Contains(strings.Join(m.testView.log, "\n"), "[done in") {
		t.Error("run log has no summary")
	}
	// A run that parses nothing and fails: the log tail is the reason.
	fakeBin(t, map[string]string{"go": `echo "cannot load package"; exit 2`})
	m.testView.tree.Each(func(_ *tests.Package, _ *tests.File, c *tests.Case) { c.Status = tests.StatusPending })
	for _, p := range m.testView.tree.Packages {
		p.Error = ""
	}
	drainTestCmd(t, m.runTestsCmd(), &m)
	if !strings.Contains(m.testView.runError, "cannot load package") {
		t.Errorf("runError = %q", m.testView.runError)
	}
	// Stopping: a long run is killed, its log says so, the live set empties.
	fakeBin(t, map[string]string{"go": `sleep 30`})
	cmd = m.runTestsCmd()
	if !m.testView.running || m.testView.proc == nil {
		t.Fatal("not running")
	}
	m.stopTestRun()
	if m.testView.running || m.testView.log[len(m.testView.log)-1] != "[stopped]" {
		t.Errorf("log %v", m.testView.log)
	}
	_ = cmd // its goroutines end with the killed process
	// StopTestRuns kills everything that still runs.
	_ = m.runTestsCmd()
	StopTestRuns()
	m.stopTestRun()
	m.stopTestRun() // not running: no-op
	(&Model{}).stopTestRun()
}

func TestCovRunnerStartProblems(t *testing.T) {
	goTestProject(t)
	m := runnerModel()
	root, _ := os.Getwd()
	pk, _ := tests.Discover(".", tests.FrameworkGo)
	m.testView.discovered = true
	m.testView.tree = &tests.Tree{Root: root, FW: tests.FrameworkGo, Packages: pk}
	// a command that cannot start (not executable)
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go"), []byte("not a program"), 0o755)
	t.Setenv("PATH", dir)
	if m.startTestRun(tests.Scope{Kind: tests.ScopeAll}) == nil || m.testView.running {
		t.Errorf("start failure: running=%v err=%q", m.testView.running, m.testView.runError)
	}
	// pytest falls back to python3 -m pytest
	m.testView.tree.FW = tests.FrameworkPytest
	fakeBin(t, map[string]string{"python3": "echo ran $@"})
	if m.startTestRun(tests.Scope{Kind: tests.ScopeAll}) == nil || !m.testView.running {
		t.Error("pytest fallback should start python3")
	}
	m.stopTestRun()
	// jest report path
	m.testView.tree.FW = tests.FrameworkJest
	fakeBin(t, map[string]string{"npx": "echo ran", "jest": "echo ran", "node": "echo ran", "npm": "echo ran"})
	_ = m.startTestRun(tests.Scope{Kind: tests.ScopeAll})
	m.stopTestRun()
}

func TestCovRunnerFinishReports(t *testing.T) {
	m := testsModel()
	st := m.testView
	st.runID, st.running = 3, true
	st.parser = tests.NewLineParser(tests.FrameworkPytest)
	st.tree.FW = tests.FrameworkPytest
	m.finishTestRun(testDoneMsg{runID: 3, report: []byte("<not xml")})
	if !strings.Contains(strings.Join(st.log, "\n"), "[report:") {
		t.Errorf("bad report not logged: %v", st.log)
	}
	st.running, st.runID = true, 4
	st.tree.FW = tests.FrameworkJest
	st.parser = tests.NewLineParser(tests.FrameworkJest)
	m.finishTestRun(testDoneMsg{runID: 4, report: []byte("{bad")})
	// stale / not running
	if m.finishTestRun(testDoneMsg{runID: 99}) != nil || (&Model{}).finishTestRun(testDoneMsg{}) != nil {
		t.Error("stale message ignored")
	}
	if m.applyTestLines(testLinesMsg{runID: 99}) != nil {
		t.Error("stale lines drop")
	}
}

func TestCovRunnerHelpers(t *testing.T) {
	if testsAnyPackageError(nil) {
		t.Error("nil tree")
	}
	if !testsAnyPackageError(&tests.Tree{Packages: []*tests.Package{{Error: "x"}}}) {
		t.Error("package error")
	}
	if got := testsLogTail([]string{"$ cmd", "", "{json}", "a", "b", "c", "d"}, 3); got != "b\nc\nd" {
		t.Errorf("tail = %q", got)
	}
	if !strings.Contains(testsLogTail([]string{"$ x"}, 3), "exited with an error") {
		t.Error("empty tail")
	}
	out := colourTestOutput("FAIL pkg\nok  pkg\nPASS\n--- PASS: T\n=== RUN T\nplain\nFAIL\t0")
	for _, want := range []string{"FAIL pkg", "ok  pkg", "plain", "=== RUN"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	st := newTestsState()
	st.resultsScroll = 2
	for i := 0; i < testLogMax+5; i++ {
		st.log = append(st.log, "x")
	}
	st.appendLog("y")
	if len(st.log) > testLogMax {
		t.Errorf("log %d", len(st.log))
	}
	// batching of queued lines
	lines := make(chan string, 600)
	done := make(chan error, 1)
	for i := 0; i < 600; i++ {
		lines <- "l"
	}
	close(lines)
	done <- nil
	msg := waitTestLines(1, lines, done, "")().(testLinesMsg)
	if len(msg.lines) != 500 {
		t.Errorf("batch = %d", len(msg.lines))
	}
	msg = msg.next().(testLinesMsg)
	if len(msg.lines) != 100 {
		t.Errorf("rest = %d", len(msg.lines))
	}
	rep := filepath.Join(t.TempDir(), "r.json")
	_ = os.WriteFile(rep, []byte("{}"), 0o644)
	d := msg.next().(testDoneMsg)
	_ = d
	l2 := make(chan string)
	close(l2)
	d2 := waitTestLines(2, l2, done2(), rep)().(testDoneMsg)
	if string(d2.report) != "{}" {
		t.Errorf("report = %q", d2.report)
	}
	if _, err := os.Stat(rep); err == nil {
		t.Error("report file must be removed")
	}
}

func done2() <-chan error {
	c := make(chan error, 1)
	c <- nil
	return c
}
