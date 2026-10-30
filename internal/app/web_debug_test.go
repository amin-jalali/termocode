package app

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// webTestDebug opens a workspace with a main.go and the laptop's debug
// state: two launch configurations (one runs a command) and no session.
func webTestDebug(t *testing.T, m *Model) (dir string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(base, "ws")
	webTestWrite(t, dir, "cmd/main.go", "package main\n\nfunc main() {\n\tprintln(1)\n}\n")
	m.ws = workspace.Workspace{Root: dir}
	d := newDebugState()
	d.configs = configCache{root: workspaceRoot(), checked: time.Now().Add(time.Hour), list: []launchConfig{
		{Name: "Debug main", Type: "delve", Request: "launch", Program: "${workspaceFolder}/cmd", Source: "launch.json"},
		{Name: "Serve", Type: "shell", Command: "make serve", Source: "launch.json"},
	}}
	m.debug = d
	return dir
}

// webTestPaused puts a stopped session in the debug state: a frame in
// the workspace and one outside it, a loaded scope and one to fetch.
func webTestPaused(m *Model, dir string) {
	d := m.ensureDebug()
	m.dapSessionActive = true
	d.status = "Paused on breakpoint"
	d.snap = dbgSnapshot{Available: true, Active: true, Stopped: true, Name: "Debug main", Thread: 1, Frame: 1001,
		Threads: []dbgThread{{ID: 1, Name: "main", Stopped: true}},
		Frames: []dbgFrame{{ID: 1001, Name: "main.main", Path: filepath.Join(dir, "cmd", "main.go"), Line: 4, Col: 2},
			{ID: 1002, Name: "runtime.main", Path: "/usr/lib/go/src/runtime/proc.go", Line: 283}},
		Scopes: []dbgScope{{Name: "Locals", Ref: 5, Loaded: true, Variables: []dbgVar{{Name: "user", Value: "{Name: \"ann\"}", Type: "User", Ref: 7}, {Name: "n", Value: "3", Type: "int"}}},
			{Name: "Globals", Ref: 6, Expensive: true}},
	}
}

// webTestFakeDebug swaps the nvim side of debugging for fakes: starts,
// runs, steps, breakpoint sets and children requests are recorded.
type webTestDebugLog struct {
	starts []string
	runs   []string
	steps  []string
	asks   []string
}

func webTestFakeDebug(t *testing.T) *webTestDebugLog {
	t.Helper()
	l := &webTestDebugLog{}
	oldStart, oldRun, oldStep, oldBp, oldAsk := webDebugStartCfg, webDebugRunCmd, webDebugStep, webDebugSetBp, webDebugAsk
	webDebugStartCfg = func(m *Model, cfg launchConfig) (tea.Cmd, error) { l.starts = append(l.starts, cfg.Name); return nil, nil }
	webDebugRunCmd = func(m *Model, cwd, cmdline string) { l.runs = append(l.runs, cmdline) }
	webDebugStep = func(m *Model, op string) tea.Cmd { l.steps = append(l.steps, op); return nil }
	webDebugSetBp = func(m *Model, abs string, line int, cond, hit, logMsg string) error {
		d := m.ensureDebug()
		next := map[string][]storedBreakpoint{}
		for p, bps := range d.bps {
			next[p] = bps
		}
		next[abs] = sortBreakpoints(append(append([]storedBreakpoint{}, next[abs]...), storedBreakpoint{Line: line, Text: "println(1)", Condition: cond, HitCondition: hit, LogMessage: logMsg}))
		d.bps = next
		return nil
	}
	webDebugAsk = func(m *Model, key string, ref int) {
		l.asks = append(l.asks, key)
		m.ensureDebug().inflight[key] = true
	}
	t.Cleanup(func() {
		webDebugStartCfg, webDebugRunCmd, webDebugStep, webDebugSetBp, webDebugAsk = oldStart, oldRun, oldStep, oldBp, oldAsk
	})
	return l
}

func webTestDebugSnap(t *testing.T, frames []map[string]any) (webproto.DebugSnap, int) {
	t.Helper()
	var s webproto.DebugSnap
	n := 0
	for _, f := range frames {
		if f["topic"] == "debug" {
			s = webproto.DebugSnap{}
			webTestData(t, f, &s)
			n++
		}
	}
	return s, n
}

