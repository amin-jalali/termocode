package app

import (
	"reflect"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/tests"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The Test Explorer from the page (WEBP-4). The tree is the laptop's
// TESTING view's (m.testView): its discovery, its runner, its results.
// The page names a row by its id; the hub finds that row in the tree, so
// no path or command line from the page reaches the runner. A browser
// that listens starts the first discovery, as opening the view does on
// the laptop. The topic is read again when the runner's books change.

func init() {
	webTopics["tests"] = webTopicDef{snapshot: webTestsSnapshot, apply: webApplyReplace[webproto.TestsSnap]}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishTests)
	webOps[webproto.OpTestsRun] = (*Model).webTestsRun
	webOps[webproto.OpTestsDebug] = (*Model).webTestsDebug
	webOps[webproto.OpTestsStop] = (*Model).webTestsStop
	webOps[webproto.OpTestsRefresh] = (*Model).webTestsRefresh
	webOps[webproto.OpTestsOutput] = (*Model).webTestsOutput
}

// Tests swap these: start a run, start a debug session.
var (
	webStartTests    = func(m *Model, req testsRunRequest) (tea.Cmd, *testsRunProblem) { return m.tryTestsRequest(req) }
	webDebugStartCfg = func(m *Model, cfg launchConfig) (tea.Cmd, error) { return m.tryDebugStartConfig(cfg) }
)

// webRunPub is what the Run, Test and Debug topics sent last (Update only).
type webRunPub struct {
	tests webTestsPub
	debug webDebugPub
}

type webTestsPub struct {
	key  string
	last *webproto.TestsSnap
}

// webTestsDelta is a tests delta: the whole tree; a newer one replaces a
// queued one.
type webTestsDelta webproto.TestsSnap

func (webTestsDelta) coalesce(next any) (any, bool) {
	n, ok := next.(webTestsDelta)
	return n, ok
}

// webTestsKey fingerprints the runner's books.
func (m *Model) webTestsKey() string {
	st := m.testView
	if st == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(strconv.FormatBool(st.discovered) + strconv.FormatBool(st.discovering) + strconv.FormatBool(st.running) + strconv.FormatBool(st.hasRun))
	b.WriteString("|" + strconv.Itoa(st.runID) + "|" + strconv.Itoa(st.results) + "|" + strconv.Itoa(len(st.log)))
	if n := len(st.log); n > 0 {
		b.WriteString("|" + st.log[n-1])
	}
	b.WriteString("|" + st.err + "|" + st.runError)
	if st.tree != nil {
		b.WriteString("|" + strconv.FormatUint(uint64(reflect.ValueOf(st.tree).Pointer()), 16))
	}
	return b.String()
}

func webTestsSnapshot(m *Model, _ string) (any, error) {
	snap := m.webTestsBuild()
	if m.web != nil {
		m.web.run.tests = webTestsPub{key: m.webTestsKey(), last: &snap}
	}
	return snap, nil
}

// webPublishTests sends the tree when it changed; the first browser that
// listens starts the discovery.
func (m *Model) webPublishTests(tea.Msg) tea.Cmd {
	h := m.web
	p := &h.run.tests
	if !h.webListens("tests") {
		p.last = nil
		return nil
	}
	var cmd tea.Cmd
	if st := m.testView; st != nil && !st.discovered && !st.discovering {
		cmd = m.refreshTests()
	}
	key := m.webTestsKey()
	if p.last != nil && key == p.key {
		return cmd
	}
	p.key = key
	cur := m.webTestsBuild()
	if p.last != nil && reflect.DeepEqual(*p.last, cur) {
		return cmd
	}
	p.last = &cur
	h.publish("tests", webTestsDelta(cur))
	return cmd
}

// webTestState is a status on the wire.
func webTestState(s tests.Status) string {
	switch s {
	case tests.StatusRunning:
		return "running"
	case tests.StatusPassed:
		return "passed"
	case tests.StatusFailed:
		return "failed"
	case tests.StatusSkipped:
		return "skipped"
	}
	return "pending"
}

// webTestSum is a package's or file's state from its counts.
func webTestSum(c tests.Counts) string {
	switch {
	case c.Failed > 0:
		return "failed"
	case c.Running > 0:
		return "running"
	case c.Total > 0 && c.Passed+c.Skipped == c.Total && c.Passed > 0:
		return "passed"
	case c.Total > 0 && c.Skipped == c.Total:
		return "skipped"
	}
	return "pending"
}

