package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/tests"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Test runner (Group E) ────────────────────────────────────────────────
//
// One runner for every entry point: the Testing sidebar (test_explorer.go),
// Alt+T / "Run: Tests", and the Ctrl+. "Run Test" code actions. The
// command comes from tests.BuildCommand; stdout+stderr stream through a
// scanner goroutine into testLinesMsg batches, each line is parsed by the
// framework's tests.LineParser and folded into the tree, and the raw lines
// land in the run log shown by the TEST RESULTS bottom tab.

// testLogMax caps the run log (oldest lines dropped).
const testLogMax = 20000

// testsState is the Testing view + runner state. Pointer on Model so copies
// of the value-type Model share it.
type testsState struct {
	tree        *tests.Tree
	discovered  bool
	discovering bool
	// err is a discovery / launch problem shown in place of the tree.
	err string

	collapsed map[string]bool // row key → collapsed
	cursor    int

	running  bool
	runID    int
	proc     *exec.Cmd
	parser   tests.LineParser
	cmd      tests.Command
	started  time.Time
	elapsed  time.Duration
	hasRun   bool   // a run finished → status-bar ✓/✗ counts show
	runError string // whole-run failure (exit error with no results)
	results  int    // result events seen in the current run

	log []string
	// resultsFor narrows the TEST RESULTS tab to one case (Case.Key); ""
	// shows the whole run log.
	resultsFor    string
	resultsScroll int // rows above the tail (0 follows new output)

	// pending is a run requested before discovery finished.
	pending *testsRunRequest
}

// testsRunRequest is a run waiting for (re)discovery. Exactly one of the
// selectors is used: all, a package dir, a file path, or case keys.
type testsRunRequest struct {
	all     bool
	pkgDir  string
	file    string
	caseKey []string
	failed  bool
	// atLine with file runs the test declared nearest above that line
	// (the whole file when none).
	atLine int
	// goFunc / goPkgDir select a Go test by name when discovery has not
	// seen it (Ctrl+. on an unsaved test).
	goFunc string
}

func newTestsState() *testsState {
	return &testsState{collapsed: map[string]bool{}}
}

// testsDiscoveredMsg carries a finished discovery walk.
type testsDiscoveredMsg struct {
	tree *tests.Tree
	err  error
}

// testLinesMsg is a batch of output lines from run runID.
type testLinesMsg struct {
	runID int
	lines []string
	next  tea.Cmd // reads the next batch; always re-armed so the pipe drains
}

// testDoneMsg ends run runID.
type testDoneMsg struct {
	runID  int
	err    error
	report []byte
}

// discoverTestsCmd walks the workspace off the UI goroutine.
func discoverTestsCmd() tea.Cmd {
	return func() tea.Msg {
		root, err := os.Getwd()
		if err != nil {
			return testsDiscoveredMsg{err: err}
		}
		fw := tests.Detect(root)
		if fw == tests.FrameworkNone {
			return testsDiscoveredMsg{tree: &tests.Tree{Root: root}, err: tests.ErrNoFramework}
		}
		pkgs, err := tests.Discover(root, fw)
		return testsDiscoveredMsg{tree: &tests.Tree{Root: root, FW: fw, Packages: pkgs}, err: err}
	}
}

// refreshTests starts a discovery walk unless one is running.
func (m *Model) refreshTests() tea.Cmd {
	st := m.testView
	if st == nil || st.discovering {
		return nil
	}
	st.discovering = true
	return discoverTestsCmd()
}

// applyTestsDiscovered installs a fresh tree, keeping last results, and
// starts a run that was waiting for it.
func (m *Model) applyTestsDiscovered(msg testsDiscoveredMsg) tea.Cmd {
	st := m.testView
	st.discovering = false
	st.discovered = true
	st.err = ""
	if msg.err != nil {
		st.err = msg.err.Error()
	}
	if msg.tree != nil {
		if st.tree != nil && !st.running {
			tests.CarryOver(st.tree.Packages, msg.tree.Packages)
		}
		if !st.running || st.tree == nil {
			st.tree = msg.tree
		}
	}
	m.testsClampCursor()
	if req := st.pending; req != nil {
		st.pending = nil
		return m.runTestsRequest(*req)
	}
	return nil
}

