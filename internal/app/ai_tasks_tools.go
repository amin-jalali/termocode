package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/tasks"
	"github.com/amin-jalali/termocode/internal/tests"
)

// K6: typed tools for tasks and tests, so the agent does not have to
// scrape a terminal.
//
//   - tasks_list (auto): every task (tasks.json, user commands, detected)
//     with group, command and the state of its last run.
//   - tasks_run (confirm, class run): runs a task by label, or the default
//     build / test task, in its visible terminal tab — the same place
//     "Tasks: Run Task…" uses.
//   - tasks_status (auto): running / exit code and the last output lines;
//     wait_ms waits (bounded, Stop cancels) until the task ends.
//   - tests_run (confirm, class run): the Testing view's runner with scope
//     all / file / cursor / failed / name.
//   - tests_results (auto): pass / fail / skip counts and the failing tests
//     with file:line and message (capped); wait_ms waits for the run.

const (
	aiTasksMaxWait     = 120 * time.Second
	aiTasksPoll        = 150 * time.Millisecond
	aiTasksMaxLines    = 500
	aiTestsMaxFailures = 20
	aiTestsMaxNames    = 50
	aiTestsMsgLines    = 6
	aiTestsMsgWidth    = 200
	aiTestsDiscoverMax = 30 * time.Second
)

var aiTestScopes = []string{"all", "file", "cursor", "failed", "name"}

// aiPollUntil calls check every poll until it reports done, wait has
// passed (timedOut) or ctx ends (errAIChatStopped). wait 0 checks once.
func aiPollUntil(ctx context.Context, wait, poll time.Duration, check func() (bool, error)) (timedOut bool, err error) {
	if wait > aiTasksMaxWait {
		wait = aiTasksMaxWait
	}
	deadline := time.Now().Add(wait)
	for {
		done, err := check()
		if err != nil || done {
			return false, err
		}
		if !time.Now().Before(deadline) {
			return true, nil
		}
		select {
		case <-ctx.Done():
			return false, errAIChatStopped
		case <-time.After(poll):
		}
	}
}

// aiWaitArg is the wait_ms argument, clamped to [0, aiTasksMaxWait].
func aiWaitArg(a map[string]any) time.Duration {
	w := time.Duration(ai.ArgInt(a, "wait_ms")) * time.Millisecond
	if w < 0 {
		w = 0
	}
	if w > aiTasksMaxWait {
		w = aiTasksMaxWait
	}
	return w
}

// aiRootRel is p relative to root when it is inside it.
func aiRootRel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return p
}

// ── Tasks ────────────────────────────────────────────────────────────────

// aiTaskState is the state of one task run.
func aiTaskState(r *taskRun) string {
	switch {
	case r == nil:
		return "not run"
	case r.Running:
		return fmt.Sprintf("running (run %d)", r.Buf)
	}
	return fmt.Sprintf("last run: exit %d", r.Code)
}

// aiTasksList runs on Update: tasks_list.
func (m *Model) aiTasksList() string {
	list, err := m.allTasks()
	tr := m.ensureTasks()
	v := m.taskVars()
	var b strings.Builder
	if err != nil {
		fmt.Fprintf(&b, "(.termocode/tasks.json is invalid: %v)\n", err)
	}
	if len(list) == 0 {
		b.WriteString("no tasks; the user can add them in .termocode/tasks.json (palette \"Tasks: Configure Tasks\")")
		return b.String()
	}
	fmt.Fprintf(&b, "%d tasks; run one with tasks_run(label) or tasks_run(group: \"build\" | \"test\"):\n", len(list))
	for _, t := range list {
		fmt.Fprintf(&b, "- %q", t.Label)
		if t.Group != "" {
			b.WriteString(" [" + t.Group)
			if t.IsDefault {
				b.WriteString(", default")
			}
			b.WriteString("]")
		}
		fmt.Fprintf(&b, " (%s): %s — %s\n", t.Source, t.CommandLine(v), aiTaskState(tr.lastRun(t.Label)))
	}
	return strings.TrimRight(b.String(), "\n")
}