// webTestRow is one row of the tree and what it stands for.
type webTestRow struct {
	item webproto.TestItem
	pkg  *tests.Package
	file *tests.File
	c    *tests.Case
}

// webTestRows walks the tree in the order the laptop shows it. Rows
// outside the workspace are left out.
func (m *Model) webTestRows() []webTestRow {
	st := m.testView
	if st == nil || st.tree == nil {
		return nil
	}
	debug := st.tree.FW == tests.FrameworkGo || st.tree.FW == tests.FrameworkPytest
	var out []webTestRow
	for _, p := range st.tree.Packages {
		dir, ok := m.webRel(p.Dir)
		if !ok {
			continue
		}
		pid := "p:" + dir
		pi := webproto.TestItem{ID: pid, Kind: "package", Label: webName(testsPkgLabel(st.tree.Root, p)), State: webTestSum(tests.CountPackage(p))}
		if p.Error != "" {
			pi.State, pi.Message = "failed", webClip(p.Error, webproto.MaxTestMessage)
			if rel, ok := m.webRel(p.ErrFile); ok && p.ErrFile != "" {
				pi.FailPath, pi.FailLine = rel, p.ErrLine
			}
		}
		out = append(out, webTestRow{item: pi, pkg: p})
		for _, f := range p.Files {
			rel, ok := m.webRel(f.Path)
			if !ok {
				continue
			}
			fid := "f:" + rel
			label := rel
			if i := strings.LastIndexByte(rel, '/'); i >= 0 {
				label = rel[i+1:]
			}
			out = append(out, webTestRow{item: webproto.TestItem{ID: fid, Kind: "file", Parent: pid, Label: webName(label), Path: rel, State: webTestSum(tests.CountFile(f))}, pkg: p, file: f})
			for _, c := range f.Cases {
				ci := webproto.TestItem{ID: "c:" + rel + "::" + c.Name, Kind: "case", Parent: fid, Label: webName(c.Name), Path: rel, Line: c.Line,
					State: webTestState(c.Status), Ms: c.Duration.Milliseconds(), Debug: debug}
				if c.Status == tests.StatusFailed {
					ci.Message = webClip(c.Message, webproto.MaxTestMessage)
					if fr, ok := m.webRel(c.FailFile); ok && c.FailFile != "" && c.FailLine > 0 {
						ci.FailPath, ci.FailLine = fr, c.FailLine
					}
				}
				out = append(out, webTestRow{item: ci, pkg: p, file: f, c: c})
			}
		}
	}
	return out
}

// webClip cuts s to max bytes at a rune start.
func webClip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && cut < len(s) && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}

// webTestsBuild is the tests topic now.
func (m *Model) webTestsBuild() webproto.TestsSnap {
	s := webproto.TestsSnap{State: "idle", Items: []webproto.TestItem{}}
	st := m.testView
	if st == nil {
		return s
	}
	switch {
	case st.running:
		s.State = "running"
	case st.discovering || !st.discovered:
		s.State = "discovering"
	}
	s.Error, s.RunError = webName(st.err), webClip(st.runError, webproto.MaxTestMessage)
	if st.tree != nil {
		if st.tree.FW != tests.FrameworkNone {
			s.Framework = st.tree.FW.String()
		}
		c := st.tree.Counts()
		s.Counts = webproto.TestCounts{Total: c.Total, Passed: c.Passed, Failed: c.Failed, Skipped: c.Skipped, Running: c.Running}
	}
	if st.runID > 0 || st.hasRun {
		s.Command = webName(st.cmd.Label())
		if !st.started.IsZero() {
			s.Started = st.started.UnixMilli()
		}
		s.ElapsedMs = st.elapsed.Milliseconds()
		if st.running {
			s.ElapsedMs, s.Live = webNow().Sub(st.started).Milliseconds(), true
		}
	}
	for i, r := range m.webTestRows() {
		if i == webproto.MaxTestItems {
			s.More++
			continue
		}
		s.Items = append(s.Items, r.item)
	}
	return s
}

// webTestFind is the row id names, or a not_found answer.
func (m *Model) webTestFind(call *webCall, id string) (webTestRow, bool) {
	for _, r := range m.webTestRows() {
		if r.item.ID == id {
			return r, true
		}
	}
	call.fail(webproto.CodeNotFound, "That test is not in the list. Refresh the tests.")
	return webTestRow{}, false
}