// runTestsRequest resolves a request against the tree and starts the run.
// With no tree yet it queues the request behind a discovery walk.
func (m *Model) runTestsRequest(req testsRunRequest) tea.Cmd {
	st := m.testView
	if st == nil {
		return nil
	}
	if !st.discovered || st.tree == nil {
		st.pending = &req
		return m.refreshTests()
	}
	if st.tree.FW == tests.FrameworkNone {
		var t tea.Cmd
		m.toast, t = m.toast.Push(toast.Warn, "No test runner detected for this project")
		return t
	}
	scope := tests.Scope{Kind: tests.ScopeAll}
	switch {
	case req.failed:
		var failed []*tests.Case
		st.tree.Each(func(_ *tests.Package, _ *tests.File, c *tests.Case) {
			if c.Status == tests.StatusFailed {
				failed = append(failed, c)
			}
		})
		if len(failed) == 0 {
			var t tea.Cmd
			m.toast, t = m.toast.Push(toast.Info, "No failed tests to rerun")
			return t
		}
		scope = tests.Scope{Kind: tests.ScopeCases, Cases: failed}
	case req.pkgDir != "":
		for _, p := range st.tree.Packages {
			if p.Dir == req.pkgDir {
				scope = tests.Scope{Kind: tests.ScopePackage, Package: p}
			}
		}
		if scope.Kind != tests.ScopePackage {
			// No discovered tests there (yet): run the directory anyway.
			scope = tests.Scope{Kind: tests.ScopePackage, Package: &tests.Package{Dir: req.pkgDir}}
		}
	case req.file != "" && len(req.caseKey) == 0:
		_, f := st.tree.FindFile(req.file)
		if f == nil {
			var t tea.Cmd
			m.toast, t = m.toast.Push(toast.Warn, "No tests found in "+filepath.Base(req.file))
			return t
		}
		scope = tests.Scope{Kind: tests.ScopeFile, File: f}
		if req.atLine > 0 {
			if c := testCaseAtLine(f, req.atLine); c != nil {
				scope = tests.Scope{Kind: tests.ScopeCases, Cases: []*tests.Case{c}}
			}
		}
	case len(req.caseKey) > 0:
		want := map[string]bool{}
		for _, k := range req.caseKey {
			want[k] = true
		}
		var cs []*tests.Case
		st.tree.Each(func(_ *tests.Package, _ *tests.File, c *tests.Case) {
			if want[c.Key()] {
				cs = append(cs, c)
			}
		})
		if len(cs) == 0 && req.goFunc != "" && req.file != "" {
			cs = []*tests.Case{{Name: req.goFunc, File: req.file}}
		}
		if len(cs) == 0 {
			var t tea.Cmd
			m.toast, t = m.toast.Push(toast.Warn, "Test not found — try Testing: Refresh")
			return t
		}
		scope = tests.Scope{Kind: tests.ScopeCases, Cases: cs}
	}
	return m.startTestRun(scope)
}