// Spec (WEBP-4): the debug topic is the laptop's session with workspace
// paths: a frame outside the workspace has only its file's name, the
// breakpoints of another project are left out; the configurations, the
// selected one and the Debug Console's newest lines come along.
func TestDebugTopicSnapshot(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	dir := webTestDebug(t, &m)
	webTestFakeDebug(t)
	webTestPaused(&m, dir)
	d := m.debug
	d.bps = map[string][]storedBreakpoint{
		filepath.Join(dir, "cmd", "main.go"): {{Line: 4, Text: "println(1)", Condition: "n > 2"}},
		"/elsewhere/other.go":                {{Line: 9}},
	}
	yes := true
	d.liveBps = map[string][]liveBreakpoint{filepath.Join(dir, "cmd", "main.go"): {{storedBreakpoint: storedBreakpoint{Line: 4}, Verified: &yes}}}
	d.watches = []dbgWatch{{Expr: "n * 2", Value: "6"}}
	d.console.AppendOutput("stdout", "hello\n")
	c := webTestSub(t, &m, "debug")
	s, _ := webTestDebugSnap(t, webTestNow(t, c))
	if !s.Active || !s.Stopped || s.Status != "Paused on breakpoint" || s.Selected != "Debug main" || len(s.Configs) != 2 || !s.Configs[0].Debug || s.Configs[1].Debug {
		t.Fatalf("snap %+v", s)
	}
	if f := s.Frames; len(f) != 2 || f[0].Path != "cmd/main.go" || f[0].Line != 4 || f[1].Path != "" || f[1].File != "proc.go" || !f[1].Outside {
		t.Fatalf("frames %+v", f)
	}
	if sc := s.Scopes; len(sc) != 2 || sc[0].Key != "s:Locals" || len(sc[0].Vars) != 2 || sc[0].Vars[0].Ref != 7 || !sc[1].Expensive || len(sc[1].Vars) != 0 {
		t.Fatalf("scopes %+v", sc)
	}
	if b := s.Breakpoints; len(b) != 1 || b[0] != (webproto.DebugBreakpoint{Path: "cmd/main.go", Line: 4, Text: "println(1)", Condition: "n > 2", Verified: "yes"}) {
		t.Fatalf("breakpoints %+v", b)
	}
	if w := s.Watches; len(w) != 1 || w[0].Value != "6" {
		t.Fatalf("watches %+v", w)
	}
	if l := s.Console; len(l) != 1 || l[0] != (webproto.DebugLine{Cat: "stdout", Text: "hello"}) {
		t.Fatalf("console %+v", l)
	}
	d.console.AppendOutput("stderr", "boom\n")
	m.webAfterUpdate(nil)
	s, n := webTestDebugSnap(t, webTestNow(t, c))
	if n != 1 || len(s.Console) != 2 || s.Console[1].Cat != "stderr" {
		t.Fatalf("after output: %d %+v", n, s.Console)
	}
	m.webAfterUpdate(nil)
	if _, n := webTestDebugSnap(t, webTestNow(t, c)); n != 0 {
		t.Fatalf("%d frames without a change", n)
	}
}

// Spec (WEBP-4): a variable the page opens has its children fetched
// (the page's open rows, not the laptop's view); they come in the topic
// by key; after the next stop they are fetched again; closing a row
// forgets its open children.
func TestDebugExpandFetchesChildren(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	dir := webTestDebug(t, &m)
	l := webTestFakeDebug(t)
	webTestPaused(&m, dir)
	c := webTestSub(t, &m, "debug")
	webTestNow(t, c)
	if f, _ := webTestCall(t, &m, c, webproto.OpDebugExpand, `{"key":"s:Locals/user","open":true}`); f["t"] != "res" {
		t.Fatalf("expand: %v", f)
	}
	if strings.Join(l.asks, ",") != "s:Locals/user" {
		t.Fatalf("asked %v", l.asks)
	}
	if m.debug.open["s:Locals/user"] {
		t.Fatal("the laptop's view opened the row")
	}
	d := m.debug
	delete(d.inflight, "s:Locals/user")
	d.children["s:Locals/user"] = []dbgVar{{Name: "Name", Value: `"ann"`, Type: "string"}}
	m.webAfterUpdate(nil)
	s, _ := webTestDebugSnap(t, webTestNow(t, c))
	if len(s.Children) != 1 || s.Children[0].Key != "s:Locals/user" || s.Children[0].Vars[0].Value != `"ann"` || strings.Join(s.Open, ",") != "s:Locals/user" {
		t.Fatalf("children %+v open %v", s.Children, s.Open)
	}
	d.dropChildren() // the next stop
	m.webAfterUpdate(nil)
	if strings.Join(l.asks, ",") != "s:Locals/user,s:Locals/user" {
		t.Fatalf("after a stop asked %v", l.asks)
	}
	webTestCall(t, &m, c, webproto.OpDebugExpand, `{"key":"s:Globals","open":true}`)
	if l.asks[len(l.asks)-1] != "s:Globals" {
		t.Fatalf("an expensive scope opened: asked %v", l.asks)
	}
	webTestCall(t, &m, c, webproto.OpDebugExpand, `{"key":"s:Locals/user","open":false}`)
	if m.web.run.debug.open["s:Locals/user"] {
		t.Fatal("still open")
	}
}

