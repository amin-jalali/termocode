package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// K7: the debugger tools. These tests use fake session data, so the
// report, the waits and the gates are checked without nvim or dlv.

func k7Stopped() aiDbgState {
	return aiDbgState{
		Available: true, Active: true, Stopped: true, Name: "Go: Debug package",
		Reason: "breakpoint", Threads: 3,
		Thread: &aiDbgThread{ID: 1, Name: "main"},
		Frames: []dbgFrame{
			{ID: 1000, Name: "main.add", Path: "/w/main.go", Line: 8},
			{ID: 1001, Name: "main.main", Path: "/w/main.go", Line: 14},
			{ID: 1002, Name: "runtime.main", Path: "/usr/lib/go/src/runtime/proc.go", Line: 283},
		},
		FrameTotal: 7,
		Scopes: []aiDbgScope{
			{Name: "Locals", Vars: []aiDbgVar{
				{Name: "a", Type: "int", Value: "1"},
				{Name: "p", Type: "main.point", Value: "{X: 1, Y: 2}", Ref: 5,
					Children: []aiDbgVar{{Name: "X", Type: "int", Value: "1"}, {Name: "Y", Type: "int", Value: "2"}}},
				{Name: "long", Type: "string", Value: strings.Repeat("x", 900)},
			}, More: 4},
			{Name: "Globals", Expensive: true},
		},
		Watches: []aiDbgWatch{{Expr: "a + 1", Value: "2"}, {Expr: "nope", Error: "could not find symbol nope"}},
	}
}

// Spec: debug_state tells the model where the program stopped (file:line
// relative to the workspace, function), the top frames, the variables of
// the top frame with children (indented), caps, and the watch values.
func TestAIDebugStateReport(t *testing.T) {
	got := aiDebugStateReport(k7Stopped(), "/w", aiDbgExtra{})
	for _, want := range []string{
		`Debug session "Go: Debug package" is paused (breakpoint) in thread 1 "main".`,
		"Stopped at main.go:8 in main.add.",
		"Call stack (3 of 7 frames):",
		"  #0 main.add (main.go:8)",
		"  #2 runtime.main (/usr/lib/go/src/runtime/proc.go:283)",
		"Variables of main.add:",
		"  Locals:\n    a int = 1\n    p main.point = {X: 1, Y: 2}\n      X int = 1\n      Y int = 2",
		"    … 4 more",
		"  Globals: not loaded (expensive scope)",
		"Watch:\n  a + 1 = 2\n  nope: error: could not find symbol nope",
		"debug_control",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report has no %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, strings.Repeat("x", aiDebugValueWidth+1)) {
		t.Errorf("a long value is not cut:\n%s", got)
	}

	run := aiDbgState{Available: true, Active: true, Name: "srv"}
	if got := aiDebugStateReport(run, "/w", aiDbgExtra{}); !strings.Contains(got, `"srv" is running`) || !strings.Contains(got, "pause") {
		t.Errorf("running:\n%s", got)
	}
	exit := 3
	none := aiDbgState{Available: true, Exit: &exit}
	got = aiDebugStateReport(none, "/w", aiDbgExtra{Breakpoints: 2, Configs: []string{"Go: Debug package", "My app"},
		Output: []string{"result 3"}})
	for _, want := range []string{"No debug session is running.", "The last session ended with exit code 3.",
		"2 breakpoints", `Configurations: "Go: Debug package", "My app"`, "debug_start", "Program output (last lines):\nresult 3"} {
		if !strings.Contains(got, want) {
			t.Errorf("no session has no %q:\n%s", want, got)
		}
	}
	if got := aiDebugStateReport(aiDbgState{}, "/w", aiDbgExtra{}); !strings.Contains(got, "nvim-dap") {
		t.Errorf("not available:\n%s", got)
	}
}

// Spec: the waits of debug_start / debug_control end on a NEW stop (not
// the stop that was there before the step), on the session end, or —
// for a start — on an error before the session began.
func TestAIDebugDone(t *testing.T) {
	paused := aiDbgStatus{Available: true, Active: true, Stopped: true, Seq: 4, Frames: 3, Sessions: 1}
	stepping := paused
	stepping.Stopped = false
	again := paused
	again.Seq = 5
	noFrames := again
	noFrames.Frames = 0
	ended := aiDbgStatus{Available: true, Seq: 4, Sessions: 1}
	for _, c := range []struct {
		op     string
		before aiDbgStatus
		now    aiDbgStatus
		want   bool
		why    string
	}{
		{"step_over", paused, paused, false, "the old stop does not count"},
		{"step_over", paused, stepping, false, "still running"},
		{"step_over", paused, noFrames, false, "frames not loaded yet"},
		{"step_over", paused, again, true, "a new stop"},
		{"continue", paused, ended, true, "the program ended"},
		{"stop", paused, again, false, "stop waits for the end"},
		{"stop", paused, ended, true, "ended"},
		{"restart", paused, ended, false, "restart waits for the new stop"},
		{"restart", paused, again, true, "new stop after restart"},
		{"start", aiDbgStatus{Available: true, Sessions: 1}, aiDbgStatus{Available: true, Sessions: 1}, false, "not started yet"},
		{"start", aiDbgStatus{Available: true, Sessions: 1}, aiDbgStatus{Available: true, Sessions: 1, Msgs: 1}, true, "an error before the start"},
		{"start", aiDbgStatus{Available: true, Sessions: 1}, aiDbgStatus{Available: true, Sessions: 2}, true, "started and ended"},
		{"start", aiDbgStatus{Available: true, Sessions: 1}, aiDbgStatus{Available: true, Active: true, Sessions: 2}, false, "running"},
		{"start", aiDbgStatus{Available: true, Sessions: 1, Seq: 4}, aiDbgStatus{Available: true, Active: true, Stopped: true, Sessions: 2, Seq: 5, Frames: 2}, true, "stopped at a breakpoint"},
	} {
		if got := aiDebugDone(c.op, c.before, c.now); got != c.want {
			t.Errorf("%s (%s): done = %v, want %v", c.op, c.why, got, c.want)
		}
	}
}

