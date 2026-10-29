package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// The debugger AI tools (ai_debug_tools.go) on a fake debugger: the Lua
// side is a scripted function, so the waits, the stops, the refusals and
// the evaluate path run without nvim-dap or dlv.

type covDap struct {
	mu       sync.Mutex
	statuses []aiDbgStatus // ai_status answers; the last one repeats
	state    string        // ai_state answer (JSON)
	eval     string        // ai_eval answer (JSON)
	setBp    string        // set_bp answer ("" = ok)
	err      error         // every call fails
	calls    []string
}

func (d *covDap) lua(code string, args ...any) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return "", d.err
	}
	switch {
	case strings.Contains(code, "ai_status"):
		d.calls = append(d.calls, "status")
		if len(d.statuses) == 0 {
			return "", nil
		}
		st := d.statuses[0]
		if len(d.statuses) > 1 {
			d.statuses = d.statuses[1:]
		}
		b, _ := json.Marshal(st)
		return string(b), nil
	case strings.Contains(code, "ai_state"):
		d.calls = append(d.calls, "state")
		return d.state, nil
	case strings.Contains(code, "ai_eval"):
		d.calls = append(d.calls, "eval "+args[0].(string))
		return d.eval, nil
	case strings.Contains(code, "set_bp"):
		d.calls = append(d.calls, "set_bp")
		return d.setBp, nil
	}
	return "", nil
}

func covStoppedJSON(t *testing.T, path string) string {
	t.Helper()
	st := k7Stopped()
	st.Frames[0].Path = path
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A status that cannot be read is "not set up yet"; a Lua error passes.
func TestCovAIDebugStatusErrors(t *testing.T) {
	d := &covDap{}
	if _, err := aiDebugStatus(d.lua); err == nil || err.Error() != "the debugger is not set up yet" {
		t.Errorf("empty status: %v", err)
	}
	d.err = errors.New("nvim gone")
	if _, err := aiDebugStatus(d.lua); err == nil || err.Error() != "nvim gone" {
		t.Errorf("lua error: %v", err)
	}
	m := &Model{}
	call := covSyncCall(m)
	if _, _, err := aiDebugReport(context.Background(), call, d.lua, "/w", 1); err == nil {
		t.Error("report with a failing debugger")
	}
	d.err = nil
	d.state = "{not json"
	if _, _, err := aiDebugReport(context.Background(), call, d.lua, "/w", 9); err == nil || !strings.Contains(err.Error(), "bad debugger state") {
		t.Errorf("bad state: %v", err)
	}
	stopped := func(context.Context, aiIDEFunc) (string, error) { return "", errAIChatStopped }
	if _, _, err := aiDebugReport(context.Background(), stopped, d.lua, "/w", -1); err != errAIChatStopped {
		t.Errorf("stopped chat: %v", err)
	}
	if _, err := aiDebugLuaFn(context.Background(), call); err == nil || err.Error() != "the editor is not running" {
		t.Errorf("no editor: %v", err)
	}
}

// Without a session the report has the last output: the adapter's copy
// wins, else the console's stdout / stderr lines of the newest session.
func TestCovAIDebugReportOutput(t *testing.T) {
	m := &Model{}
	call := covSyncCall(m)
	dbg := m.ensureDebug()
	dbg.console.AppendOutput("stdout", "old run line\n")
	dbg.console.AppendOutput("console", "Starting new session\n")
	for i := 0; i < aiDebugOutputLines+5; i++ {
		dbg.console.AppendOutput("stdout", "line\n")
	}
	dbg.console.AppendOutput("stderr", "panic: boom\n")
	dbg.watches = append(dbg.watches, dbgWatch{Expr: "x"})
	d := &covDap{state: `{"available":true,"active":false}`}
	_, report, err := aiDebugReport(context.Background(), call, d.lua, "/w", 1)
	if err != nil || !strings.Contains(report, "panic: boom") || strings.Contains(report, "old run line") {
		t.Errorf("console output:\n%s %v", report, err)
	}
	d.state = `{"available":true,"active":false,"output":"adapter line 1\nadapter line 2\n"}`
	if _, report, _ = aiDebugReport(context.Background(), call, d.lua, "/w", 1); !strings.Contains(report, "adapter line 2") || strings.Contains(report, "panic: boom") {
		t.Errorf("adapter output:\n%s", report)
	}
}

// After start / control: a new stop, a session that did not start, a
// program that ran to its end, a session that ended, still running and
// running.
func TestCovAIDebugAfterHeads(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "main.go")
	_ = os.WriteFile(src, []byte("package main\n"), 0o644)
	m := &Model{}
	call := covSyncCall(m)
	stopped := covStoppedJSON(t, src)
	before := aiDbgStatus{Available: true, Seq: 1, Sessions: 1, Msgs: 1}
	cases := []struct {
		op    string
		now   aiDbgStatus
		state string
		want  string
	}{
		{"continue", aiDbgStatus{Active: true, Stopped: true, Seq: 2, Frames: 3}, stopped, "Paused at main.go:8; the editor shows the line."},
		{"start", aiDbgStatus{Sessions: 1, Msgs: 2, Msg: "could not launch: no main package"}, `{"active":false}`, "The debug session did not start: could not launch: no main package"},
		{"start", aiDbgStatus{Sessions: 1, Msgs: 1}, `{"active":false}`, "The debug session did not start; see the Debug Console."},
		{"start", aiDbgStatus{Sessions: 2, Msgs: 1}, `{"active":false}`, "The program ran to its end without stopping at a breakpoint."},
		{"stop", aiDbgStatus{}, `{"active":false}`, "The debug session ended."},
		{"continue", aiDbgStatus{Active: true, Seq: 1}, `{"active":true,"name":"x"}`, "Still running after 30ms (no breakpoint hit)."},
		{"continue", aiDbgStatus{Active: true, Stopped: true, Seq: 2, Frames: 1}, `{"active":true,"name":"x"}`, "Running."},
	}
	for i, c := range cases {
		d := &covDap{statuses: []aiDbgStatus{c.now}, state: c.state}
		wait := 30 * time.Millisecond
		if i == len(cases)-1 {
			wait = 0
		}
		out, err := aiDebugAfter(context.Background(), call, d.lua, root, c.op, before, wait)
		if err != nil || !strings.HasPrefix(out, c.want) {
			t.Errorf("%s %+v: %q %v", c.op, c.now, out, err)
		}
	}
	d := &covDap{statuses: []aiDbgStatus{{Active: true, Stopped: true, Seq: 2}}, state: `{"active":true,"stopped":true,"name":"x"}`}
	if out, _ := aiDebugAfter(context.Background(), call, d.lua, root, "continue", before, 0); !strings.HasPrefix(out, "Paused.") {
		t.Errorf("stop without frames: %q", out)
	}
	d = &covDap{err: errors.New("nvim gone")}
	if _, err := aiDebugAfter(context.Background(), call, d.lua, root, "continue", before, 10*time.Millisecond); err == nil {
		t.Error("a failing debugger gave no error")
	}
}