// startTestRun launches the command for scope, replacing a running one.
func (m *Model) startTestRun(scope tests.Scope) tea.Cmd {
	st := m.testView
	m.stopTestRun()
	report := ""
	switch st.tree.FW {
	case tests.FrameworkPytest:
		report = filepath.Join(os.TempDir(), fmt.Sprintf("termocode-pytest-%d-%d.xml", os.Getpid(), st.runID+1))
	case tests.FrameworkJest, tests.FrameworkVitest:
		report = filepath.Join(os.TempDir(), fmt.Sprintf("termocode-jest-%d-%d.json", os.Getpid(), st.runID+1))
	}
	cmd, err := tests.BuildCommand(st.tree, scope, report)
	if err != nil {
		st.err = err.Error()
		return nil
	}
	if cmd.Args[0] == "pytest" {
		if _, err := exec.LookPath("pytest"); err != nil {
			cmd.Args = append([]string{"python3", "-m", "pytest"}, cmd.Args[1:]...)
		}
	}
	if _, err := exec.LookPath(cmd.Args[0]); err != nil {
		st.runError = cmd.Args[0] + " not found on PATH"
		var t tea.Cmd
		m.toast, t = m.toast.PushDetail(toast.Errr, "Cannot run tests", st.runError)
		return t
	}

	st.runID++
	st.tree.MarkRunning(scope.Includes)
	st.running, st.hasRun = true, false
	st.cmd, st.parser = cmd, tests.NewLineParser(st.tree.FW)
	st.started, st.elapsed = time.Now(), 0
	st.runError, st.results = "", 0
	st.log = []string{"$ " + cmd.Label()}
	st.resultsScroll = 0

	c := exec.Command(cmd.Args[0], cmd.Args[1:]...)
	c.Dir = cmd.Dir
	c.Env = append(os.Environ(), cmd.Env...)
	setTestProcGroup(c)
	pr, pw := io.Pipe()
	c.Stdout, c.Stderr = pw, pw
	if err := c.Start(); err != nil {
		st.running = false
		st.tree.FinishRun()
		st.runError = err.Error()
		var t tea.Cmd
		m.toast, t = m.toast.PushDetail(toast.Errr, "Cannot run tests", err.Error())
		return t
	}
	st.proc = c
	lines := make(chan string, 1024)
	done := make(chan error, 1)
	go func() {
		err := c.Wait()
		pw.Close()
		done <- err
	}()
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for sc.Scan() {
			lines <- sc.Text()
		}
		// Drain whatever is left so the writer never blocks.
		_, _ = io.Copy(io.Discard, pr)
		close(lines)
	}()
	return waitTestLines(st.runID, lines, done, cmd.Report)
}

// waitTestLines returns the next batch of lines (up to 500 already
// queued) or, when the stream ended, the run's testDoneMsg.
func waitTestLines(runID int, lines <-chan string, done <-chan error, report string) tea.Cmd {
	var self tea.Cmd
	self = func() tea.Msg {
		first, ok := <-lines
		if !ok {
			err := <-done
			msg := testDoneMsg{runID: runID, err: err}
			if report != "" {
				msg.report, _ = os.ReadFile(report)
				_ = os.Remove(report)
			}
			return msg
		}
		batch := []string{first}
		for len(batch) < 500 {
			select {
			case ln, ok := <-lines:
				if !ok {
					return testLinesMsg{runID: runID, lines: batch, next: self}
				}
				batch = append(batch, ln)
			default:
				return testLinesMsg{runID: runID, lines: batch, next: self}
			}
		}
		return testLinesMsg{runID: runID, lines: batch, next: self}
	}
	return self
}

// stopTestRun kills the running process (if any). Its pending messages are
// ignored because runID moves on with the next run.
func (m *Model) stopTestRun() {
	st := m.testView
	if st == nil || !st.running {
		return
	}
	if st.proc != nil && st.proc.Process != nil {
		killTestProc(st.proc)
	}
	st.running = false
	st.proc = nil
	st.runID++
	if st.tree != nil {
		st.tree.FinishRun()
	}
	st.appendLog("[stopped]")
}

// appendLog adds lines to the run log, keeping it under testLogMax.
func (st *testsState) appendLog(lines ...string) {
	for _, ln := range lines {
		st.log = append(st.log, sanitizeOutputLine(ln))
	}
	if over := len(st.log) - testLogMax; over > 0 {
		st.log = append([]string(nil), st.log[over:]...)
	}
	if st.resultsScroll > 0 {
		st.resultsScroll += len(lines)
	}
}

// applyTestLines parses a batch into the tree and log. Batches of a
// stopped (stale) run are dropped, but the reader is still re-armed so the
// old process's pipe drains and its goroutines exit.
func (m *Model) applyTestLines(msg testLinesMsg) tea.Cmd {
	st := m.testView
	if st == nil || msg.runID != st.runID || !st.running {
		return msg.next
	}
	for _, ln := range msg.lines {
		m.applyTestEvents(st.parser.Line(ln))
	}
	return msg.next
}

// applyTestEvents folds parsed events into the tree and the log (for go
// test -json the log gets the decoded Output text, not the raw JSON).
func (m *Model) applyTestEvents(evs []tests.Event) {
	st := m.testView
	for _, ev := range evs {
		switch ev.Kind {
		case tests.EventOutput:
			st.appendLog(ev.Line)
		case tests.EventResult:
			if st.tree.Apply(ev) != nil {
				st.results++
			}
		default:
			st.tree.Apply(ev)
		}
	}
}