// aiTasksRun runs on Update: tasks_run.
func (m *Model) aiTasksRun(label, group string) (string, tea.Cmd, error) {
	if m.nvim == nil {
		return "", nil, errors.New("the editor is not running")
	}
	list, lerr := m.allTasks()
	bad := ""
	if lerr != nil {
		bad = fmt.Sprintf(" (.termocode/tasks.json is invalid: %v)", lerr)
	}
	var t tasks.Task
	if label != "" {
		var ok bool
		if t, ok = tasks.Find(list, label); !ok {
			return "", nil, fmt.Errorf("no task %q; tasks_list shows the labels%s", label, bad)
		}
	} else {
		if group == tasks.GroupTest {
			tt, runner, ok := testTaskRoute(list, m.testsFramework())
			if runner {
				cmd := m.runTestsCmd()
				return "no test task in tasks.json, so the Testing view's runner started (all tests). " +
					"Read the result with tests_results(wait_ms).", cmd, nil
			}
			if !ok {
				return "", nil, fmt.Errorf("no test task found%s; add one with \"group\": \"test\" in .termocode/tasks.json", bad)
			}
			t = tt
		} else {
			var ok bool
			if t, ok = tasks.DefaultFor(list, group); !ok {
				return "", nil, fmt.Errorf("no %s task found%s; add one with \"group\": \"%s\" in .termocode/tasks.json", group, bad, group)
			}
		}
	}
	tr := m.ensureTasks()
	before := tr.lastRun(t.Label)
	cmd := m.runTask(t)
	r := tr.lastRun(t.Label)
	if r == nil || r == before {
		msg := "the task did not start"
		if m.err != "" {
			msg += ": " + m.err
		}
		return "", cmd, errors.New(msg)
	}
	tab := 0
	for i, tt := range m.terminalTabs {
		if tt.BufID == r.Buf {
			tab = i + 1
		}
	}
	return fmt.Sprintf("started task %q (run %d) in terminal tab %d; the user sees it run. Command: %s\n"+
		"Use tasks_status(label: %q, wait_ms) for the exit code and the output.",
		t.Label, r.Buf, tab, t.CommandLine(m.taskVars()), t.Label), cmd, nil
}

// aiTaskSnap is one look at a task run.
type aiTaskSnap struct {
	Label   string
	Buf     int
	Running bool
	Code    int
	Took    time.Duration
	Cmd     string
}

// aiTaskLook runs on Update: the newest run of label ("" = any task).
func (m *Model) aiTaskLook(label string) (aiTaskSnap, error) {
	tr := m.ensureTasks()
	r := tr.lastRun(label)
	if r == nil {
		if label == "" {
			return aiTaskSnap{}, errors.New("no task has run yet; start one with tasks_run")
		}
		return aiTaskSnap{}, fmt.Errorf("task %q has not run yet (or its tab was closed); start it with tasks_run", label)
	}
	s := aiTaskSnap{Label: r.Task.Label, Buf: r.Buf, Running: r.Running, Code: r.Code, Cmd: r.Task.CommandLine(m.taskVars())}
	s.Took = time.Since(r.Started).Round(100 * time.Millisecond)
	return s, nil
}

// aiTaskOutput runs on Update: the last n lines of the task's terminal.
func (m *Model) aiTaskOutput(buf, n int) []string {
	if m.nvim == nil {
		return nil
	}
	var raw string
	if err := m.nvim.ExecLuaResult(aiTermSnapLua, &raw, buf, n); err != nil || raw == "" {
		return nil
	}
	var s aiTermSnap
	if json.Unmarshal([]byte(raw), &s) != nil {
		return nil
	}
	return s.Lines
}

