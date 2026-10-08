package app

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"termocode/internal/activity"
	"termocode/internal/statusbar"
	"termocode/internal/tests"
	"termocode/internal/toast"
)

// testsModel is a Model with the Testing view showing a small Go tree.
func testsModel() Model {
	m := Model{
		w:             120,
		h:             30,
		showExp:       true,
		explorerWidth: defaultExplorerWidth,
		testView:      newTestsState(),
		toast:         toast.New(),
		focus:         FocusExplorer,
	}
	m.activity.SetActive(activity.ViewTests)
	m.testView.discovered = true
	m.testView.tree = &tests.Tree{Root: "/w", FW: tests.FrameworkGo, Packages: []*tests.Package{
		{Name: "example.com/demo/calc", Dir: "/w/calc", Files: []*tests.File{{Path: "/w/calc/calc_test.go", Rel: "calc/calc_test.go", Cases: []*tests.Case{
			{Name: "TestAdd", File: "/w/calc/calc_test.go", Line: 7},
			{Name: "TestAddFails", File: "/w/calc/calc_test.go", Line: 13},
			{Name: "TestTable", File: "/w/calc/calc_test.go", Line: 20},
			{Name: "TestSkipped", File: "/w/calc/calc_test.go", Line: 25},
		}}}},
		{Name: "example.com/demo/broken", Dir: "/w/broken", Files: []*tests.File{{Path: "/w/broken/broken_test.go", Cases: []*tests.Case{
			{Name: "TestX", File: "/w/broken/broken_test.go", Line: 5},
		}}}},
	}}
	return m
}

// replayGoRun feeds the recorded go test -json fixture through the model
// exactly like a live run would (no process is started).
func replayGoRun(t *testing.T, m *Model) {
	t.Helper()
	data, err := os.ReadFile("../tests/testdata/go_test.json")
	if err != nil {
		t.Fatal(err)
	}
	st := m.testView
	st.runID++
	st.running = true
	st.started = time.Now()
	st.parser = tests.NewLineParser(tests.FrameworkGo)
	st.cmd = tests.Command{Args: []string{"go", "test", "-json", "./..."}}
	st.tree.MarkRunning(nil)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if cmd := m.applyTestLines(testLinesMsg{runID: st.runID, lines: lines}); cmd != nil {
		t.Fatal("nil next expected for a synthetic batch")
	}
	m.finishTestRun(testDoneMsg{runID: st.runID})
}

func TestTestsRunUpdatesTreeAndStatus(t *testing.T) {
	m := testsModel()
	replayGoRun(t, &m)
	st := m.testView
	if st.running || !st.hasRun {
		t.Fatalf("running=%v hasRun=%v", st.running, st.hasRun)
	}
	c := st.tree.Counts()
	if c.Passed != 1 || c.Failed != 2 || c.Skipped != 1 {
		t.Fatalf("counts = %+v", c)
	}
	var s statusbar.State
	m.fillTestsStatus(&s)
	if !s.ShowTests || s.TestsPassed != 1 || s.TestsFailed != 2 {
		t.Fatalf("status chip = %+v", s)
	}
	// The run log holds decoded output, not raw JSON.
	joined := strings.Join(st.log, "\n")
	if !strings.Contains(joined, "Add(2, 2) = 4, want 5") || strings.Contains(joined, `"Action"`) {
		t.Fatalf("log = %q", joined)
	}
}

// TS-03 / TS-08: tree rows carry glyphs, counts and inline failure lines.
func TestTestsSidebarRender(t *testing.T) {
	m := testsModel()
	replayGoRun(t, &m)
	out := ansi.Strip(m.renderTestsSidebar(40, 30))
	for _, want := range []string{"T E S T I N G", "▶ Run All", "↻ Failed", "✓ TestAdd", "✗ TestAddFails", "┃ calc_test.go:16: Add(2, 2)", "⊘ TestSkipped", "⚠ broken/broken_test.go:5:28"} {
		if !strings.Contains(out, want) {
			t.Errorf("sidebar missing %q:\n%s", want, out)
		}
	}
	for i, ln := range strings.Split(m.renderTestsSidebar(40, 30), "\n") {
		if w := lipgloss.Width(ln); w != 40 {
			t.Fatalf("row %d width %d, want 40: %q", i, w, ansi.Strip(ln))
		}
	}
}

