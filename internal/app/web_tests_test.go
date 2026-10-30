package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/tests"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// webTestTestsTree opens a workspace whose TESTING view found one Go
// package with one file and two tests (as a discovery would).
func webTestTestsTree(t *testing.T, m *Model) (dir string, a, b *tests.Case) {
	t.Helper()
	dir = t.TempDir()
	webTestWrite(t, dir, "pkg/a_test.go", "package pkg\n\nfunc TestA(t *testing.T) {}\n\nfunc TestB(t *testing.T) {}\n")
	m.ws = workspace.Workspace{Root: dir}
	file := filepath.Join(dir, "pkg", "a_test.go")
	a = &tests.Case{Name: "TestA", File: file, Line: 3}
	b = &tests.Case{Name: "TestB", File: file, Line: 5}
	m.testView = newTestsState()
	m.testView.tree = &tests.Tree{Root: dir, FW: tests.FrameworkGo, Packages: []*tests.Package{{
		Name: "example.com/m/pkg", Dir: filepath.Join(dir, "pkg"),
		Files: []*tests.File{{Path: file, Rel: "pkg/a_test.go", Cases: []*tests.Case{a, b}}},
	}}}
	m.testView.discovered = true
	return dir, a, b
}

// webTestFakeTestRuns swaps the runner's start for a fake that keeps the
// books as a real start does (the run's cases turn running); reqs gets
// what was asked.
func webTestFakeTestRuns(t *testing.T) *[]testsRunRequest {
	t.Helper()
	var reqs []testsRunRequest
	old := webStartTests
	webStartTests = func(m *Model, req testsRunRequest) (tea.Cmd, *testsRunProblem) {
		reqs = append(reqs, req)
		st := m.testView
		want := map[string]bool{}
		for _, k := range req.caseKey {
			want[k] = true
		}
		st.runID++
		st.tree.MarkRunning(func(c *tests.Case) bool { return req.all || want[c.Key()] })
		st.running, st.hasRun, st.started = true, false, webNow()
		st.cmd = tests.Command{Args: []string{"go", "test", "-json", "./..."}}
		return nil, nil
	}
	t.Cleanup(func() { webStartTests = old })
	return &reqs
}

func webTestTests(t *testing.T, frames []map[string]any) (webproto.TestsSnap, int) {
	t.Helper()
	var s webproto.TestsSnap
	n := 0
	for _, f := range frames {
		if f["topic"] == "tests" {
			s = webproto.TestsSnap{}
			webTestData(t, f, &s)
			n++
		}
	}
	return s, n
}

// webTestFinishRun ends the fake run: TestA passed, TestB failed.
func webTestFinishRun(m *Model, a, b *tests.Case) {
	st := m.testView
	a.Status, a.Duration = tests.StatusPassed, 12*time.Millisecond
	b.Status, b.Message, b.FailFile, b.FailLine = tests.StatusFailed, "a_test.go:5: want 2, got 3", a.File, 5
	b.Output = []string{"=== RUN   TestB", "    a_test.go:5: want 2, got 3", "--- FAIL: TestB (0.00s)"}
	st.results += 2
	st.running, st.hasRun, st.elapsed = false, true, 40*time.Millisecond
	st.appendLog("ok", "FAIL")
}

