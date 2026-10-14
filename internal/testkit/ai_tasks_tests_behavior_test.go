package testkit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// K6: the AI runs tasks (.termocode/tasks.json) and tests (the Testing
// view's runner) with typed tools: tasks_list / tasks_run / tasks_status
// and tests_run / tests_results.

// k6TasksJSON has a quick task that prints a marker and a build task.
const k6TasksJSON = `{
  "version": 1,
  "tasks": [
    { "label": "say-hi", "command": "echo k6-task-out" },
    { "label": "k6-build", "command": "echo k6-build-out", "group": { "kind": "build", "isDefault": true } }
  ]
}`

// k6GoFiles is a Go module with one passing and one failing test. The
// failure is at calc_test.go:10.
var k6GoFiles = map[string]string{
	"go.mod":  "module k6\n\ngo 1.21\n",
	"calc.go": "package k6\n\nfunc Add(a, b int) int { return a + b }\n",
	"calc_test.go": `package k6

import "testing"

func TestGood(t *testing.T) {
	if Add(2, 2) != 4 { t.Fatal("bad") }
}

func TestBad(t *testing.T) {
	t.Errorf("want 5, got %d", Add(2, 2))
}
`,
}

// realGoCache is the user's go build cache, read before the sandbox
// moves HOME (a fresh cache makes the first go test slow).
var realGoCache = func() string {
	if v := os.Getenv("GOCACHE"); v != "" {
		return v
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "go-build")
	}
	return ""
}()

// writeFiles writes files into the workspace.
func writeFiles(d *Driver, files map[string]string) {
	d.T.Helper()
	for rel, body := range files {
		p := filepath.Join(d.Dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			d.T.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			d.T.Fatal(err)
		}
	}
}

// goWorkspace makes the workspace a Go module with k6GoFiles; it skips
// the test when go is not on PATH.
func goWorkspace(d *Driver) {
	d.T.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		d.T.Skip("go is not on PATH: the Go test runner can not run")
	}
	if realGoCache != "" {
		d.T.Setenv("GOCACHE", realGoCache)
	}
	d.T.Setenv("GOTOOLCHAIN", "local")
	d.T.Setenv("GOFLAGS", "")
	writeFiles(d, k6GoFiles)
}

// K6 rows of TestAIToolsTable.
var aiTasksToolCases = []aiToolCase{
	{
		name:  "tasks_list shows tasks.json tasks with group and command",
		call:  toolCall{"tasks_list", map[string]any{}},
		setup: func(d *Driver) { writeFiles(d, map[string]string{".termocode/tasks.json": k6TasksJSON}) },
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 {
				t.Fatalf("results %q", r)
			}
			for _, want := range []string{`"say-hi"`, "echo k6-task-out", `"k6-build"`, "build, default", "not run", "tasks_run"} {
				if !strings.Contains(r[0], want) {
					t.Errorf("tasks_list has no %q: %q", want, r[0])
				}
			}
		},
	},
	{
		name:    "tasks_run asks, then runs the task in a visible terminal tab",
		call:    toolCall{"tasks_run", map[string]any{"label": "say-hi"}},
		setup:   func(d *Driver) { writeFiles(d, map[string]string{".termocode/tasks.json": k6TasksJSON}) },
		confirm: true,
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], `started task "say-hi"`) || !strings.Contains(r[0], "tasks_status") {
				t.Errorf("results %q", r)
			}
			if !eventually(d, func() bool { return strings.Contains(screen(d), "k6-task-out") }) {
				t.Errorf("the task output is not in the terminal panel:\n%s", screen(d))
			}
		},
	},
	{
		name: "tasks_run of an unknown label is a clear error",
		call: toolCall{"tasks_run", map[string]any{"label": "nope"}},
		// The tool asks before it runs; the error comes after Allow.
		confirm: true,
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], `no task "nope"`) || !strings.Contains(r[0], "tasks_list") {
				t.Errorf("results %q", r)
			}
		},
	},
	{
		name: "tasks_status with no run",
		call: toolCall{"tasks_status", map[string]any{}},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "no task has run") {
				t.Errorf("results %q", r)
			}
		},
	},
	{
		name: "tests_results with no run",
		call: toolCall{"tests_results", map[string]any{}},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "No test run yet") || !strings.Contains(r[0], "tests_run") {
				t.Errorf("results %q", r)
			}
		},
	},
	{
		name:    "tests_run all asks, then runs the Testing view's runner",
		call:    toolCall{"tests_run", map[string]any{"scope": "all"}},
		setup:   goWorkspace,
		confirm: true,
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "started") || !strings.Contains(r[0], "tests_results") {
				t.Errorf("results %q", r)
			}
			if !d.WaitForText("1 failed, 1 passed", 60*time.Second) {
				t.Errorf("the run did not finish with 1 failed, 1 passed:\n%s", screen(d))
			}
		},
	},
}

