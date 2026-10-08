package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Group D — pure tests for Run & Debug (launch configs, breakpoint store,
// Debug Console, Run view rows / layout, event + result routing).

func TestParseLaunchJSONC(t *testing.T) {
	src := `{
	  // VSCode-style comments
	  "version": "0.2.0",
	  "configurations": [
	    { "name": "Launch pkg", "type": "go", "request": "launch", "program": "${workspaceFolder}", }, /* trailing */
	    { "name": "Build", "command": "make build" },
	    { "name": "no type" },
	    { "type": "python", "program": "a // not a comment.py" },
	  ],
	}`
	cfgs, err := parseLaunchJSON([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cfgs) != 3 {
		t.Fatalf("got %d configs: %+v", len(cfgs), cfgs)
	}
	if c := cfgs[0]; c.Name != "Launch pkg" || c.Type != "go" || !c.Debuggable() || c.Source != "launch.json" {
		t.Errorf("cfg0 = %+v", c)
	}
	if c := cfgs[1]; c.Type != "shell" || c.Debuggable() || c.RunCommand() != "make build" {
		t.Errorf("cfg1 = %+v", c)
	}
	if c := cfgs[2]; c.Name != "Configuration 4" || c.Program != "a // not a comment.py" || c.Request != "launch" || c.Raw["request"] != "launch" {
		t.Errorf("cfg2 = %+v", c)
	}
}

func TestDetectLaunchConfigs(t *testing.T) {
	dir := t.TempDir()
	if got := detectLaunchConfigs(dir); len(got) != 0 {
		t.Fatalf("empty dir: %+v", got)
	}
	for _, f := range []string{"go.mod", "package.json", "Makefile"} {
		_ = os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644)
	}
	got := detectLaunchConfigs(dir)
	names := map[string]launchConfig{}
	for _, c := range got {
		names[c.Name] = c
	}
	for _, want := range []string{"Go: Debug package", "Go: Run package", "Go: Test", "npm start", "Node: Debug current file", "make"} {
		if _, ok := names[want]; !ok {
			t.Errorf("missing %q in %v", want, got)
		}
	}
	if !got[0].Debuggable() {
		t.Errorf("first config should be a debug config: %+v", got[0])
	}
	if names["Go: Run package"].RunCommand() != "go run ." {
		t.Errorf("run command = %q", names["Go: Run package"].RunCommand())
	}
}

func TestMergeLaunchConfigsUserFirst(t *testing.T) {
	user := []launchConfig{{Name: "make"}, {Name: "Mine"}}
	auto := []launchConfig{{Name: "Go: Test"}, {Name: "make"}}
	got := mergeLaunchConfigs(user, auto)
	if len(got) != 3 || got[0].Name != "make" || got[1].Name != "Mine" || got[2].Name != "Go: Test" {
		t.Errorf("merge = %+v", got)
	}
}

func TestRunCommandAndExpand(t *testing.T) {
	c := launchConfig{Type: "delve", Program: "${fileDirname}"}
	cmd := expandLaunchVars(c.RunCommand(), "/ws", "/ws/cmd/app/main.go")
	if cmd != "go run '/ws/cmd/app'" {
		t.Errorf("go run = %q", cmd)
	}
	c = launchConfig{Type: "python"}
	if got := expandLaunchVars(c.RunCommand(), "/ws", "/ws/a b.py"); got != "python3 '/ws/a b.py'" {
		t.Errorf("python = %q", got)
	}
	if (launchConfig{Type: "lldb"}).RunCommand() != "" {
		t.Error("unknown adapter type has no run command")
	}
}

func TestConfigCacheReloadsOnChange(t *testing.T) {
	dir := t.TempDir()
	var c configCache
	now := time.Now()
	if got := c.get(dir, now); len(got) != 0 {
		t.Fatalf("empty: %+v", got)
	}
	_ = os.MkdirAll(filepath.Join(dir, ".termocode"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, launchJSONRel), []byte(`{"configurations":[{"name":"x","type":"go"}]}`), 0o644)
	if got := c.get(dir, now.Add(time.Second)); len(got) != 0 {
		t.Fatalf("throttled read should keep the old list: %+v", got)
	}
	if got := c.get(dir, now.Add(3*time.Second)); len(got) != 1 || got[0].Name != "x" {
		t.Fatalf("after change: %+v", got)
	}
	_ = os.WriteFile(filepath.Join(dir, launchJSONRel), []byte(`{bad`), 0o644)
	c.invalidate()
	c.get(dir, now.Add(4*time.Second))
	if !strings.HasPrefix(c.err, "launch.json:") {
		t.Errorf("parse error not reported: %q", c.err)
	}
}

func TestBreakpointFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "breakpoints.json")
	if got := loadBreakpointFile(path); len(got) != 0 {
		t.Fatalf("missing file: %+v", got)
	}
	store := map[string][]storedBreakpoint{
		"/a/x.go": {{Line: 9, Text: "b()"}, {Line: 3, Text: "a()", Condition: "i > 2"}},
	}
	if err := saveBreakpointFile(path, store); err != nil {
		t.Fatal(err)
	}
	got := loadBreakpointFile(path)
	if l := got["/a/x.go"]; len(l) != 2 || l[0].Line != 3 || l[0].Condition != "i > 2" || l[1].Line != 9 {
		t.Errorf("round trip = %+v", got)
	}
	_ = os.WriteFile(path, []byte("{broken"), 0o644)
	if got := loadBreakpointFile(path); len(got) != 0 {
		t.Errorf("broken file must load empty: %+v", got)
	}
}