// Spec (WEBP-4): the tests topic is the laptop's TESTING tree with
// workspace paths: a package, its file, its tests with their state;
// tests.run runs a row by its id (the laptop's own request, never a
// command), the run and its results each send the tree again, and the
// run is audited. tests.output answers a test's own output.
func TestTestsTopicAndRun(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	now := webTestClock(t)
	webTestHub(t, &m)
	_, a, b := webTestTestsTree(t, &m)
	reqs := webTestFakeTestRuns(t)
	c := webTestSub(t, &m, "tests")
	s, _ := webTestTests(t, webTestNow(t, c))
	if s.Framework != "go test" || s.State != "idle" || s.Counts.Total != 2 || len(s.Items) != 4 {
		t.Fatalf("snap %+v", s)
	}
	want := []webproto.TestItem{
		{ID: "p:pkg", Kind: "package", Label: "pkg", State: "pending"},
		{ID: "f:pkg/a_test.go", Kind: "file", Parent: "p:pkg", Label: "a_test.go", Path: "pkg/a_test.go", State: "pending"},
		{ID: "c:pkg/a_test.go::TestA", Kind: "case", Parent: "f:pkg/a_test.go", Label: "TestA", Path: "pkg/a_test.go", Line: 3, State: "pending", Debug: true},
		{ID: "c:pkg/a_test.go::TestB", Kind: "case", Parent: "f:pkg/a_test.go", Label: "TestB", Path: "pkg/a_test.go", Line: 5, State: "pending", Debug: true},
	}
	for i, w := range want {
		if s.Items[i] != w {
			t.Errorf("item %d %+v, want %+v", i, s.Items[i], w)
		}
	}

	if f, _ := webTestCall(t, &m, c, webproto.OpTestsRun, `{"id":"c:pkg/a_test.go::TestB"}`); f["t"] != "res" {
		t.Fatalf("run: %v", f)
	}
	if len(*reqs) != 1 || len((*reqs)[0].caseKey) != 1 || (*reqs)[0].caseKey[0] != b.Key() {
		t.Fatalf("asked %+v", *reqs)
	}
	m.webAfterUpdate(nil)
	*now = now.Add(2 * time.Second)
	s, n := webTestTests(t, webTestNow(t, c))
	if n != 1 || s.State != "running" || !s.Live || s.Items[3].State != "running" || s.Items[2].State != "pending" || s.Command != "go test -json ./..." {
		t.Fatalf("after the start: %d %+v", n, s)
	}
	m.webAfterUpdate(nil)
	if _, n := webTestTests(t, webTestNow(t, c)); n != 0 {
		t.Fatalf("%d frames without a change", n)
	}
	webTestFinishRun(&m, a, b)
	m.webAfterUpdate(nil)
	s, _ = webTestTests(t, webTestNow(t, c))
	if s.State != "idle" || s.Live || s.ElapsedMs != 40 || s.Counts.Passed != 1 || s.Counts.Failed != 1 ||
		s.Items[0].State != "failed" || s.Items[2].State != "passed" || s.Items[2].Ms != 12 ||
		s.Items[3].Message != "a_test.go:5: want 2, got 3" || s.Items[3].FailPath != "pkg/a_test.go" || s.Items[3].FailLine != 5 {
		t.Fatalf("after the run: %+v", s)
	}

	var out webproto.TestOutput
	f, _ := webTestCall(t, &m, c, webproto.OpTestsOutput, `{"id":"c:pkg/a_test.go::TestB"}`)
	webTestData(t, f, &out)
	if strings.Join(out.Lines, "|") != "=== RUN   TestB|    a_test.go:5: want 2, got 3|--- FAIL: TestB (0.00s)" {
		t.Fatalf("output %+v", out)
	}
	f, _ = webTestCall(t, &m, c, webproto.OpTestsOutput, `{}`)
	webTestData(t, f, &out)
	if strings.Join(out.Lines, "|") != "ok|FAIL" {
		t.Fatalf("log %+v", out)
	}
	if a := webTestAudit(&m); !strings.Contains(a, "tests.run  c:pkg/a_test.go::TestB  by=web:phone  → ok") {
		t.Errorf("audit:\n%s", a)
	}
}

// Spec (WEBP-4): a row the tree does not have is not found and nothing
// runs (the page cannot name a path of its own); all and failed map to
// the laptop's requests; a problem (no failed test) is a conflict.
func TestTestsRunOnlyListedRows(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	webTestTestsTree(t, &m)
	reqs := webTestFakeTestRuns(t)
	c := webTestSub(t, &m)
	for _, id := range []string{"c:pkg/a_test.go::TestC", "f:pkg/b_test.go", "p:other", "c:pkg/b_test.go::TestA"} {
		if f, _ := webTestCall(t, &m, c, webproto.OpTestsRun, `{"id":"`+id+`"}`); webTestErr(f) == "" {
			t.Errorf("%s: %v", id, f)
		}
	}
	if len(*reqs) != 0 {
		t.Fatalf("started %+v", *reqs)
	}
	webTestCall(t, &m, c, webproto.OpTestsRun, `{"id":"f:pkg/a_test.go"}`)
	webTestCall(t, &m, c, webproto.OpTestsRun, `{"id":"p:pkg"}`)
	webTestCall(t, &m, c, webproto.OpTestsRun, `{}`)
	if len(*reqs) != 3 || !strings.HasSuffix((*reqs)[0].file, "a_test.go") || !strings.HasSuffix((*reqs)[1].pkgDir, "pkg") || !(*reqs)[2].all {
		t.Fatalf("asked %+v", *reqs)
	}
	webStartTests = func(m *Model, req testsRunRequest) (tea.Cmd, *testsRunProblem) {
		return nil, &testsRunProblem{msg: "No failed tests to rerun"}
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpTestsRun, `{"failed":true}`); webTestErr(f) != "conflict" || f["msg"] != "No failed tests to rerun." {
		t.Fatalf("failed: %v", f)
	}
}