// aiTasksStatus is tasks_status on the tool goroutine. synced remembers
// the runs whose end already refreshed the IDE in this chat turn.
func aiTasksStatus(ctx context.Context, call aiIDECaller, synced *sync.Map, label string, lines int, wait time.Duration) (string, error) {
	if lines <= 0 {
		lines = 40
	}
	if lines > aiTasksMaxLines {
		lines = aiTasksMaxLines
	}
	var snap aiTaskSnap
	var out []string
	look := func(withLines bool) error {
		_, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
			s, err := m.aiTaskLook(label)
			if err != nil {
				return "", nil, err
			}
			snap = s
			if withLines {
				out = m.aiTaskOutput(s.Buf, lines)
			}
			return "", nil, nil
		})
		return err
	}
	timedOut, err := aiPollUntil(ctx, wait, aiTasksPoll, func() (bool, error) {
		if err := look(false); err != nil {
			return false, err
		}
		return !snap.Running, nil
	})
	if err != nil {
		return "", err
	}
	if err := look(true); err != nil {
		return "", err
	}
	var head string
	switch {
	case !snap.Running:
		head = fmt.Sprintf("task %q (run %d) exited with code %d", snap.Label, snap.Buf, snap.Code)
	case timedOut && wait > 0:
		head = fmt.Sprintf("task %q (run %d) is still running after %s; call tasks_status again with wait_ms", snap.Label, snap.Buf, wait)
	default:
		head = fmt.Sprintf("task %q (run %d) is running (%s so far); wait_ms waits for the end", snap.Label, snap.Buf, snap.Took)
	}
	head += "\ncommand: " + snap.Cmd
	res := head + "\nlast output:\n" + aiTermTail(out, lines)
	if !snap.Running {
		if snap.Code != 0 {
			res += "\nUse open_file(path, line) to jump to a file:line from the output; the task's problems are in diagnostics when it has a problem matcher."
		}
		if _, done := synced.LoadOrStore(snap.Buf, true); !done {
			res += aiSyncAfter(ctx, call, "", aiSyncDisk) // the task may have changed files
		}
	}
	return res, nil
}

// ── Tests ────────────────────────────────────────────────────────────────

// aiTestsBusy says whether a test run is going or queued.
func (st *testsState) aiTestsBusy() bool {
	return st != nil && (st.running || st.pending != nil)
}

// aiTestsRequest runs on Update: the request for a tests_run scope.
func (m *Model) aiTestsRequest(root, scope, path, name string) (testsRunRequest, string, error) {
	abs := func(p string) string {
		if p != "" && !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		return filepath.Clean(p)
	}
	switch scope {
	case "all":
		return testsRunRequest{all: true}, "all tests", nil
	case "failed":
		return testsRunRequest{failed: true}, "the failed tests", nil
	case "name":
		return testsRunRequest{name: name}, "tests named " + name, nil
	case "file", "cursor":
		p := path
		if p == "" || scope == "cursor" {
			p = m.editor.Path()
		}
		if p == "" {
			return testsRunRequest{}, "", errors.New("no file is open; pass path")
		}
		p = abs(p)
		if scope == "cursor" {
			return testsRunRequest{file: p, atLine: m.cursorLine},
				fmt.Sprintf("the test at %s:%d", aiRootRel(root, p), m.cursorLine), nil
		}
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return testsRunRequest{pkgDir: p}, "the tests in " + aiRootRel(root, p), nil
		}
		return testsRunRequest{file: p}, "the tests in " + aiRootRel(root, p), nil
	}
	return testsRunRequest{}, "", fmt.Errorf("unknown scope %q; use one of %s", scope, strings.Join(aiTestScopes, ", "))
}