func TestMergeBreakpoints(t *testing.T) {
	store := map[string][]storedBreakpoint{
		"/a.go": {{Line: 1}},
		"/b.go": {{Line: 2}},
		"/c.go": {{Line: 3}},
	}
	live := liveBreakpoints{
		Loaded: []string{"/a.go", "/b.go", "/d.go"},
		Bps: map[string][]liveBreakpoint{
			"/b.go": {{storedBreakpoint: storedBreakpoint{Line: 5, Text: "x"}}},
			"/d.go": {{storedBreakpoint: storedBreakpoint{Line: 7}}},
		},
	}
	got, changed := mergeBreakpoints(store, live)
	if !changed {
		t.Fatal("expected a change")
	}
	if _, ok := got["/a.go"]; ok {
		t.Error("loaded file without breakpoints must be dropped")
	}
	if l := got["/b.go"]; len(l) != 1 || l[0].Line != 5 {
		t.Errorf("/b.go = %+v", l)
	}
	if l := got["/c.go"]; len(l) != 1 || l[0].Line != 3 {
		t.Errorf("unloaded /c.go must be kept: %+v", l)
	}
	if l := got["/d.go"]; len(l) != 1 {
		t.Errorf("/d.go = %+v", l)
	}
	if _, changed := mergeBreakpoints(got, live); changed {
		t.Error("second merge must be a no-op")
	}
	if len(store) != 3 {
		t.Error("merge must not mutate its input")
	}
}

func TestDebugConsoleOutputAndInput(t *testing.T) {
	c := newDebugConsole()
	c.AppendOutput("stdout", "hel")
	c.AppendOutput("stdout", "lo\nwor")
	c.AppendOutput("stderr", "boom\r\n")
	lines := c.Lines()
	if len(lines) != 3 || lines[0].Text != "hello" || lines[1].Category != "stderr" || lines[2].Text != "wor" {
		t.Fatalf("lines = %+v", lines)
	}
	c.insert([]rune("a+b"))
	c.cursor = 1
	c.backspace()
	if string(c.input) != "+b" || c.cursor != 0 {
		t.Errorf("input = %q cursor %d", string(c.input), c.cursor)
	}
	if got := c.submit(); got != "+b" || len(c.input) != 0 {
		t.Errorf("submit = %q", got)
	}
	c.insert([]rune("x"))
	c.submit()
	c.historyMove(-1)
	if string(c.input) != "x" {
		t.Errorf("history -1 = %q", string(c.input))
	}
	c.historyMove(-1)
	if string(c.input) != "+b" {
		t.Errorf("history -2 = %q", string(c.input))
	}
	c.historyMove(1)
	c.historyMove(1)
	if len(c.input) != 0 {
		t.Errorf("past the newest entry the line is empty, got %q", string(c.input))
	}
}