// Spec (WEBP-4): tests.debug starts the laptop's debug configuration for
// one test (Go: delve in test mode with -test.run); not for a file, not
// while a session runs. tests.stop ends a run; without one it is a
// conflict.
func TestTestsDebugAndStop(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	webTestTestsTree(t, &m)
	webTestFakeTestRuns(t)
	var got []launchConfig
	old := webDebugStartCfg
	webDebugStartCfg = func(m *Model, cfg launchConfig) (tea.Cmd, error) { got = append(got, cfg); return nil, nil }
	t.Cleanup(func() { webDebugStartCfg = old })
	c := webTestSub(t, &m)
	if f, _ := webTestCall(t, &m, c, webproto.OpTestsDebug, `{"id":"f:pkg/a_test.go"}`); webTestErr(f) != "bad_request" {
		t.Fatalf("debug a file: %v", f)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpTestsDebug, `{"id":"c:pkg/a_test.go::TestA"}`); f["t"] != "res" {
		t.Fatalf("debug: %v", f)
	}
	if len(got) != 1 || got[0].Type != "delve" || !strings.Contains(strings.Join(webAnyStrings(got[0].Raw["args"]), " "), "^TestA$") {
		t.Fatalf("configs %+v", got)
	}
	m.dapSessionActive = true
	if f, _ := webTestCall(t, &m, c, webproto.OpTestsDebug, `{"id":"c:pkg/a_test.go::TestA"}`); webTestErr(f) != "conflict" {
		t.Fatalf("debug during a session: %v", f)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpTestsStop, `{}`); webTestErr(f) != "conflict" {
		t.Fatalf("stop with no run: %v", f)
	}
	webTestCall(t, &m, c, webproto.OpTestsRun, `{}`)
	if f, _ := webTestCall(t, &m, c, webproto.OpTestsStop, `{}`); f["t"] != "res" || m.testView.running {
		t.Fatalf("stop: %v running=%v", f, m.testView.running)
	}
}

func webAnyStrings(v any) []string {
	var out []string
	if l, ok := v.([]any); ok {
		for _, x := range l {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// Spec (WEBP-4): a browser that listens to tests starts the first
// discovery (as opening the view does on the laptop), once.
func TestTestsListenStartsDiscovery(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	m.testView = newTestsState()
	if cmd := m.webPublishTests(nil); cmd != nil {
		t.Fatal("discovery with nobody listening")
	}
	webTestSub(t, &m, "tests")
	if cmd := m.webPublishTests(nil); cmd == nil || !m.testView.discovering {
		t.Fatal("no discovery for a listening browser")
	}
	if cmd := m.webPublishTests(nil); cmd != nil {
		t.Fatal("a second discovery while one runs")
	}
}

// Spec (WEBP-4): the tests replay golden: a run and its results replay
// to the final snapshot.
func TestTestsReplayGolden(t *testing.T) {
	m := aiTestModel(t)
	now := webTestClock(t)
	h := webTestHub(t, &m)
	_, a, b := webTestTestsTree(t, &m)
	webTestFakeTestRuns(t)
	c := webReplayRecord(t, &m, h, "tests", func() {
		m.web.ops[webproto.OpTestsRun](&m, &webCall{ctx: h.ctx, conn: &webConn{out: newWebQueue()}, op: webproto.OpTestsRun, args: &webproto.TestRunArgs{}})
		m.webAfterUpdate(nil)
		*now = now.Add(time.Second)
		webTestFinishRun(&m, a, b)
		m.webAfterUpdate(nil)
	})
	webReplayCheck(t, h.topics["tests"], c)
	webReplayGolden(t, webReplayPath("tests", "run_results"), c)
}