// finishTestRun handles testDoneMsg: report parsing, summary, toast.
func (m *Model) finishTestRun(msg testDoneMsg) tea.Cmd {
	st := m.testView
	if st == nil || msg.runID != st.runID || !st.running {
		return nil
	}
	m.applyTestEvents(st.parser.Flush())
	if len(msg.report) > 0 {
		var evs []tests.Event
		var err error
		switch st.tree.FW {
		case tests.FrameworkPytest:
			evs, err = tests.ParseJUnitXML(msg.report)
		case tests.FrameworkJest, tests.FrameworkVitest:
			evs, err = tests.ParseJestJSON(msg.report)
		}
		if err != nil {
			st.appendLog("[report: " + err.Error() + "]")
		}
		for _, ev := range evs {
			if st.tree.Apply(ev) != nil {
				st.results++
			}
		}
	}
	st.tree.FinishRun()
	st.running, st.proc, st.hasRun = false, nil, true
	st.elapsed = time.Since(st.started).Round(10 * time.Millisecond)
	c := st.tree.Counts()
	exit := "exit 0"
	if msg.err != nil {
		exit = msg.err.Error()
		if st.results == 0 && !testsAnyPackageError(st.tree) {
			// Nothing parsed: surface the tail of the log as the reason.
			st.runError = testsLogTail(st.log, 3)
		}
	}
	st.appendLog(fmt.Sprintf("[done in %s · %s · ✓ %d ✗ %d ⊘ %d]", st.elapsed, exit, c.Passed, c.Failed, c.Skipped))

	var t tea.Cmd
	switch {
	case st.runError != "":
		m.toast, t = m.toast.PushDetail(toast.Errr, "Test run failed", st.runError)
	case c.Failed > 0 || testsAnyPackageError(st.tree):
		m.toast, t = m.toast.PushDetail(toast.Warn, fmt.Sprintf("Tests: %d failed, %d passed", c.Failed, c.Passed), st.cmd.Label())
	default:
		m.toast, t = m.toast.PushDetail(toast.Info, fmt.Sprintf("Tests: %d passed", c.Passed), st.cmd.Label())
	}
	return t
}

func testsAnyPackageError(t *tests.Tree) bool {
	if t == nil {
		return false
	}
	for _, p := range t.Packages {
		if p.Error != "" {
			return true
		}
	}
	return false
}

// testsLogTail returns the last n non-empty, non-marker log lines.
func testsLogTail(log []string, n int) string {
	var out []string
	for i := len(log) - 1; i >= 0 && len(out) < n; i-- {
		ln := strings.TrimSpace(log[i])
		if ln == "" || strings.HasPrefix(ln, "$ ") || strings.HasPrefix(ln, "{") {
			continue
		}
		out = append([]string{ln}, out...)
	}
	if len(out) == 0 {
		return "the test command exited with an error"
	}
	return strings.Join(out, "\n")
}

// runTestsCmd is Alt+T / "Run: Tests": run every test with the streaming
// runner and reveal the Testing view (without stealing keyboard focus).
func (m *Model) runTestsCmd() tea.Cmd {
	m.revealTestsView(false)
	return m.runTestsRequest(testsRunRequest{all: true})
}

// colourTestOutput adds light ANSI colouring to the output so PASS/FAIL
// jump out at a glance. Operates line-by-line; preserves any ANSI the test
// runner already emitted. Used by user commands (user_commands.go).
func colourTestOutput(s string) string {
	const (
		reset = "\x1b[0m"
		green = "\x1b[38;2;115;201;145m"
		red   = "\x1b[38;2;199;78;57m"
		dim   = "\x1b[38;2;138;138;138m"
	)
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		switch {
		case strings.Contains(line, "FAIL") && !strings.Contains(line, "FAIL\t0"):
			b.WriteString(red)
			b.WriteString(line)
			b.WriteString(reset)
		case strings.HasPrefix(strings.TrimSpace(line), "ok ") || strings.HasPrefix(strings.TrimSpace(line), "PASS"):
			b.WriteString(green)
			b.WriteString(line)
			b.WriteString(reset)
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "==="):
			b.WriteString(dim)
			b.WriteString(line)
			b.WriteString(reset)
		default:
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	return b.String()
}
