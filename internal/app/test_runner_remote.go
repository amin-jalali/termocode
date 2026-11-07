package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/tests"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// ── Tests in a container workspace (RC-5 4d) ─────────────────────────────
//
// Discovery reads the test files through the Host FS; a run is the same
// command as on this machine, started in the container through the Host
// Exec (`docker exec -i -w <root> …`). Owner rule (plan RC, decision 4):
// a container termocode did not create is never written to, so no report
// file goes to its /tmp: pytest results come from the -v lines and
// jest/vitest print their JSON report into the output.

// remoteTestDiscoverTimeout caps one discovery walk in a container.
const remoteTestDiscoverTimeout = 2 * time.Minute

// remoteTestOutMax caps the output kept for the jest/vitest report.
const remoteTestOutMax = 8 << 20

// discoverRemoteTestsCmd walks root through fsys off the UI goroutine.
func discoverRemoteTestsCmd(fsys workspace.FS, root string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), remoteTestDiscoverTimeout)
		defer cancel()
		fw := tests.DetectFS(ctx, fsys, root)
		if fw == tests.FrameworkNone {
			return testsDiscoveredMsg{tree: &tests.Tree{Root: root}, err: tests.ErrNoFramework}
		}
		pkgs, err := tests.DiscoverFS(ctx, fsys, root, fw)
		return testsDiscoveredMsg{tree: &tests.Tree{Root: root, FW: fw, Packages: pkgs}, err: err}
	}
}

// remoteTestExec is the Host Exec of a container workspace and its short
// name; ok is false for a local folder.
func (m *Model) remoteTestExec() (services.Exec, string, bool) {
	ws := m.workspace()
	if isLocal(ws) || ws.Host == nil {
		return nil, "", false
	}
	return ws.Host.Exec(), ws.Host.Label(), true
}

// errNoRemoteShell: tests in a container need its sh (the run wrapper).
var errNoRemoteShell = errors.New("this container has no sh, so tests cannot run there; use a terminal tab")

// startRemoteTestRun runs scope in the container through x.
func (m *Model) startRemoteTestRun(scope tests.Scope, x services.Exec, label string) tea.Cmd {
	st := m.testView
	cmd, err := tests.BuildCommand(st.tree, scope, "") // no report file in the container
	if err != nil {
		st.err = err.Error()
		return nil
	}
	fail := func(why string) tea.Cmd {
		st.runError = why
		var t tea.Cmd
		m.toast, t = m.toast.PushDetail(toast.Errr, "Cannot run tests", why)
		return t
	}
	if x == nil {
		return fail(errNoHostExec.Error())
	}
	if ws := m.workspace(); ws.Host != nil && !ws.Host.Caps().HasShell {
		return fail(errNoRemoteShell.Error())
	}

	st.runID++
	runID := st.runID
	st.tree.MarkRunning(scope.Includes)
	st.running, st.hasRun = true, false
	st.cmd, st.parser = cmd, tests.NewLineParser(st.tree.FW)
	st.started, st.elapsed = time.Now(), 0
	st.runError, st.results = "", 0
	st.log = []string{"$ " + cmd.Label() + "   (in " + label + ")"}
	st.resultsScroll = 0

	ctx, cancel := context.WithCancel(context.Background())
	out, stop, err := x.Stream(ctx, cmd.Dir, tests.RemoteArgv(cmd)...)
	if err != nil {
		cancel()
		st.running = false
		st.tree.FinishRun()
		return fail(remoteErrText(label, err))
	}
	stopAll := func() {
		stop()
		cancel()
	}
	st.stop = stopAll
	liveTestProcs.addRemote(runID, stopAll)
	lines := make(chan string, 1024)
	done := make(chan remoteTestEnd, 1)
	crash.Go("the test runner", func() {
		done <- scanRemoteTestOutput(out, lines, cmd.ReportOnStdout, label, cmd.Args[0])
		liveTestProcs.removeRemote(runID)
		stopAll()
	})
	return waitRemoteTestLines(runID, lines, done)
}

// remoteTestEnd is how a container run ended.
type remoteTestEnd struct {
	err    error
	report []byte
}

// scanRemoteTestOutput sends every output line but the exit marker to
// lines (then closes it) and gives the run's end: a non-zero exit (or no
// marker: the stream broke) is err; keepReport keeps the output for the
// jest/vitest JSON report.
func scanRemoteTestOutput(out io.ReadCloser, lines chan<- string, keepReport bool, label, prog string) remoteTestEnd {
	defer close(lines)
	defer out.Close()
	var kept []byte
	code, marked := 0, false
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		ln := sc.Text()
		if c, ok := tests.ParseExitMarker(ln); ok {
			code, marked = c, true
			continue
		}
		if keepReport && len(kept)+len(ln)+1 <= remoteTestOutMax {
			kept = append(append(kept, ln...), '\n')
		}
		lines <- ln
	}
	_, _ = io.Copy(io.Discard, out)
	end := remoteTestEnd{}
	if keepReport {
		end.report = tests.ExtractJSONReport(kept)
	}
	switch {
	case !marked:
		end.err = fmt.Errorf("the run in %s ended without an exit code (the container stopped or the connection broke)", label)
	case code == 127:
		end.err = fmt.Errorf("%s not found in %s (exit 127)", prog, label)
	case code != 0:
		end.err = fmt.Errorf("exit status %d", code)
	}
	return end
}

// waitRemoteTestLines is waitTestLines for a container run: the report
// comes from the output, not from a file.
func waitRemoteTestLines(runID int, lines <-chan string, done <-chan remoteTestEnd) tea.Cmd {
	var self tea.Cmd
	self = func() tea.Msg {
		first, ok := <-lines
		if !ok {
			end := <-done
			return testDoneMsg{runID: runID, err: end.err, report: end.report}
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

func (s *testProcSet) addRemote(id int, stop func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.remote == nil {
		s.remote = map[int]func(){}
	}
	s.remote[id] = stop
}

func (s *testProcSet) removeRemote(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.remote, id)
}

// stopRemoteLocked ends every container run; s.mu is held.
func (s *testProcSet) stopRemoteLocked() {
	for id, stop := range s.remote {
		stop()
		delete(s.remote, id)
	}
}