// Start refuses when the debugger is missing, a session runs, the config
// is unknown or nothing is open.
func TestCovAIDebugStartRefusals(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	m := &Model{}
	for _, c := range []struct {
		st   aiDbgStatus
		name string
		path string
		want string
	}{
		{aiDbgStatus{}, "", "", "not downloaded yet"},
		{aiDbgStatus{Available: true, Active: true, Name: "Go"}, "", "", `a debug session ("Go") is already running`},
		{aiDbgStatus{Available: true}, "nope", "", "nope"},
		{aiDbgStatus{Available: true}, "", filepath.Join(root, "x.unknownlang"), ""},
		{aiDbgStatus{Available: true}, "", "", "no debug configuration and no open file"},
	} {
		d := &covDap{statuses: []aiDbgStatus{c.st}}
		_, _, _, err := m.aiDebugStartNow(d.lua, root, c.name, c.path)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v %q %q: %v", c.st, c.name, c.path, err)
		}
	}
	if _, _, _, err := m.aiDebugStartNow((&covDap{}).lua, root, "", ""); err == nil {
		t.Error("start with no debugger state")
	}
}

// Control refuses steps while running and anything without a session;
// pause on a paused program says so; stop needs no editor work.
func TestCovAIDebugControlRefusals(t *testing.T) {
	m := &Model{}
	for _, c := range []struct {
		st   aiDbgStatus
		op   string
		want string
	}{
		{aiDbgStatus{}, "continue", "no debug session is running"},
		{aiDbgStatus{Active: true}, "step_over", "the program is running, not paused"},
	} {
		if _, _, _, err := m.aiDebugControlNow((&covDap{statuses: []aiDbgStatus{c.st}}).lua, c.op); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.op, err)
		}
	}
	if _, _, already, err := m.aiDebugControlNow((&covDap{statuses: []aiDbgStatus{{Active: true, Stopped: true}}}).lua, "pause"); err != nil || !already {
		t.Errorf("pause when paused: %v %v", already, err)
	}
	if _, _, _, err := m.aiDebugControlNow((&covDap{}).lua, "pause"); err == nil {
		t.Error("control with no debugger state")
	}
	if _, _, _, err := m.aiDebugControlNow((&covDap{statuses: []aiDbgStatus{{Active: true}}}).lua, "stop"); err != nil {
		t.Errorf("stop: %v", err)
	}
}