// aiTestsRun is tests_run on the tool goroutine: discover first (so a
// bad scope is an error, not a toast), then start the run.
func aiTestsRun(ctx context.Context, call aiIDECaller, root, scope, path, name string) (string, error) {
	_, err := aiPollUntil(ctx, aiTestsDiscoverMax, aiTasksPoll, func() (bool, error) {
		out, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
			st := m.testView
			if st == nil {
				return "", nil, errors.New("the Testing view is not available")
			}
			if st.discovered && !st.discovering {
				return "ready", nil, nil
			}
			return "", m.refreshTests(), nil
		})
		return out == "ready", err
	})
	if err != nil {
		return "", err
	}
	return call(ctx, func(m *Model) (string, tea.Cmd, error) {
		req, what, err := m.aiTestsRequest(root, scope, path, name)
		if err != nil {
			return "", nil, err
		}
		st := m.testView
		m.revealTestsView(false)
		cmd, p := m.tryTestsRequest(req)
		if p != nil {
			msg := p.msg
			if req.name != "" || req.file != "" {
				msg += "; tests_results lists the known tests after a run, or use scope \"all\""
			}
			return "", cmd, errors.New(msg)
		}
		if !st.aiTestsBusy() {
			msg := st.runError
			if msg == "" {
				msg = st.err
			}
			if msg == "" {
				msg = "the test run did not start"
			}
			return "", cmd, errors.New(msg)
		}
		return fmt.Sprintf("started %s: %s (the Testing view shows progress). "+
			"Call tests_results(wait_ms: 60000) to wait for the result.", what, st.cmd.Label()), cmd, nil
	})
}

// aiTestsResults is tests_results on the tool goroutine.
func aiTestsResults(ctx context.Context, call aiIDECaller, root string, onlyFailed bool, wait time.Duration) (string, error) {
	timedOut, err := aiPollUntil(ctx, wait, aiTasksPoll, func() (bool, error) {
		out, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
			if m.testView.aiTestsBusy() {
				return "", nil, nil
			}
			return "done", nil, nil
		})
		return out == "done", err
	})
	if err != nil {
		return "", err
	}
	return call(ctx, func(m *Model) (string, tea.Cmd, error) {
		return testsResultsReport(m.testView, root, onlyFailed, timedOut && wait > 0, wait), nil, nil
	})
}

// clipMessage is the first lines of a failure message, each cut short.
func clipMessage(s string) string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if len(out) == aiTestsMsgLines {
			out = append(out, "…")
			break
		}
		if r := []rune(l); len(r) > aiTestsMsgWidth {
			l = string(r[:aiTestsMsgWidth]) + "…"
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n    ")
}