func TestRunToolbarLayout(t *testing.T) {
	btns := runToolbarLayout(false, false, "Go: Debug package", 40)
	ids := []string{}
	for _, b := range btns {
		ids = append(ids, b.id)
		if b.end > 40 || b.start < 0 || b.end <= b.start {
			t.Errorf("button out of range: %+v", b)
		}
	}
	if strings.Join(ids, ",") != "start,config,launch" {
		t.Errorf("idle ids = %v", ids)
	}
	btns = runToolbarLayout(true, true, "", 40)
	if btns[0].id != "continue" || btns[len(btns)-1].id != "console" {
		t.Errorf("paused toolbar = %+v", btns)
	}
	if runToolbarLayout(true, false, "", 40)[0].id != "pause" {
		t.Error("running session shows pause")
	}
	for i := 1; i < len(btns); i++ {
		if btns[i].start < btns[i-1].end {
			t.Errorf("overlap: %+v / %+v", btns[i-1], btns[i])
		}
	}
	// Narrow: nothing overflows.
	for _, b := range runToolbarLayout(false, false, "a very long configuration name", 14) {
		if b.end > 14 {
			t.Errorf("narrow overflow: %+v", b)
		}
	}
}

// debugTestModel is a Model with a paused session snapshot.
func debugTestModel(t *testing.T) Model {
	t.Helper()
	t.Chdir(t.TempDir())
	_ = os.WriteFile("go.mod", []byte("module x\n"), 0o644)
	d := newDebugState()
	d.snap = dbgSnapshot{
		Available: true, Active: true, Stopped: true, Name: "dbg", Thread: 1, Frame: 11,
		Threads: []dbgThread{{ID: 1, Name: "main", Stopped: true}},
		Frames: []dbgFrame{
			{ID: 11, Name: "main.add", Path: "/ws/main.go", Line: 8},
			{ID: 12, Name: "main.main", Path: "/ws/main.go", Line: 14},
		},
		Scopes: []dbgScope{
			{Name: "Locals", Ref: 100, Loaded: true, Variables: []dbgVar{
				{Name: "a", Value: "1", Type: "int"},
				{Name: "p", Value: "main.point {X: 1, Y: 2}", Ref: 101},
			}},
			{Name: "Globals", Ref: 200, Expensive: true},
		},
	}
	d.status = "Paused on breakpoint"
	d.watches = []dbgWatch{{Expr: "a+1", Value: "2"}, {Expr: "bad", Value: "undefined", Err: true}}
	d.bps = map[string][]storedBreakpoint{"/ws/main.go": {{Line: 8, Text: "s := a + b"}}}
	return Model{w: 120, h: 40, explorerWidth: 41, debug: d, dapSessionActive: true}
}