// Evaluate: only on a paused program; an error from the debugger and a
// bad answer reach the model; the line goes to the Debug Console.
func TestCovAIDebugEvaluate(t *testing.T) {
	m := &Model{}
	call := covSyncCall(m)
	ctx := context.Background()
	if _, err := aiDebugEvaluate(ctx, call, (&covDap{statuses: []aiDbgStatus{{}}}).lua, "x"); err == nil || !strings.Contains(err.Error(), "no debug session") {
		t.Errorf("no session: %v", err)
	}
	if _, err := aiDebugEvaluate(ctx, call, (&covDap{statuses: []aiDbgStatus{{Active: true}}}).lua, "x"); err == nil || !strings.Contains(err.Error(), "pause it first") {
		t.Errorf("running: %v", err)
	}
	if _, err := aiDebugEvaluate(ctx, call, (&covDap{}).lua, "x"); err == nil {
		t.Error("no debugger state")
	}
	paused := []aiDbgStatus{{Active: true, Stopped: true}}
	if _, err := aiDebugEvaluate(ctx, call, (&covDap{statuses: paused, eval: "{bad"}).lua, "x"); err == nil || !strings.Contains(err.Error(), "bad answer") {
		t.Errorf("bad answer: %v", err)
	}
	if _, err := aiDebugEvaluate(ctx, call, (&covDap{statuses: paused, eval: `{"error":"undefined: y"}`}).lua, "y"); err == nil || err.Error() != "evaluate failed: undefined: y" {
		t.Errorf("eval error: %v", err)
	}
	out, err := aiDebugEvaluate(ctx, call, (&covDap{statuses: paused, eval: `{"result":"{X: 1}","type":"main.point","children":[{"name":"X","type":"int","value":"1"}]}`}).lua, "p")
	if err != nil || !strings.HasPrefix(out, "p = {X: 1} (main.point)\n  X int = 1") {
		t.Errorf("eval: %q %v", out, err)
	}
	var lines []string
	for _, l := range m.ensureDebug().console.Lines() {
		lines = append(lines, l.Category+":"+l.Text)
	}
	if got := strings.Join(lines, "|"); !strings.Contains(got, "› y") || !strings.Contains(got, "stderr:undefined: y") || !strings.Contains(got, "result:{X: 1}") {
		t.Errorf("console = %q", got)
	}
}