// testsResultsReport is the tests_results text: counts, failing tests
// with file:line and message, package errors, then (unless onlyFailed)
// the passed and skipped names. Everything is capped.
func testsResultsReport(st *testsState, root string, onlyFailed, stillBusy bool, wait time.Duration) string {
	if st == nil {
		return "the Testing view is not available"
	}
	if !st.hasRun && !st.aiTestsBusy() {
		msg := "No test run yet. Start one with tests_run(scope: \"all\")."
		if st.runError != "" {
			msg = "The last test run did not start: " + st.runError
		}
		return msg
	}
	var b strings.Builder
	c := st.tree.Counts()
	label := st.cmd.Label()
	switch {
	case st.aiTestsBusy():
		if stillBusy {
			fmt.Fprintf(&b, "Tests are still running after %s (%s); call tests_results again with wait_ms. So far: ", wait, label)
		} else {
			fmt.Fprintf(&b, "Tests are running (%s); tests_results(wait_ms: 60000) waits for the end. So far: ", label)
		}
	default:
		fmt.Fprintf(&b, "Test run %s finished in %s: ", label, st.elapsed)
	}
	fmt.Fprintf(&b, "%d passed, %d failed, %d skipped.\n", c.Passed, c.Failed, c.Skipped)
	if st.runError != "" {
		b.WriteString("The run failed before any result: " + clipMessage(st.runError) + "\n")
	}

	var failed, passed, skipped []*tests.Case
	st.tree.Each(func(_ *tests.Package, _ *tests.File, tc *tests.Case) {
		switch tc.Status {
		case tests.StatusFailed:
			failed = append(failed, tc)
		case tests.StatusPassed:
			passed = append(passed, tc)
		case tests.StatusSkipped:
			skipped = append(skipped, tc)
		}
	})
	var pkgErrs []string
	if st.tree != nil {
		for _, p := range st.tree.Packages {
			if p.Error == "" {
				continue
			}
			loc := aiRootRel(root, p.Dir)
			if p.ErrFile != "" && p.ErrLine > 0 {
				loc = fmt.Sprintf("%s:%d", aiRootRel(root, p.ErrFile), p.ErrLine)
			}
			pkgErrs = append(pkgErrs, fmt.Sprintf("- %s (%s): %s", p.Name, loc, clipMessage(p.Error)))
		}
	}
	if len(failed) > 0 {
		fmt.Fprintf(&b, "%d failed:\n", len(failed))
		for i, tc := range failed {
			if i == aiTestsMaxFailures {
				fmt.Fprintf(&b, "… and %d more failed tests\n", len(failed)-i)
				break
			}
			f, l := tc.Location()
			fmt.Fprintf(&b, "- %s (%s:%d)", tc.Name, aiRootRel(root, f), l)
			if msg := clipMessage(tc.Message); msg != "" {
				b.WriteString(": " + msg)
			}
			b.WriteString("\n")
		}
	}
	if len(pkgErrs) > 0 {
		b.WriteString("Package errors (build failed or the suite did not load):\n")
		if len(pkgErrs) > aiTestsMaxFailures {
			pkgErrs = append(pkgErrs[:aiTestsMaxFailures], fmt.Sprintf("… and %d more", len(pkgErrs)-aiTestsMaxFailures))
		}
		b.WriteString(strings.Join(pkgErrs, "\n") + "\n")
	}
	if len(failed) > 0 || len(pkgErrs) > 0 {
		b.WriteString("Use open_file(path, line) to jump to a failure; after a fix, tests_run(scope: \"failed\") reruns only the failed tests.\n")
	}
	if !onlyFailed {
		names := func(title string, cs []*tests.Case) {
			if len(cs) == 0 {
				return
			}
			var ns []string
			for i, tc := range cs {
				if i == aiTestsMaxNames {
					ns = append(ns, fmt.Sprintf("… and %d more", len(cs)-i))
					break
				}
				ns = append(ns, tc.Name)
			}
			fmt.Fprintf(&b, "%s: %s\n", title, strings.Join(ns, ", "))
		}
		names("Passed", passed)
		names("Skipped", skipped)
	}
	return strings.TrimRight(b.String(), "\n")
}

// ── Tool definitions ─────────────────────────────────────────────────────