func TestRunRowsPausedSession(t *testing.T) {
	m := debugTestModel(t)
	rows := m.runRows()
	var kinds []string
	find := func(kind runRowKind, label string) (runRow, bool) {
		for _, r := range rows {
			if r.kind == kind && r.label == label {
				return r, true
			}
		}
		return runRow{}, false
	}
	for _, r := range rows {
		if r.kind == runRowSection {
			kinds = append(kinds, r.section.label())
		}
	}
	if strings.Join(kinds, ",") != "CONFIGURATIONS,VARIABLES,WATCH,CALL STACK,BREAKPOINTS" {
		t.Errorf("sections = %v", kinds)
	}
	if r, ok := find(runRowVar, "p"); !ok || !r.expandable || r.expanded {
		t.Errorf("p row = %+v", r)
	}
	if r, ok := find(runRowScope, "Globals"); !ok || r.expanded {
		t.Errorf("expensive scope must start collapsed: %+v", r)
	}
	if r, ok := find(runRowFrame, "main.add"); !ok || r.glyph != "▶" || r.detail != "main.go:8" {
		t.Errorf("current frame row = %+v", r)
	}
	if r, ok := find(runRowBreakpoint, "main.go:8"); !ok || r.line != 8 {
		t.Errorf("breakpoint row = %+v", r)
	}
	if r, ok := find(runRowWatch, "bad"); !ok || r.detail != "undefined" {
		t.Errorf("watch row = %+v", r)
	}

	// Opening p shows "Loading…" until its children arrive.
	m.debug.open["s:Locals/p"] = true
	rows = m.runRows()
	if _, ok := find(runRowNote, "Loading…"); !ok {
		t.Error("open var without children shows Loading…")
	}
	m.debug.children["s:Locals/p"] = []dbgVar{{Name: "X", Value: "1"}}
	rows = m.runRows()
	if r, ok := find(runRowVar, "X"); !ok || r.depth != 2 || r.key != "s:Locals/p/X" {
		t.Errorf("child row = %+v", r)
	}
}

func TestRenderRunSidebarWidthContract(t *testing.T) {
	m := debugTestModel(t)
	for _, w := range []int{18, 40, 63} {
		out := m.renderRunSidebar(w, 30)
		lines := strings.Split(out, "\n")
		if len(lines) != 30 {
			t.Fatalf("w=%d: %d lines", w, len(lines))
		}
		for i, ln := range lines {
			if got := lipgloss.Width(ln); got != w {
				t.Errorf("w=%d line %d width %d: %q", w, i, got, ln)
			}
		}
	}
	// Idle (no session) renders too.
	m.debug = nil
	m.dapSessionActive = false
	for i, ln := range strings.Split(m.renderRunSidebar(30, 12), "\n") {
		if lipgloss.Width(ln) != 30 {
			t.Errorf("idle line %d width %d", i, lipgloss.Width(ln))
		}
	}
}

func TestRunSidebarKeysAndToggle(t *testing.T) {
	m := debugTestModel(t)
	m.focus = FocusExplorer
	// Walk down to the "p" row and expand it with →.
	rows := m.runRows()
	for i, r := range rows {
		if r.kind == runRowVar && r.label == "p" {
			m.debug.cursor = i
		}
	}
	m.handleRunSidebarKey(tea.KeyMsg{Type: tea.KeyRight})
	if !m.debug.open["s:Locals/p"] {
		t.Fatal("→ should open p")
	}
	m.handleRunSidebarKey(tea.KeyMsg{Type: tea.KeyLeft})
	if m.debug.open["s:Locals/p"] {
		t.Fatal("← should close p")
	}
	// x on a watch removes it.
	for i, r := range m.runRows() {
		if r.kind == runRowWatch && r.label == "bad" {
			m.debug.cursor = i
		}
	}
	m.handleRunSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if len(m.debug.watches) != 1 || m.debug.watches[0].Expr != "a+1" {
		t.Errorf("watches = %+v", m.debug.watches)
	}
	// Enter on a section header collapses it.
	m.debug.cursor = 0
	m.handleRunSidebarKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.debug.collapsed[runSecConfigs] {
		t.Error("Enter on CONFIGURATIONS should collapse it")
	}
}