// Spec: debug_start(path) builds a debug configuration from the file's
// language; Go debugs the file's package folder.
func TestAIDebugFileConfig(t *testing.T) {
	root := t.TempDir()
	for _, c := range []struct{ file, typ, program string }{
		{"cmd/app/main.go", "delve", "cmd/app"},
		{"tool.py", "python", "tool.py"},
		{"web/index.js", "pwa-node", "web/index.js"},
	} {
		abs := filepath.Join(root, c.file)
		cfg, err := aiDebugFileConfig(root, abs)
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		if cfg.Type != c.typ || cfg.Raw["program"] != filepath.Join(root, c.program) || !cfg.Debuggable() {
			t.Errorf("%s: %+v", c.file, cfg)
		}
		if !strings.Contains(cfg.Name, c.file) {
			t.Errorf("%s: name %q", c.file, cfg.Name)
		}
	}
	if cfg, _ := aiDebugFileConfig(root, filepath.Join(root, "main.go")); cfg.Raw["outputMode"] != "remote" {
		t.Errorf("Go program output does not go to the Debug Console: %+v", cfg)
	}
	_ = os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644)
	for _, c := range detectLaunchConfigs(root) {
		if c.Type == "delve" && c.Raw["outputMode"] != "remote" {
			t.Errorf("auto config %q: program output does not reach the Debug Console", c.Name)
		}
	}
	if cfg, _ := aiDebugFileConfig(root, filepath.Join(root, "x_test.go")); cfg.Raw["mode"] != "test" {
		t.Errorf("a _test.go file is not debugged in test mode: %+v", cfg)
	}
	if _, err := aiDebugFileConfig(root, filepath.Join(root, "notes.txt")); err == nil || !strings.Contains(err.Error(), "launch.json") {
		t.Errorf("unknown language: %v", err)
	}
}

// Spec: debug_start(config) finds a configuration by name (exact, then
// any case); a run-only (shell) config and an unknown name are clear
// errors that list the debug configurations.
func TestAIDebugFindConfig(t *testing.T) {
	list := []launchConfig{
		dapConfig("Go: Debug package", "delve", "${workspaceFolder}", nil),
		shellConfig("Go: Test", "go test ./..."),
	}
	if c, err := aiDebugFindConfig(list, "go: debug PACKAGE"); err != nil || c.Name != "Go: Debug package" {
		t.Errorf("case: %+v %v", c, err)
	}
	if _, err := aiDebugFindConfig(list, "Go: Test"); err == nil || !strings.Contains(err.Error(), "tasks_run") {
		t.Errorf("shell config: %v", err)
	}
	if _, err := aiDebugFindConfig(list, "nope"); err == nil || !strings.Contains(err.Error(), `"Go: Debug package"`) {
		t.Errorf("unknown: %v", err)
	}
}

// Spec: debug_breakpoint list shows each breakpoint as workspace path:line
// with its line text, condition / log message and whether the debugger
// confirmed it.
func TestAIDebugBreakpointReport(t *testing.T) {
	no := false
	store := map[string][]storedBreakpoint{
		"/w/main.go": {{Line: 8, Text: "s := a + b"}, {Line: 9, Text: "return s", Condition: "s > 2"}},
		"/w/b.go":    {{Line: 3, LogMessage: "x={x}"}},
	}
	live := map[string][]liveBreakpoint{"/w/main.go": {{storedBreakpoint: storedBreakpoint{Line: 9}, Verified: &no}}}
	got := aiDebugBreakpointReport(store, live, "/w")
	for _, want := range []string{"3 breakpoints:", "- b.go:3 log \"x={x}\"", "- main.go:8: s := a + b",
		"- main.go:9: return s if s > 2 (not confirmed by the debugger)"} {
		if !strings.Contains(got, want) {
			t.Errorf("list has no %q:\n%s", want, got)
		}
	}
	if got := aiDebugBreakpointReport(nil, nil, "/w"); !strings.Contains(got, "No breakpoints") {
		t.Errorf("empty: %q", got)
	}
}