// Spec (WEBP-4): debug.start starts a configuration by name (it becomes
// the selected one); a command configuration only runs (debug.run, in a
// laptop terminal, its own command); steps need a session, and the
// stepping ones a paused program; everything is audited.
func TestDebugStartRunAndSteps(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	webTestDebug(t, &m)
	l := webTestFakeDebug(t)
	c := webTestSub(t, &m)
	if f, _ := webTestCall(t, &m, c, webproto.OpDebugStart, `{"config":"Nope"}`); webTestErr(f) != "not_found" {
		t.Fatalf("unknown config: %v", f)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpDebugStart, `{"config":"Serve"}`); webTestErr(f) != "conflict" {
		t.Fatalf("start a command config: %v", f)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpDebugStart, `{}`); f["t"] != "res" || strings.Join(l.starts, ",") != "Debug main" {
		t.Fatalf("start: %v %v", f, l.starts)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpDebugRun, `{"config":"Serve"}`); f["t"] != "res" || strings.Join(l.runs, ",") != "make serve" {
		t.Fatalf("run: %v %v", f, l.runs)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpDebugControl, `{"op":"step_over"}`); webTestErr(f) != "conflict" {
		t.Fatalf("step without a session: %v", f)
	}
	m.dapSessionActive = true
	if f, _ := webTestCall(t, &m, c, webproto.OpDebugControl, `{"op":"step_over"}`); webTestErr(f) != "conflict" {
		t.Fatalf("step while running: %v", f)
	}
	webTestCall(t, &m, c, webproto.OpDebugControl, `{"op":"pause"}`)
	m.debug.snap.Stopped = true
	webTestCall(t, &m, c, webproto.OpDebugControl, `{"op":"step_over"}`)
	webTestCall(t, &m, c, webproto.OpDebugControl, `{"op":"stop"}`)
	if strings.Join(l.steps, ",") != "pause,step_over,stop" {
		t.Fatalf("steps %v", l.steps)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpDebugSelect, `{"config":"Serve"}`); f["t"] != "res" || m.debug.selected != "Serve" {
		t.Fatalf("select: %v %q", f, m.debug.selected)
	}
	a := webTestAudit(&m)
	if !strings.Contains(a, "debug.start  Debug main  by=web:phone  → ok") || !strings.Contains(a, "debug.run  Serve  by=web:phone  → ok") {
		t.Errorf("audit:\n%s", a)
	}
}