// webTestsRun runs a row of the tree, every test, or the failed ones.
func (m *Model) webTestsRun(call *webCall) tea.Cmd {
	a := call.args.(*webproto.TestRunArgs)
	req := testsRunRequest{all: a.ID == "" && !a.Failed, failed: a.Failed}
	what := "all tests"
	if a.Failed {
		what = "failed tests"
	}
	if a.ID != "" {
		r, ok := m.webTestFind(call, a.ID)
		if !ok {
			return nil
		}
		switch r.item.Kind {
		case "package":
			req.pkgDir = r.pkg.Dir
		case "file":
			req.file = r.file.Path
		default:
			req.caseKey = []string{r.c.Key()}
		}
		what = a.ID
	}
	cmd, problem := webStartTests(m, req)
	if problem != nil {
		m.webAuditOp(call, what, "failed")
		call.fail(webproto.CodeConflict, problem.msg+".")
		return nil
	}
	m.webAuditOp(call, what, "ok")
	call.reply(nil)
	return cmd
}

// webTestsDebug starts a debug session for one test (Go and pytest).
func (m *Model) webTestsDebug(call *webCall) tea.Cmd {
	id := call.args.(*webproto.TestIDArgs).ID
	r, ok := m.webTestFind(call, id)
	if !ok {
		return nil
	}
	if r.c == nil {
		call.fail(webproto.CodeBadRequest, "Pick one test to debug.")
		return nil
	}
	if m.dapSessionActive {
		call.fail(webproto.CodeConflict, "A debug session runs. Stop it first.")
		return nil
	}
	fw, root := m.testFrameworkFor(r.c.File)
	cfg, err := testDebugConfig(fw, root, r.c.File, r.c.Name)
	if err != nil {
		call.fail(webproto.CodeConflict, webSentence(err.Error()))
		return nil
	}
	cmd, err := webDebugStartCfg(m, cfg)
	if err != nil {
		m.webAuditOp(call, id, "failed")
		call.fail(webproto.CodeConflict, webSentence(err.Error()))
		return cmd
	}
	m.webAuditOp(call, id, "ok")
	call.reply(nil)
	return cmd
}

// webSentence starts s with a capital letter and ends it with a period.
func webSentence(s string) string {
	s = strings.TrimSpace(webName(s))
	if s == "" {
		return s
	}
	if c := s[0]; c >= 'a' && c <= 'z' {
		s = string(c-'a'+'A') + s[1:]
	}
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

// webTestsStop stops the running test run.
func (m *Model) webTestsStop(call *webCall) tea.Cmd {
	if st := m.testView; st == nil || !st.running {
		call.fail(webproto.CodeConflict, "No tests are running.")
		return nil
	}
	m.stopTestRun()
	m.webAuditOp(call, "", "ok")
	call.reply(nil)
	return nil
}

// webTestsRefresh finds the tests again.
func (m *Model) webTestsRefresh(call *webCall) tea.Cmd {
	call.reply(nil)
	return m.refreshTests()
}

// webTestsOutput answers one test's output from the last run, or (no
// id) the run's log: the newest lines, capped.
func (m *Model) webTestsOutput(call *webCall) tea.Cmd {
	id := call.args.(*webproto.TestOutputArgs).ID
	st := m.testView
	if st == nil {
		call.fail(webproto.CodeNotFound, "No tests here.")
		return nil
	}
	lines := st.log
	if id != "" {
		r, ok := m.webTestFind(call, id)
		if !ok {
			return nil
		}
		switch {
		case r.c != nil:
			lines = r.c.Output
			if len(lines) == 0 && r.c.Message != "" {
				lines = strings.Split(r.c.Message, "\n")
			}
		case r.item.Kind == "package" && r.pkg.Error != "":
			lines = strings.Split(r.pkg.Error, "\n")
		}
	}
	out := webproto.TestOutput{ID: id, Lines: []string{}}
	if len(lines) > webproto.MaxTestOutputLines {
		lines, out.Truncated = lines[len(lines)-webproto.MaxTestOutputLines:], true
	}
	size, from := 0, len(lines)
	for from > 0 && size+len(lines[from-1])+1 <= webproto.MaxTestOutputBytes {
		from--
		size += len(lines[from]) + 1
	}
	if from > 0 {
		out.Truncated = true
	}
	for _, l := range lines[from:] {
		out.Lines = append(out.Lines, sanitizeOutputLine(l))
	}
	call.reply(out)
	return nil
}