func init() { aiToolCases = append(aiToolCases, aiTasksToolCases...) }

// Spec: Given a Go workspace with one passing and one failing test, when
// the model calls tests_run(all) (the user allows it) and then
// tests_results(wait_ms), then the result has 1 passed and 1 failed, the
// failing test with calc_test.go:10 and its message, and tells the model
// to jump with open_file. only_failed leaves the passed names out.
func TestAITestsRunThenResults(t *testing.T) {
	s, srv := fakeToolModel(t,
		toolCall{"tests_run", map[string]any{"scope": "all"}},
		toolCall{"tests_results", map[string]any{"wait_ms": 60000}},
		toolCall{"tests_results", map[string]any{"only_failed": true}},
	)
	d := aiDriver(t, srv.URL, nil, false, "run")
	goWorkspace(d)
	open(d, "a.txt")
	askAI(d, "run the tests")
	if !d.WaitForText("fake-done", 90*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
	r := s.toolResults()
	if len(r) != 3 {
		t.Fatalf("results %q", r)
	}
	for _, want := range []string{"1 passed", "1 failed", "0 skipped", "TestBad", "calc_test.go:10", "want 5, got 4", "open_file", "TestGood"} {
		if !strings.Contains(r[1], want) {
			t.Errorf("tests_results has no %q:\n%s", want, r[1])
		}
	}
	if strings.Contains(r[2], "TestGood") || !strings.Contains(r[2], "TestBad") {
		t.Errorf("only_failed:\n%s", r[2])
	}
	if !strings.Contains(screen(d), "TESTING") {
		t.Errorf("the Testing view is not shown:\n%s", screen(d))
	}
}

// Spec: tests_run(scope: "name") runs only the named test.
func TestAITestsRunByName(t *testing.T) {
	s, srv := fakeToolModel(t,
		toolCall{"tests_run", map[string]any{"scope": "name", "name": "TestGood"}},
		toolCall{"tests_results", map[string]any{"wait_ms": 60000}},
	)
	d := aiDriver(t, srv.URL, nil, false, "run")
	goWorkspace(d)
	open(d, "a.txt")
	askAI(d, "run one test")
	if !d.WaitForText("fake-done", 90*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
	r := s.toolResults()
	if len(r) != 2 || !strings.Contains(r[1], "1 passed, 0 failed") {
		t.Errorf("results %q", r)
	}
}

// Spec: tasks_run then tasks_status(wait_ms) returns the exit code 0 and
// the task's output.
func TestAITasksRunThenStatus(t *testing.T) {
	s, srv := fakeToolModel(t,
		toolCall{"tasks_run", map[string]any{"group": "build"}},
		toolCall{"tasks_status", map[string]any{"label": "k6-build", "wait_ms": 10000}},
		toolCall{"tasks_list", map[string]any{}},
	)
	d := aiDriver(t, srv.URL, nil, false)
	writeFiles(d, map[string]string{".termocode/tasks.json": k6TasksJSON})
	open(d, "a.txt")
	askAI(d, "build it")
	if !d.WaitForText("AI wants to", 5*time.Second) {
		t.Fatalf("no confirm dialog:\n%s", screen(d))
	}
	if !strings.Contains(screen(d), "Run the default build task") {
		t.Errorf("the dialog does not say what runs:\n%s", screen(d))
	}
	clickAIAllow(d)
	if !d.WaitForText("fake-done", 15*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
	r := s.toolResults()
	if len(r) != 3 {
		t.Fatalf("results %q", r)
	}
	if !strings.Contains(r[0], `started task "k6-build"`) {
		t.Errorf("tasks_run: %q", r[0])
	}
	if !strings.Contains(r[1], "exited with code 0") || !strings.Contains(r[1], "\nk6-build-out") {
		t.Errorf("tasks_status: %q", r[1])
	}
	if !strings.Contains(r[2], "exit 0") {
		t.Errorf("tasks_list does not show the last exit: %q", r[2])
	}
}

// Spec: a failing task reports its exit code.
func TestAITaskStatusFailingExit(t *testing.T) {
	s, srv := fakeToolModel(t,
		toolCall{"tasks_run", map[string]any{"label": "boom"}},
		toolCall{"tasks_status", map[string]any{"wait_ms": 10000}},
	)
	d := aiDriver(t, srv.URL, nil, false, "run")
	writeFiles(d, map[string]string{".termocode/tasks.json": `{"version":1,"tasks":[{"label":"boom","command":"echo k6-boom; exit 3"}]}`})
	open(d, "a.txt")
	askAI(d, "run boom")
	if !d.WaitForText("fake-done", 15*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
	r := s.toolResults()
	if len(r) != 2 || !strings.Contains(r[1], "exited with code 3") || !strings.Contains(r[1], "k6-boom") {
		t.Errorf("results %q", r)
	}
}
