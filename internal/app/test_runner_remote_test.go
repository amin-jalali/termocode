package app

import (
	"context"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/tests"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// rcStreamExec is rcExec with a canned Stream: out is what the container
// prints for the streamed command.
type rcStreamExec struct {
	*rcExec
	out     string
	smu     sync.Mutex
	dir     string
	streams [][]string
	stopped int
}

func (x *rcStreamExec) Stream(_ context.Context, dir string, argv ...string) (io.ReadCloser, func(), error) {
	x.smu.Lock()
	x.dir = dir
	x.streams = append(x.streams, slices.Clone(argv))
	x.smu.Unlock()
	return io.NopCloser(strings.NewReader(x.out)), func() { x.smu.Lock(); x.stopped++; x.smu.Unlock() }, nil
}

// rcStreamHost is an rcHost whose Exec streams.
type rcStreamHost struct {
	*rcHost
	x *rcStreamExec
}

func (h rcStreamHost) Exec() services.Exec { return h.x }

func rcTestsModel(t *testing.T, files fstest.MapFS, out string) (*Model, *rcHost, *rcStreamExec) {
	t.Helper()
	ws, h := newRCWorkspace(t, files)
	x := &rcStreamExec{rcExec: h.exec, out: out}
	ws.Host = rcStreamHost{rcHost: h, x: x}
	m := newTestModel(t)
	m.ws = ws
	m.testView = newTestsState()
	return &m, h, x
}

// drainTests runs cmd and every command it chains until the run is done.
func drainTests(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for i := 0; cmd != nil && i < 100; i++ {
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

var rcGoTests = fstest.MapFS{
	"go.mod":                   {Data: []byte("module example.com/app\n")},
	"pkg/a_test.go":            {Data: []byte("package pkg\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n")},
	"pkg/b.go":                 {Data: []byte("package pkg\n")},
	"node_modules/x/y_test.go": {Data: []byte("package y\n\nimport \"testing\"\n\nfunc TestNo(t *testing.T) {}\n")},
}

// RC-5 4d: discovery in a container reads the test files through the
// Host FS, never the local folder (its decoys have no tests).
func TestRemoteTestDiscoveryReadsTheHostFS(t *testing.T) {
	m, h, _ := rcTestsModel(t, rcGoTests, "")
	msg := discoverTestsCmd(m.workspace())().(testsDiscoveredMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if msg.tree.FW != tests.FrameworkGo || len(msg.tree.Packages) != 1 {
		t.Fatalf("tree = %+v", msg.tree)
	}
	p := msg.tree.Packages[0]
	if p.Name != "example.com/app/pkg" || len(p.Files) != 1 || p.Files[0].Rel != "pkg/a_test.go" || p.Files[0].Cases[0].Name != "TestA" {
		t.Fatalf("package = %+v / %+v", p, p.Files)
	}
	for _, c := range h.calls() {
		if strings.Contains(c, "node_modules/x") {
			t.Fatalf("walked into node_modules: %q", c)
		}
	}
}

// A run goes through the Host Exec in the workspace root, wrapped so the
// exit code comes back, and its go test -json lines fill the tree.
func TestRemoteTestRunStreamsThroughHostExec(t *testing.T) {
	out := `{"Action":"run","Package":"example.com/app/pkg","Test":"TestA"}
{"Action":"pass","Package":"example.com/app/pkg","Test":"TestA","Elapsed":0.01}
{"Action":"pass","Package":"example.com/app/pkg","Elapsed":0.02}
` + tests.ExitMarker + "0\n"
	m, _, x := rcTestsModel(t, rcGoTests, out)
	m.applyTestsDiscovered(discoverTestsCmd(m.workspace())().(testsDiscoveredMsg))
	drainTests(t, m, m.startTestRun(tests.Scope{Kind: tests.ScopeAll}))
	if len(x.streams) != 1 || x.dir != m.ws.Root {
		t.Fatalf("streams = %q in %q", x.streams, x.dir)
	}
	argv := x.streams[0]
	if argv[0] != "sh" || argv[1] != "-c" || !strings.Contains(argv[2], tests.ExitMarker) || strings.Join(argv[4:], " ") != "go test -json ./..." {
		t.Fatalf("argv = %q", argv)
	}
	if c := m.testView.tree.Counts(); c.Passed != 1 || c.Failed != 0 {
		t.Fatalf("counts = %+v (log %q)", c, m.testView.log)
	}
	if m.testView.running || m.testView.runError != "" {
		t.Fatalf("running=%v runError=%q", m.testView.running, m.testView.runError)
	}
	for _, ln := range m.testView.log {
		if strings.Contains(ln, tests.ExitMarker) {
			t.Fatalf("the exit marker leaked into the log: %q", ln)
		}
	}
}

// Owner rule: no report file in a container termocode did not create.
func TestRemotePytestRunWritesNoReportFile(t *testing.T) {
	files := fstest.MapFS{
		"pytest.ini":         {Data: []byte("[pytest]\n")},
		"tests/test_math.py": {Data: []byte("def test_add():\n    assert 1 + 1 == 2\n")},
	}
	out := "tests/test_math.py::test_add PASSED\n" + tests.ExitMarker + "0\n"
	m, _, x := rcTestsModel(t, files, out)
	m.applyTestsDiscovered(discoverTestsCmd(m.workspace())().(testsDiscoveredMsg))
	drainTests(t, m, m.startTestRun(tests.Scope{Kind: tests.ScopeAll}))
	joined := strings.Join(x.streams[0], " ")
	if strings.Contains(joined, "junitxml") || strings.Contains(joined, "/tmp") {
		t.Fatalf("argv names a report file: %q", joined)
	}
	if c := m.testView.tree.Counts(); c.Passed != 1 {
		t.Fatalf("counts = %+v (log %q)", c, m.testView.log)
	}
}

// A program missing in the image is named, not a bare exit code.
func TestRemoteTestRunNamesAMissingProgram(t *testing.T) {
	out := "sh: go: not found\n" + tests.ExitMarker + "127\n"
	m, _, _ := rcTestsModel(t, rcGoTests, out)
	m.applyTestsDiscovered(discoverTestsCmd(m.workspace())().(testsDiscoveredMsg))
	drainTests(t, m, m.startTestRun(tests.Scope{Kind: tests.ScopeAll}))
	if last := m.testView.log[len(m.testView.log)-1]; !strings.Contains(last, "go not found in api-1") {
		t.Fatalf("last log line = %q", last)
	}
}

// Stop ends the engine stream; quitting stops every container run.
func TestRemoteTestRunStops(t *testing.T) {
	m, _, x := rcTestsModel(t, rcGoTests, "")
	m.applyTestsDiscovered(discoverTestsCmd(m.workspace())().(testsDiscoveredMsg))
	_ = m.startTestRun(tests.Scope{Kind: tests.ScopeAll})
	m.stopTestRun()
	x.smu.Lock()
	n := x.stopped
	x.smu.Unlock()
	if n == 0 {
		t.Fatal("stop did not end the stream")
	}
	if m.testView.running || m.testView.stop != nil {
		t.Fatal("the run is still marked running")
	}
}

// A host without sh cannot run the wrapper: say so, run nothing.
func TestRemoteTestRunNeedsAShell(t *testing.T) {
	m, _, x := rcTestsModel(t, rcGoTests, "")
	m.applyTestsDiscovered(discoverTestsCmd(m.workspace())().(testsDiscoveredMsg))
	m.ws.Host = noShellHost{m.ws.Host}
	_ = m.startTestRun(tests.Scope{Kind: tests.ScopeAll})
	if len(x.streams) != 0 || !strings.Contains(m.testView.runError, "no sh") {
		t.Fatalf("streams=%q runError=%q", x.streams, m.testView.runError)
	}
}

type noShellHost struct{ workspace.Host }

func (noShellHost) Caps() workspace.Caps { return workspace.Caps{} }