func TestDebugResultRouting(t *testing.T) {
	m := debugTestModel(t)
	d := m.debug
	d.pending[1] = dbgPending{kind: "vars", key: "s:Locals/p"}
	d.inflight["s:Locals/p"] = true
	d.pending[2] = dbgPending{kind: "watch", key: "a+1"}
	d.pending[3] = dbgPending{kind: "repl", key: "a"}
	m.onDebugResult(1, dbgResult{Variables: []dbgVar{{Name: "X", Value: "1"}}})
	if len(d.children["s:Locals/p"]) != 1 || d.inflight["s:Locals/p"] {
		t.Errorf("children = %+v inflight=%v", d.children, d.inflight)
	}
	m.onDebugResult(2, dbgResult{Error: "nope"})
	if !d.watches[0].Err || d.watches[0].Value != "nope" {
		t.Errorf("watch = %+v", d.watches[0])
	}
	m.onDebugResult(3, dbgResult{Result: "42"})
	lines := d.console.Lines()
	if len(lines) == 0 || lines[len(lines)-1].Text != "42" || lines[len(lines)-1].Category != "result" {
		t.Errorf("console = %+v", lines)
	}
	// Stale ids (dropped on stop) are ignored.
	m.onDebugResult(99, dbgResult{Result: "x"})
}

func TestDebugEventsDriveSessionState(t *testing.T) {
	m := Model{}
	m.onDebugEvent("session", `{"active":true,"name":"t"}`)
	if !m.dapSessionActive || m.debug.status != "Running" {
		t.Fatalf("after session: active=%v status=%q", m.dapSessionActive, m.debug.status)
	}
	m.debug.children["k"] = []dbgVar{{Name: "x"}}
	m.onDebugEvent("stopped", `{"reason":"breakpoint","thread":1}`)
	if !m.debug.snap.Stopped || m.debug.status != "Paused on breakpoint" || len(m.debug.children) != 0 {
		t.Errorf("after stopped: %+v status=%q", m.debug.snap, m.debug.status)
	}
	m.onDebugEvent("continued", `{}`)
	if m.debug.snap.Stopped || m.debug.status != "Running" {
		t.Errorf("after continued: stopped=%v status=%q", m.debug.snap.Stopped, m.debug.status)
	}
	m.onDebugEvent("output", `{"category":"stdout","output":"hi\n"}`)
	m.debugApplySnapshot(dbgSnapshot{Available: true})
	if m.dapSessionActive || m.debug.status != "" {
		t.Errorf("inactive snapshot must clear the session: active=%v status=%q", m.dapSessionActive, m.debug.status)
	}
	var texts []string
	for _, l := range m.debug.console.Lines() {
		texts = append(texts, l.Text)
	}
	joined := strings.Join(texts, "|")
	if !strings.Contains(joined, "hi") || !strings.Contains(joined, "session ended") {
		t.Errorf("console = %q", joined)
	}
}

type fakeCSI string

func (f fakeCSI) String() string { return string(f) }

func TestRouteDebugMsgShiftAltF5(t *testing.T) {
	m := Model{}
	if _, ok := m.routeDebugMsg(fakeCSI(shiftAltF5CSI)); !ok {
		t.Error("Shift+Alt+F5 CSI should be handled")
	}
	if _, ok := m.routeDebugMsg(fakeCSI("?CSI[1 2]?")); ok {
		t.Error("other CSI sequences must pass through")
	}
	if _, ok := m.routeDebugMsg(tea.KeyMsg{Type: tea.KeyF5, Alt: true}); ok {
		t.Error("key messages go through the keymap")
	}
	called := false
	cmd, ok := m.routeDebugMsg(debugMsg{apply: func(*Model) tea.Cmd { called = true; return nil }})
	if !ok || !called || cmd != nil {
		t.Error("debugMsg must run its closure")
	}
}

func TestStopStatus(t *testing.T) {
	if stopStatus("exception", "panic: boom\nmore") != "Paused on exception: panic: boom" {
		t.Error(stopStatus("exception", "panic: boom\nmore"))
	}
	if stopStatus("weird", "") != "Paused" {
		t.Error("unknown reason → Paused")
	}
}