func TestTestsRowsCollapse(t *testing.T) {
	m := testsModel()
	st := m.testView
	all := len(st.rows())
	m2, _ := m.handleTestsSidebarKey(tea.KeyMsg{Type: tea.KeyLeft}) // cursor 0 = first package
	m = m2.(Model)
	if got := len(st.rows()); got >= all {
		t.Fatalf("rows after collapse = %d, want < %d", got, all)
	}
	m2, _ = m.handleTestsSidebarKey(tea.KeyMsg{Type: tea.KeyRight})
	m = m2.(Model)
	if got := len(st.rows()); got != all {
		t.Fatalf("rows after expand = %d, want %d", got, all)
	}
	m.testsCollapseAll()
	if got := len(st.rows()); got != 2 {
		t.Fatalf("collapse all → %d rows, want 2 packages", got)
	}
}

func TestTestsActionLayoutHitTest(t *testing.T) {
	acts := testsActionLayout(false, true, 40)
	var ids []string
	for _, a := range acts {
		ids = append(ids, a.id)
	}
	if strings.Join(ids, ",") != "run,failed,refresh,collapse" {
		t.Fatalf("ids = %v", ids)
	}
	if acts[0].start != 1 || acts[1].start != acts[0].end+2 {
		t.Fatalf("layout = %+v", acts)
	}
	if acts := testsActionLayout(true, false, 12); len(acts) != 3 || acts[0].id != "stop" || acts[2].end != 11 {
		t.Fatalf("narrow running layout = %+v", acts)
	}
}

func TestTestsResultsTabNarrowed(t *testing.T) {
	m := testsModel()
	replayGoRun(t, &m)
	st := m.testView
	st.resultsFor = "/w/calc/calc_test.go::TestAddFails"
	lines, label := st.testResultsLines()
	if label != "TestAddFails" || !strings.Contains(strings.Join(lines, "\n"), "want 5") {
		t.Fatalf("narrowed = %q %q", label, lines)
	}
	rows := renderTestResultsTab(&m, 100, 10)
	if !strings.Contains(ansi.Strip(rows[0]), "✗ 2 failed") || !strings.Contains(ansi.Strip(rows[0]), "✕ TestAddFails") {
		t.Fatalf("header = %q", ansi.Strip(rows[0]))
	}
}

func TestTestCaseAtLine(t *testing.T) {
	f := &tests.File{Cases: []*tests.Case{{Name: "A", Line: 3}, {Name: "B", Line: 10}}}
	if c := testCaseAtLine(f, 12); c == nil || c.Name != "B" {
		t.Fatalf("line 12 → %+v", c)
	}
	if c := testCaseAtLine(f, 5); c == nil || c.Name != "A" {
		t.Fatalf("line 5 → %+v", c)
	}
	if c := testCaseAtLine(f, 1); c != nil {
		t.Fatalf("line 1 → %+v, want nil", c)
	}
}

func TestStaleTestLinesStillDrain(t *testing.T) {
	m := testsModel()
	called := false
	next := func() tea.Msg { called = true; return nil }
	cmd := m.applyTestLines(testLinesMsg{runID: 99, lines: []string{"x"}, next: next})
	if cmd == nil {
		t.Fatal("stale batch must re-arm the reader so the pipe drains")
	}
	cmd()
	if !called || len(m.testView.log) != 0 {
		t.Fatalf("called=%v log=%v", called, m.testView.log)
	}
}

func TestStatusBarTestsChipSpan(t *testing.T) {
	var sb statusbar.Model
	sb.SetWidth(120)
	s := statusbar.State{Project: "demo", Line: 1, Col: 1, ShowTests: true, TestsPassed: 12, TestsFailed: 1}
	x0, x1, ok := sb.TestsChipSpan(s)
	if !ok || x1-x0 != lipgloss.Width("✓ 12 ✗ 1") {
		t.Fatalf("span = %d..%d ok=%v", x0, x1, ok)
	}
	row := ansi.Strip(sb.View(s))
	if got := ansi.Cut(sb.View(s), x0, x1); ansi.Strip(got) != "✓ 12 ✗ 1" {
		t.Fatalf("span covers %q in %q", ansi.Strip(got), row)
	}
}
