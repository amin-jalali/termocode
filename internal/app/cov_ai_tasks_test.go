package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/tasks"
	"github.com/amin-jalali/termocode/internal/tests"
)

// tasks_* and tests_* AI tools (ai_tasks_tools.go): the task list, a
// visible run in a terminal tab, its status and output, the test scopes
// and the results report.

func covSyncCall(m *Model) aiIDECaller {
	var mu sync.Mutex
	return func(ctx context.Context, fn aiIDEFunc) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		out, _, err := fn(m)
		return out, err
	}
}

func covTasksTool(t *testing.T, root string, call aiIDECaller, name string) ai.Tool {
	t.Helper()
	tl, ok := ai.FindTool(aiTasksTools(root, call), name)
	if !ok {
		t.Fatalf("no tool %s", name)
	}
	return tl
}

func covWriteTasks(t *testing.T, root, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".termocode", "tasks.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The list names a broken tasks.json and an empty workspace plainly.
func TestCovAITasksListEdges(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	m := &Model{}
	out, err := covTasksTool(t, root, covSyncCall(m), "tasks_list").Run(context.Background(), nil)
	if err != nil || !strings.HasPrefix(out, "no tasks;") {
		t.Errorf("empty: %q %v", out, err)
	}
	covWriteTasks(t, root, "{broken")
	if out := m.aiTasksList(); !strings.Contains(out, ".termocode/tasks.json is invalid") {
		t.Errorf("broken tasks.json: %q", out)
	}
	if aiTaskState(&taskRun{Running: true, Buf: 7}) != "running (run 7)" {
		t.Error("running state")
	}
	for in, want := range map[int]time.Duration{-5: 0, 500: 500 * time.Millisecond, 10_000_000: aiTasksMaxWait} {
		if got := aiWaitArg(map[string]any{"wait_ms": in}); got != want {
			t.Errorf("wait %d = %s", in, got)
		}
	}
}

// tasks_run refuses before anything starts: no editor, an unknown
// label, no task of a group; the card text names the task.
func TestCovAITasksRunRefusals(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	m := &Model{}
	if _, _, err := m.aiTasksRun("x", ""); err == nil || err.Error() != "the editor is not running" {
		t.Errorf("no editor: %v", err)
	}
	tl := covTasksTool(t, root, nil, "tasks_run")
	if tl.Validate(map[string]any{}) == nil || tl.Validate(map[string]any{"group": "deploy"}) == nil || tl.Validate(map[string]any{"group": "build"}) != nil {
		t.Error("tasks_run validate")
	}
	if tl.Describe(map[string]any{"label": "lint"}) != "Run task: lint" || tl.Describe(map[string]any{"group": "test"}) != "Run the default test task" {
		t.Error("tasks_run card text")
	}
	st := covTasksTool(t, root, nil, "tasks_status")
	if st.Describe(map[string]any{"label": "lint"}) != "Task status lint" {
		t.Error("tasks_status card text")
	}
	tr := covTasksTool(t, root, nil, "tests_run")
	for a, want := range map[string]string{"file": "Run tests in the open file", "cursor": "Run the test at the cursor", "failed": "Rerun the failed tests", "all": "Run all tests"} {
		if got := tr.Describe(map[string]any{"scope": a}); got != want {
			t.Errorf("tests_run %s card = %q", a, got)
		}
	}
	if tr.Describe(map[string]any{"scope": "file", "path": "a_test.go"}) != "Run tests in a_test.go" || tr.Describe(map[string]any{"scope": "name", "name": "TestX"}) != "Run test TestX" {
		t.Error("tests_run card with args")
	}
	if tr.Validate(map[string]any{"scope": "some"}) == nil || tr.Validate(map[string]any{"scope": "name"}) == nil || tr.Validate(map[string]any{"scope": "all"}) != nil {
		t.Error("tests_run validate")
	}
	if covTasksTool(t, root, nil, "tests_results").Describe(nil) != "Read test results" {
		t.Error("tests_results card text")
	}
}

// A task runs in a visible tab; tasks_status reads its output, waits for
// the end and reports the exit code once, with the file sync after it.
func TestCovAITasksRunAndStatusNvim(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("SHELL", "/bin/sh")
	c := covNvim(t)
	_ = c.ExecLua(`vim.o.shell = '/bin/sh'`)
	covWriteTasks(t, root, `{"version":"2.0.0","tasks":[
 {"label":"hello","type":"shell","command":"echo hello-from-task; exit 3"},
 {"label":"build it","type":"shell","command":"echo built","group":{"kind":"build","isDefault":true}},
 {"label":"check","type":"shell","command":"echo checked","group":{"kind":"test","isDefault":true}}]}`)
	m := &Model{w: 100, h: 30, nvim: c, terminalRows: 8, output: newOutputStore(), tasks: newTaskRunner(), problems: newProblemsPanel(), testView: newTestsState()}
	call := covSyncCall(m)
	if _, _, err := m.aiTasksRun("nope", ""); err == nil || !strings.Contains(err.Error(), `no task "nope"`) {
		t.Errorf("unknown label: %v", err)
	}
	status := covTasksTool(t, root, call, "tasks_status")
	if _, err := status.Run(context.Background(), map[string]any{}); err == nil || !strings.Contains(err.Error(), "no task has run yet") {
		t.Errorf("status before a run: %v", err)
	}
	if _, err := status.Run(context.Background(), map[string]any{"label": "hello"}); err == nil || !strings.Contains(err.Error(), `task "hello" has not run yet`) {
		t.Errorf("status of a task that never ran: %v", err)
	}
	out, err := covTasksTool(t, root, call, "tasks_run").Run(context.Background(), map[string]any{"label": "hello"})
	if err != nil || !strings.HasPrefix(out, `started task "hello" (run `) || !strings.Contains(out, "in terminal tab 1") {
		t.Fatalf("run: %q %v", out, err)
	}
	r := m.tasks.lastRun("hello")
	out, err = status.Run(context.Background(), map[string]any{"label": "hello", "wait_ms": 100})
	if err != nil || !strings.Contains(out, "is still running after 100ms") {
		t.Errorf("still running: %q %v", out, err)
	}
	out, err = status.Run(context.Background(), map[string]any{"label": "hello", "lines": 5000})
	if err != nil || !strings.Contains(out, "is running (") {
		t.Errorf("running: %q %v", out, err)
	}
	var lines []string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if lines = m.aiTaskOutput(r.Buf, 10); strings.Contains(strings.Join(lines, "\n"), "hello-from-task") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	m.onTaskExit(r.Buf, 3)
	out, err = status.Run(context.Background(), map[string]any{"label": "hello"})
	if err != nil || !strings.Contains(out, `task "hello" (run `) || !strings.Contains(out, "exited with code 3") ||
		!strings.Contains(out, "hello-from-task") || !strings.Contains(out, "open_file(path, line)") {
		t.Errorf("exited: %q %v", out, err)
	}
	if m.aiTaskOutput(-1, 5) != nil {
		t.Error("output of a gone buffer")
	}
	out, err = covTasksTool(t, root, call, "tasks_run").Run(context.Background(), map[string]any{"group": "build"})
	if err != nil || !strings.Contains(out, `started task "build it"`) {
		t.Errorf("default build task: %q %v", out, err)
	}
	if out, _, err := m.aiTasksRun("", tasks.GroupTest); err != nil || !strings.Contains(out, `started task "check"`) {
		t.Errorf("default test task: %q %v", out, err)
	}
	if got := m.aiTasksList(); !strings.Contains(got, `"hello" (tasks.json): echo hello-from-task; exit 3 — last run: exit 3`) || !strings.Contains(got, "[build, default]") {
		t.Errorf("list after runs:\n%s", got)
	}
}

// tests_run scopes: a file, a folder, the cursor, and the refusals.
func TestCovAITestsRequestScopes(t *testing.T) {
	root := t.TempDir()
	m := &Model{}
	if _, _, err := m.aiTestsRequest(root, "file", "", ""); err == nil || err.Error() != "no file is open; pass path" {
		t.Errorf("no open file: %v", err)
	}
	if _, _, err := m.aiTestsRequest(root, "nope", "", ""); err == nil || !strings.Contains(err.Error(), "unknown scope") {
		t.Errorf("unknown scope: %v", err)
	}
	if req, what, _ := m.aiTestsRequest(root, "failed", "", ""); !req.failed || what != "the failed tests" {
		t.Errorf("failed: %+v %q", req, what)
	}
	_ = os.MkdirAll(filepath.Join(root, "pkg"), 0o755)
	if req, what, _ := m.aiTestsRequest(root, "file", "pkg", ""); req.pkgDir != filepath.Join(root, "pkg") || what != "the tests in pkg" {
		t.Errorf("folder: %+v %q", req, what)
	}
	if req, what, _ := m.aiTestsRequest(root, "file", "pkg/a_test.go", ""); req.file != filepath.Join(root, "pkg", "a_test.go") || what != "the tests in pkg/a_test.go" {
		t.Errorf("file: %+v %q", req, what)
	}
	if got := aiRootRel(root, "/elsewhere/x.go"); got != "/elsewhere/x.go" {
		t.Errorf("outside root = %q", got)
	}
}

// tests_run refuses with the runner's reason: no Testing view, no
// runner, nothing failed to rerun, no test by that name.
func TestCovAITestsRunRefusals(t *testing.T) {
	root := t.TempDir()
	m := &Model{w: 100, h: 30}
	call := covSyncCall(m)
	run := covTasksTool(t, root, call, "tests_run")
	if _, err := run.Run(context.Background(), map[string]any{"scope": "all"}); err == nil || !strings.Contains(err.Error(), "Testing view is not available") {
		t.Errorf("no view: %v", err)
	}
	m.testView = newTestsState()
	m.testView.discovered = true
	m.testView.tree = &tests.Tree{Root: root, FW: tests.FrameworkNone}
	if _, err := run.Run(context.Background(), map[string]any{"scope": "all"}); err == nil || !strings.Contains(err.Error(), "No test runner detected") {
		t.Errorf("no runner: %v", err)
	}
	m.testView.tree = k6Tree(root, 0)
	if _, err := run.Run(context.Background(), map[string]any{"scope": "failed"}); err == nil || !strings.Contains(err.Error(), "No failed tests to rerun") {
		t.Errorf("nothing failed: %v", err)
	}
	if _, err := run.Run(context.Background(), map[string]any{"scope": "name", "name": "TestNope"}); err == nil || !strings.Contains(err.Error(), "tests_results lists the known tests") {
		t.Errorf("unknown name: %v", err)
	}
	if _, err := run.Run(context.Background(), map[string]any{"scope": "cursor"}); err == nil || err.Error() != "no file is open; pass path" {
		t.Errorf("cursor without a file: %v", err)
	}
	stopped := func(context.Context, aiIDEFunc) (string, error) { return "", errAIChatStopped }
	if _, err := aiTestsRun(context.Background(), stopped, root, "all", "", ""); err != errAIChatStopped {
		t.Errorf("stopped chat: %v", err)
	}
	if _, err := aiTestsResults(context.Background(), stopped, root, false, 0); err != errAIChatStopped {
		t.Errorf("stopped chat results: %v", err)
	}
}

// The results report: a run that did not start, package errors with
// file:line, skipped names and long name lists.
func TestCovAITestsResultsReportMore(t *testing.T) {
	root := "/w"
	st := newTestsState()
	st.runError = "go: cannot find main module"
	if got := testsResultsReport(st, root, false, false, 0); got != "The last test run did not start: go: cannot find main module" {
		t.Errorf("did not start: %q", got)
	}
	st.hasRun = true
	tree := k6Tree(root, 0)
	f := tree.Packages[0].Files[0]
	for i := 0; i < aiTestsMaxNames+2; i++ {
		f.Cases = append(f.Cases, &tests.Case{Name: "TestSkip" + string(rune('A'+i%26)), File: f.Path, Status: tests.StatusSkipped})
	}
	for i := 0; i < aiTestsMaxFailures+1; i++ {
		tree.Packages = append(tree.Packages, &tests.Package{Name: "broken", Dir: root + "/broken", Error: "undefined: x\n\n  second line", ErrFile: root + "/broken/b.go", ErrLine: 4})
	}
	tree.Packages = append(tree.Packages, &tests.Package{Name: "nofile", Dir: root + "/nofile", Error: "setup failed"})
	st.tree = tree
	got := testsResultsReport(st, root, false, false, 0)
	for _, want := range []string{"The run failed before any result: go: cannot find main module", "Package errors (build failed or the suite did not load):",
		"- broken (broken/b.go:4): undefined: x\n    second line", "… and ", "Skipped: TestSkipA", "open_file(path, line)"} {
		if !strings.Contains(got, want) {
			t.Errorf("report has no %q:\n%s", want, got)
		}
	}
	if !strings.Contains(clipMessage("a\nb\nc\nd\ne\nf\ng\nh"), "…") {
		t.Error("long messages are not cut")
	}
	st.running = true
	if got := testsResultsReport(st, root, true, false, 0); !strings.HasPrefix(got, "Tests are running (") {
		t.Errorf("running: %q", got)
	}
	if testsResultsReport(nil, root, false, false, 0) != "the Testing view is not available" {
		t.Error("no view")
	}
	// tests_results waits while a run goes on, then reports.
	m := &Model{testView: st}
	call := covSyncCall(m)
	out, err := covTasksTool(t, root, call, "tests_results").Run(context.Background(), map[string]any{"wait_ms": 60, "only_failed": true})
	if err != nil || !strings.HasPrefix(out, "Tests are still running after 60ms") {
		t.Errorf("results while running: %q %v", out, err)
	}
}

// No test task but a Go module: the Testing view's runner starts.
func TestCovAITasksTestGroupUsesRunner(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := covNvim(t)
	m := &Model{w: 100, h: 30, nvim: c, output: newOutputStore(), tasks: newTaskRunner(), problems: newProblemsPanel(), testView: newTestsState()}
	out, _, err := m.aiTasksRun("", tasks.GroupTest)
	if err != nil || !strings.HasPrefix(out, "no test task in tasks.json, so the Testing view's runner started") {
		t.Errorf("runner route: %q %v", out, err)
	}
	if _, _, err := m.aiTasksRun("", tasks.GroupBuild); err != nil && !strings.Contains(err.Error(), "no build task found") {
		t.Errorf("build group: %v", err)
	}
}