// aiTasksTools are tasks_list, tasks_run, tasks_status, tests_run and
// tests_results.
func aiTasksTools(root string, call aiIDECaller) []ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	num := func(d string) map[string]any { return map[string]any{"type": "integer", "description": d} }
	boolean := func(d string) map[string]any { return map[string]any{"type": "boolean", "description": d} }
	waitParam := num("wait up to this long (ms, max 120000) for the run to end; default 0 (answer at once)")
	synced := &sync.Map{}
	return []ai.Tool{
		{
			Name: "tasks_list",
			Description: "List the workspace tasks (.termocode/tasks.json, user commands, auto-detected build/test tasks): " +
				"label, group (build/test, default), command and the state of the last run.",
			Params:   map[string]any{},
			Describe: func(map[string]any) string { return "List tasks" },
			Run: func(ctx context.Context, _ map[string]any) (string, error) {
				return call(ctx, func(m *Model) (string, tea.Cmd, error) { return m.aiTasksList(), nil, nil })
			},
		},
		{
			Name: "tasks_run",
			Description: "Run a task in its own VISIBLE terminal tab (like Tasks: Run Task). Pass label (see tasks_list), " +
				"or group \"build\" / \"test\" for the default task of that group. Returns the run id; read the exit code " +
				"and output with tasks_status(label, wait_ms).",
			Params: map[string]any{
				"label": str("task label from tasks_list"),
				"group": map[string]any{"type": "string", "enum": []string{tasks.GroupBuild, tasks.GroupTest},
					"description": "run the default task of this group instead of a label"},
			},
			Mutating: true, Class: ai.ClassRun,
			Validate: func(a map[string]any) error {
				label, group := ai.ArgString(a, "label"), ai.ArgString(a, "group")
				if label == "" && group == "" {
					return errors.New("pass label (see tasks_list) or group (build / test)")
				}
				if label == "" && group != tasks.GroupBuild && group != tasks.GroupTest {
					return fmt.Errorf("unknown group %q; use build or test", group)
				}
				return nil
			},
			Describe: func(a map[string]any) string {
				if l := ai.ArgString(a, "label"); l != "" {
					return "Run task: " + l
				}
				return "Run the default " + ai.ArgString(a, "group") + " task"
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				label, group := ai.ArgString(a, "label"), ai.ArgString(a, "group")
				return call(ctx, func(m *Model) (string, tea.Cmd, error) { return m.aiTasksRun(label, group) })
			},
		},
		{
			Name: "tasks_status",
			Description: "State of a task run: running or the exit code, and the last output lines. label defaults to " +
				"the newest run. wait_ms waits until the task ends (bounded).",
			Params: map[string]any{
				"label":   str("task label; default the newest run"),
				"wait_ms": waitParam,
				"lines":   num("how many last output lines (default 40, max 500)"),
			},
			Describe: func(a map[string]any) string { return "Task status " + ai.ArgString(a, "label") },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				return aiTasksStatus(ctx, call, synced, ai.ArgString(a, "label"), ai.ArgInt(a, "lines"), aiWaitArg(a))
			},
		},
		{
			Name: "tests_run",
			Description: "Run tests with the IDE's test runner (go test, pytest, cargo, jest, vitest); the Testing view shows " +
				"progress. scope: all, file (path or the open file; a folder runs its tests), cursor (the test at the " +
				"cursor), failed (rerun the failed ones), name (tests with this name). Then call tests_results(wait_ms).",
			Params: map[string]any{
				"scope": map[string]any{"type": "string", "enum": aiTestScopes, "description": "which tests"},
				"path":  str("file or folder for scope file (default the open file)"),
				"name":  str("test name for scope name, e.g. TestParse"),
			},
			Required: []string{"scope"},
			Mutating: true, Class: ai.ClassRun,
			Validate: func(a map[string]any) error {
				scope := ai.ArgString(a, "scope")
				if !contains(aiTestScopes, scope) {
					return fmt.Errorf("unknown scope %q; use one of %s", scope, strings.Join(aiTestScopes, ", "))
				}
				if scope == "name" && strings.TrimSpace(ai.ArgString(a, "name")) == "" {
					return errors.New("scope name needs name")
				}
				return nil
			},
			Describe: func(a map[string]any) string {
				switch ai.ArgString(a, "scope") {
				case "file":
					p := ai.ArgString(a, "path")
					if p == "" {
						p = "the open file"
					}
					return "Run tests in " + p
				case "cursor":
					return "Run the test at the cursor"
				case "failed":
					return "Rerun the failed tests"
				case "name":
					return "Run test " + ai.ArgString(a, "name")
				}
				return "Run all tests"
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				return aiTestsRun(ctx, call, root, ai.ArgString(a, "scope"), ai.ArgString(a, "path"),
					strings.TrimSpace(ai.ArgString(a, "name")))
			},
		},
		{
			Name: "tests_results",
			Description: "Results of the last test run: pass / fail / skip counts, each failing test with file:line and " +
				"its message, package (build) errors. wait_ms waits until a running test run ends (bounded). " +
				"only_failed leaves out the passed and skipped names.",
			Params: map[string]any{
				"only_failed": boolean("list only failures"),
				"wait_ms":     waitParam,
			},
			Describe: func(map[string]any) string { return "Read test results" },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				return aiTestsResults(ctx, call, root, ai.ArgBool(a, "only_failed"), aiWaitArg(a))
			},
		},
	}
}