// Breakpoints: set (plain, conditional, logpoint), a debugger that
// refuses, remove a missing one, clear one file and clear all.
func TestCovAIDebugBreakpointOps(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.go")
	m := &Model{}
	d := &covDap{}
	for _, c := range []struct{ cond, log, want string }{
		{"", "", "set a breakpoint at a.go:3."}, {"x > 1", "", "set a conditional breakpoint (if x > 1) at a.go:3"}, {"", "x={x}", "set a logpoint (x={x}) at a.go:3"},
	} {
		out, _, err := m.aiDebugBreakpoint(d.lua, root, "set", a, 3, c.cond, c.log)
		if err != nil || !strings.HasPrefix(out, c.want) {
			t.Errorf("set %+v: %q %v", c, out, err)
		}
	}
	d.setBp = "the debugger is not set up yet"
	if _, _, err := m.aiDebugBreakpoint(d.lua, root, "set", a, 3, "", ""); err == nil || err.Error() != d.setBp {
		t.Errorf("refused set: %v", err)
	}
	if _, _, err := m.aiDebugBreakpoint((&covDap{err: errors.New("gone")}).lua, root, "set", a, 3, "", ""); err == nil {
		t.Error("set with a failing debugger")
	}
	if _, _, err := m.aiDebugBreakpoint(d.lua, root, "remove", a, 9, "", ""); err == nil || !strings.Contains(err.Error(), "no breakpoint at a.go:9") {
		t.Errorf("remove missing: %v", err)
	}
	dbg := m.ensureDebug()
	dbg.bps = map[string][]storedBreakpoint{a: {{Line: 3}, {Line: 5}}, filepath.Join(root, "b.go"): {{Line: 1}}}
	if out, _, err := m.aiDebugBreakpoint(d.lua, root, "remove", a, 5, "", ""); err != nil || out != "removed the breakpoint at a.go:5" {
		t.Errorf("remove: %q %v", out, err)
	}
	if out, _, err := m.aiDebugBreakpoint(d.lua, root, "clear", a, 0, "", ""); err != nil || out != "removed 1 breakpoints in a.go" {
		t.Errorf("clear file: %q %v", out, err)
	}
	if out, _, err := m.aiDebugBreakpoint(d.lua, root, "clear", "", 0, "", ""); err != nil || out != "removed all 1 breakpoints" {
		t.Errorf("clear all: %q %v", out, err)
	}
	if _, _, err := m.aiDebugBreakpoint(d.lua, root, "jump", a, 1, "", ""); err == nil {
		t.Error("unknown op")
	}
	if got := aiDebugBreakpointReport(map[string][]storedBreakpoint{a: {{Line: 2, HitCondition: "3"}}}, nil, root); !strings.Contains(got, "a.go:2 hit 3") {
		t.Errorf("hit condition: %q", got)
	}
}

// The tool layer: without the editor every debug tool says so; a path
// must be an existing file; the card texts.
func TestCovAIDebugToolsWithoutEditor(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "dir"), 0o755)
	m := &Model{}
	tools := map[string]ai.Tool{}
	for _, tl := range aiDebugTools(root, covSyncCall(m)) {
		tools[tl.Name] = tl
	}
	ctx := context.Background()
	for name, a := range map[string]map[string]any{"debug_state": {"depth": 2}, "debug_start": {}, "debug_control": {"op": "pause"}, "debug_evaluate": {"expr": "x"}, "debug_breakpoint": {"op": "list"}} {
		if _, err := tools[name].Run(ctx, a); err == nil || err.Error() != "the editor is not running" {
			t.Errorf("%s without the editor: %v", name, err)
		}
	}
	if tools["debug_start"].Validate(map[string]any{"config": "a", "path": "b"}) == nil {
		t.Error("config and path together")
	}
	for a, want := range map[string]string{"config": "Start debugging: x", "path": "Start debugging x", "": "Start debugging"} {
		args := map[string]any{}
		if a != "" {
			args[a] = "x"
		}
		if got := tools["debug_start"].Describe(args); got != want {
			t.Errorf("start card %q = %q", a, got)
		}
	}
	bp := tools["debug_breakpoint"]
	for _, c := range []struct {
		a    map[string]any
		want string
	}{
		{map[string]any{"op": "set", "path": "a.go", "line": 3}, "Set breakpoint a.go:3"},
		{map[string]any{"op": "remove", "path": "a.go", "line": 3}, "Remove breakpoint a.go:3"},
		{map[string]any{"op": "clear", "path": "a.go"}, "Remove all breakpoints in a.go"},
		{map[string]any{"op": "clear"}, "Remove all breakpoints"},
		{map[string]any{"op": "list"}, "List breakpoints"},
	} {
		if got := bp.Describe(c.a); got != c.want {
			t.Errorf("breakpoint card = %q, want %q", got, c.want)
		}
	}
	if got := tools["debug_control"].Describe(map[string]any{"op": "step_over"}) + "|" + tools["debug_evaluate"].Describe(map[string]any{"expr": "x"}) + "|" + tools["debug_state"].Describe(nil); got != "Debug: step over|Evaluate in debugger: x|Read the debug state" {
		t.Errorf("cards = %q", got)
	}
}