// Spec: the gates. Reading is auto; setting and removing one breakpoint
// is auto (like F9); clearing many asks (ide); start, steps, continue,
// restart and evaluate ask (run); pause and stop ask (ide).
func TestAIDebugToolGates(t *testing.T) {
	call := func(context.Context, aiIDEFunc) (string, error) { return "", nil }
	tools := map[string]ai.Tool{}
	for _, tl := range aiDebugTools(t.TempDir(), call) {
		tools[tl.Name] = tl
	}
	gate := func(name string, args map[string]any) (bool, string) {
		tl, ok := tools[name]
		if !ok {
			t.Fatalf("no tool %s", name)
		}
		if tl.Gate != nil {
			return tl.Gate(args)
		}
		return tl.Mutating, tl.Class
	}
	for _, c := range []struct {
		name  string
		args  map[string]any
		mut   bool
		class string
	}{
		{"debug_state", nil, false, ""},
		{"debug_breakpoint", map[string]any{"op": "list"}, false, ""},
		{"debug_breakpoint", map[string]any{"op": "set"}, false, ""},
		{"debug_breakpoint", map[string]any{"op": "remove"}, false, ""},
		{"debug_breakpoint", map[string]any{"op": "clear"}, true, ai.ClassIDE},
		{"debug_start", nil, true, ai.ClassRun},
		{"debug_control", map[string]any{"op": "step_over"}, true, ai.ClassRun},
		{"debug_control", map[string]any{"op": "continue"}, true, ai.ClassRun},
		{"debug_control", map[string]any{"op": "restart"}, true, ai.ClassRun},
		{"debug_control", map[string]any{"op": "pause"}, true, ai.ClassIDE},
		{"debug_control", map[string]any{"op": "stop"}, true, ai.ClassIDE},
		{"debug_evaluate", map[string]any{"expr": "x"}, true, ai.ClassRun},
	} {
		mut, class := gate(c.name, c.args)
		if mut != c.mut || class != c.class {
			t.Errorf("%s %v: mutating %v class %q, want %v %q", c.name, c.args, mut, class, c.mut, c.class)
		}
	}
	v := tools["debug_breakpoint"].Validate
	for _, bad := range []map[string]any{{"op": "nope"}, {"op": "set"}, {"op": "remove", "path": "a.go"}} {
		if v(bad) == nil {
			t.Errorf("debug_breakpoint %v is not refused", bad)
		}
	}
	if err := tools["debug_control"].Validate(map[string]any{"op": "jump"}); err == nil {
		t.Error("debug_control op jump is not refused")
	}
	if err := tools["debug_evaluate"].Validate(map[string]any{"expr": " "}); err == nil {
		t.Error("debug_evaluate with no expr is not refused")
	}
}

// Spec: breakpoints follow a rename of their file or of a folder above it,
// and a delete drops them.
func TestBreakpointStoreFollowsPaths(t *testing.T) {
	store := map[string][]storedBreakpoint{
		"/w/a.go":       {{Line: 3}},
		"/w/pkg/b.go":   {{Line: 5}},
		"/w/pkgx/c.go":  {{Line: 7}},
		"/w/other/d.go": {{Line: 9}},
	}
	got, changed := moveBreakpointStore(store, "/w/pkg", "/w/lib")
	if !changed || got["/w/lib/b.go"] == nil || got["/w/pkg/b.go"] != nil || got["/w/pkgx/c.go"] == nil {
		t.Errorf("folder rename: %v", got)
	}
	got, changed = moveBreakpointStore(got, "/w/a.go", "/w/z.go")
	if !changed || got["/w/z.go"][0].Line != 3 || got["/w/a.go"] != nil {
		t.Errorf("file rename: %v", got)
	}
	if _, changed := moveBreakpointStore(got, "/w/none.go", "/w/x.go"); changed {
		t.Error("a rename of a file with no breakpoints changed the store")
	}
	got, changed = dropBreakpointStore(got, []string{"/w/lib", "/w/z.go"})
	if !changed || len(got) != 2 || got["/w/pkgx/c.go"] == nil || got["/w/other/d.go"] == nil {
		t.Errorf("delete: %v", got)
	}
	if len(store) != 4 || store["/w/a.go"] == nil {
		t.Errorf("the input store was changed: %v", store)
	}
}

// Spec: the program output in the report is its last lines.
func TestAITailLines(t *testing.T) {
	got := aiTailLines("a\r\nb\nc\nd\n", 2)
	if strings.Join(got, ",") != "c,d" {
		t.Errorf("got %q", got)
	}
}

// Spec: the system prompt tells the model how the debug tools fit.
func TestAIDebugSystemPromptHint(t *testing.T) {
	call := func(context.Context, aiIDEFunc) (string, error) { return "", nil }
	sys := ai.SystemPrompt("base", aiDebugTools(t.TempDir(), call), true)
	for _, want := range []string{"debug_breakpoint", "debug_start", "debug_state", "debug_control", "editor follows"} {
		if !strings.Contains(sys, want) {
			t.Errorf("system prompt has no %q:\n%s", want, sys)
		}
	}
}