// Spec (WEBP-4): breakpoints by workspace path and line: toggle sets one
// (and on a line with one removes it), set takes a condition, remove of
// a line without one is not found, clear empties a file or everything;
// a path outside the workspace is refused.
func TestDebugBreakpoints(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir := webTestDebug(t, &m)
	webTestFakeDebug(t)
	c := webTestSub(t, &m)
	main := filepath.Join(dir, "cmd", "main.go")
	lines := func() string {
		var out []string
		for p, bps := range m.debug.bps {
			for _, b := range bps {
				out = append(out, filepath.Base(p)+":"+string(rune('0'+b.Line))+b.Condition)
			}
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	webTestCall(t, &m, c, webproto.OpDebugBp, `{"op":"toggle","path":"cmd/main.go","line":4}`)
	webTestCall(t, &m, c, webproto.OpDebugBp, `{"op":"set","path":"cmd/main.go","line":3,"condition":"x"}`)
	if got := lines(); got != "main.go:3x,main.go:4" {
		t.Fatalf("after set: %s", got)
	}
	webTestCall(t, &m, c, webproto.OpDebugBp, `{"op":"toggle","path":"cmd/main.go","line":4}`)
	if got := lines(); got != "main.go:3x" {
		t.Fatalf("after the second toggle: %s", got)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpDebugBp, `{"op":"remove","path":"cmd/main.go","line":5}`); webTestErr(f) != "not_found" {
		t.Fatalf("remove nothing: %v", f)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpDebugBp, `{"op":"toggle","path":"cmd/nope.go","line":1}`); webTestErr(f) != "not_found" {
		t.Fatalf("a missing file: %v", f)
	}
	webTestCall(t, &m, c, webproto.OpDebugBp, `{"op":"clear","path":"cmd/main.go"}`)
	if got := lines(); got != "" || len(m.debug.bps[main]) != 0 {
		t.Fatalf("after clear: %s", got)
	}
	webTestCall(t, &m, c, webproto.OpDebugBp, `{"op":"toggle","path":"cmd/main.go","line":4}`)
	webTestCall(t, &m, c, webproto.OpDebugBp, `{"op":"clear"}`)
	if got := lines(); got != "" {
		t.Fatalf("after clear all: %s", got)
	}
	if a := webTestAudit(&m); !strings.Contains(a, "debug.bp  set cmd/main.go:4  by=web:phone  → ok") || !strings.Contains(a, "debug.bp  all (1)") {
		t.Errorf("audit:\n%s", a)
	}
}

// Spec (WEBP-4): watches are added and removed by their text; a Debug
// Console line needs a session and goes to the console as the laptop's
// own input line; the audit keeps the length, never the expression; a
// frame not on the stack is not found.
func TestDebugWatchEvalFrame(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir := webTestDebug(t, &m)
	webTestFakeDebug(t)
	c := webTestSub(t, &m)
	webTestCall(t, &m, c, webproto.OpDebugWatch, `{"op":"add","expr":"len(secretItems)"}`)
	if len(m.debug.watches) != 1 || m.debug.watches[0].Expr != "len(secretItems)" {
		t.Fatalf("watches %+v", m.debug.watches)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpDebugWatch, `{"op":"remove","expr":"nope"}`); webTestErr(f) != "not_found" {
		t.Fatalf("remove unknown: %v", f)
	}
	webTestCall(t, &m, c, webproto.OpDebugWatch, `{"op":"remove","expr":"len(secretItems)"}`)
	if len(m.debug.watches) != 0 {
		t.Fatalf("watches %+v", m.debug.watches)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpDebugEval, `{"expr":"user.Name"}`); webTestErr(f) != "conflict" {
		t.Fatalf("eval without a session: %v", f)
	}
	webTestPaused(&m, dir)
	webTestCall(t, &m, c, webproto.OpDebugEval, `{"expr":"user.Name"}`)
	if l := m.debug.console.Lines(); len(l) == 0 || l[len(l)-1].Category != "input" || l[len(l)-1].Text != "› user.Name" {
		t.Fatalf("console %+v", l)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpDebugFrame, `{"id":9}`); webTestErr(f) != "not_found" {
		t.Fatalf("frame 9: %v", f)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpDebugFrame, `{"id":1002}`); f["t"] != "res" {
		t.Fatalf("frame: %v", f)
	}
	a := webTestAudit(&m)
	if strings.Contains(a, "secretItems") || strings.Contains(a, "user.Name") || !strings.Contains(a, "debug.eval  9 chars") {
		t.Errorf("audit:\n%s", a)
	}
}

// Spec (WEBP-4): the debug replay golden: a session that stops at a
// breakpoint, prints and ends replays to the final snapshot.
func TestDebugReplayGolden(t *testing.T) {
	m := aiTestModel(t)
	h := webTestHub(t, &m)
	dir := webTestDebug(t, &m)
	webTestFakeDebug(t)
	c := webReplayRecord(t, &m, h, "debug", func() {
		webTestPaused(&m, dir)
		m.webAfterUpdate(nil)
		m.debug.console.AppendOutput("stdout", "1\n")
		m.webAfterUpdate(nil)
		m.debugApplySnapshot(dbgSnapshot{Available: true})
		m.webAfterUpdate(nil)
	})
	webReplayCheck(t, h.topics["debug"], c)
	// The workspace path is the test's temp dir: the golden keeps the
	// frames' workspace paths, never an absolute one.
	webReplayGolden(t, webReplayPath("debug", "stop_print_end"), c)
}
