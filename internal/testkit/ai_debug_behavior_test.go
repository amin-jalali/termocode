package testkit

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// K7: the agent drives the debugger (nvim-dap) with debug_breakpoint,
// debug_state, debug_start, debug_control and debug_evaluate.
//
// The sandbox has no nvim-dap checkout, so each test sets it up with the
// "plugin:nvim-dap=<dir>" dev action: the real nvim-dap when it is
// downloaded on this machine, else a small fake (testdata/fake-nvim-dap)
// that keeps breakpoints only. Breakpoint rows run with either; a real
// session needs the real nvim-dap, dlv and go, and skips without them.

// realNvimDap is the downloaded nvim-dap, looked up before the sandbox
// moves HOME ("" when there is none).
var realNvimDap = func() string {
	var roots []string
	if v := os.Getenv("TERMOCODE_PLUGINS_DIR"); v != "" {
		roots = append(roots, v)
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		roots = append(roots, filepath.Join(v, "termocode", "plugins"))
	}
	if h, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(h, ".local", "share", "termocode", "plugins"))
	}
	for _, r := range roots {
		p := filepath.Join(r, "nvim-dap")
		if _, err := os.Stat(filepath.Join(p, "lua", "dap.lua")); err == nil {
			return p
		}
	}
	return ""
}()

// fakeNvimDap is the test double (absolute: the driver changes the cwd).
var fakeNvimDap = func() string {
	p, _ := filepath.Abs(filepath.Join("testdata", "fake-nvim-dap"))
	return p
}()

// realGoPath / realGoModCache are read before the sandbox moves HOME, so a
// pinned GOTOOLCHAIN is found in the module cache.
var realGoPath, realGoModCache = func() (string, string) {
	gp := os.Getenv("GOPATH")
	if gp == "" {
		if h, err := os.UserHomeDir(); err == nil {
			gp = filepath.Join(h, "go")
		}
	}
	mc := os.Getenv("GOMODCACHE")
	if mc == "" && gp != "" {
		mc = filepath.Join(strings.Split(gp, string(os.PathListSeparator))[0], "pkg", "mod")
	}
	return gp, mc
}()

// withDap sets nvim-dap up in the running app (real when real is set and
// it is downloaded, else the fake).
func withDap(d *Driver, real bool) {
	d.T.Helper()
	dir := fakeNvimDap
	if real || realNvimDap != "" {
		dir = realNvimDap
	}
	if dir == "" {
		d.T.Skip("nvim-dap is not downloaded on this machine (~/.local/share/termocode/plugins/nvim-dap): no real debug session")
	}
	d.Action("plugin:nvim-dap=" + dir)
	if !eventually(d, func() bool {
		return lua(d, `return (_G._termocode_dap and _G._termocode_dap.available and _G._termocode_dap.available()) and 'y' or ''`) == "y"
	}) {
		d.T.Fatalf("nvim-dap did not load from %s", dir)
	}
}

// signsAt is the dap breakpoint signs of the workspace file rel.
func signsAt(d *Driver, rel string) string {
	d.T.Helper()
	return lua(d, `local p = ...
		local b = vim.fn.bufnr(p)
		if b < 1 then return '' end
		local out = {}
		for _, s in ipairs(vim.fn.sign_getplaced(b, { group = 'dap_breakpoints' })[1].signs) do
			table.insert(out, s.name .. '@' .. s.lnum)
		end
		return table.concat(out, ',')`, filepath.Join(d.Dir, rel))
}

// toggleBreakpoint sets a breakpoint on the cursor line of the open file
// the way F9 does.
func toggleBreakpoint(d *Driver, line int) {
	d.T.Helper()
	lua(d, `vim.api.nvim_win_set_cursor(0, { ..., 0 }); return ''`, line)
	d.Action("palette:debug-toggle-breakpoint")
}

// storedBreakpoints is the persisted breakpoints.json of the sandbox.
func storedBreakpoints(d *Driver) string {
	b, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "breakpoints.json"))
	return string(b)
}

// K7 rows of TestAIToolsTable.
var aiDebugToolCases = []aiToolCase{
	{
		name: "debug_breakpoint set shows in the gutter and the Run view and is saved",
		call: toolCall{"debug_breakpoint", map[string]any{"op": "set", "path": "a.txt", "line": 2}},
		setup: func(d *Driver) {
			withDap(d, false)
			d.Action("palette:debug-view")
		},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "set a breakpoint at a.txt:2: bravo") {
				t.Errorf("results %q", r)
			}
			if got := signsAt(d, "a.txt"); got != "DapBreakpoint@2" {
				t.Errorf("gutter signs %q", got)
			}
			if !eventually(d, func() bool { return strings.Contains(screen(d), "a.txt:2") }) {
				t.Errorf("the Run view does not list a.txt:2:\n%s", screen(d))
			}
			if s := storedBreakpoints(d); !strings.Contains(s, filepath.Join(d.Dir, "a.txt")) || !strings.Contains(s, `"line": 2`) {
				t.Errorf("not saved: %s", s)
			}
		},
	},
	{
		name: "debug_breakpoint set with a condition in a file that is not open",
		call: toolCall{"debug_breakpoint", map[string]any{"op": "set", "path": "b.txt", "line": 3, "condition": "x > 1"}},
		setup: func(d *Driver) {
			withDap(d, false)
			d.Action("palette:debug-view")
		},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "conditional breakpoint (if x > 1) at b.txt:3") {
				t.Errorf("results %q", r)
			}
			if got := signsAt(d, "b.txt"); got != "DapBreakpointCondition@3" {
				t.Errorf("signs %q", got)
			}
			if !eventually(d, func() bool { return strings.Contains(screen(d), "if x > 1") }) {
				t.Errorf("the Run view does not show the condition:\n%s", screen(d))
			}
			if got, _ := tabs(d); strings.Contains(strings.Join(got, " "), "b.txt") {
				t.Errorf("a tab opened for b.txt: %v", got)
			}
		},
	},
	{
		name:  "debug_breakpoint set outside the file is a clear error",
		call:  toolCall{"debug_breakpoint", map[string]any{"op": "set", "path": "a.txt", "line": 40}},
		setup: func(d *Driver) { withDap(d, false) },
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "outside the file") {
				t.Errorf("results %q", r)
			}
		},
	},
	{
		name: "debug_breakpoint list shows a breakpoint the user set with F9",
		call: toolCall{"debug_breakpoint", map[string]any{"op": "list"}},
		setup: func(d *Driver) {
			withDap(d, false)
			toggleBreakpoint(d, 3)
		},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "1 breakpoints:\n- a.txt:3: charlie") {
				t.Errorf("results %q", r)
			}
		},
	},
	{
		name: "debug_breakpoint remove takes it out of the gutter",
		call: toolCall{"debug_breakpoint", map[string]any{"op": "remove", "path": "a.txt", "line": 1}},
		setup: func(d *Driver) {
			withDap(d, false)
			toggleBreakpoint(d, 1)
			if !eventually(d, func() bool { return signsAt(d, "a.txt") == "DapBreakpoint@1" }) {
				d.T.Fatalf("F9 placed no sign: %q", signsAt(d, "a.txt"))
			}
		},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "removed the breakpoint at a.txt:1") {
				t.Errorf("results %q", r)
			}
			if got := signsAt(d, "a.txt"); got != "" {
				t.Errorf("signs left: %q", got)
			}
		},
	},
	{
		name:    "debug_breakpoint clear asks, then removes all",
		call:    toolCall{"debug_breakpoint", map[string]any{"op": "clear"}},
		confirm: true,
		setup: func(d *Driver) {
			withDap(d, false)
			toggleBreakpoint(d, 1)
			toggleBreakpoint(d, 2)
		},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "removed all 2 breakpoints") {
				t.Errorf("results %q", r)
			}
			if got := signsAt(d, "a.txt"); got != "" {
				t.Errorf("signs left: %q", got)
			}
		},
	},
	{
		name:  "debug_state with no session lists what to do",
		call:  toolCall{"debug_state", map[string]any{}},
		setup: func(d *Driver) { withDap(d, false) },
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "No debug session is running.") || !strings.Contains(r[0], "debug_start") {
				t.Errorf("results %q", r)
			}
		},
	},
	{
		name:    "debug_control with no session is a clear error",
		call:    toolCall{"debug_control", map[string]any{"op": "step_over"}},
		confirm: true,
		setup:   func(d *Driver) { withDap(d, false) },
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "no debug session is running") {
				t.Errorf("results %q", r)
			}
		},
	},
}

func init() { aiToolCases = append(aiToolCases, aiDebugToolCases...) }

// Spec: breakpoints follow their file. When the agent renames b.txt (open
// in a tab) and notes/ (todo.md is not open), the breakpoints move with
// them: the list, the gutter and breakpoints.json have the new paths.
// When it deletes a file, its breakpoints are gone.
func TestAIDebugBreakpointsFollowRenameAndDelete(t *testing.T) {
	s, srv := fakeToolModel(t,
		toolCall{"debug_breakpoint", map[string]any{"op": "set", "path": "b.txt", "line": 2}},
		toolCall{"debug_breakpoint", map[string]any{"op": "set", "path": "notes/todo.md", "line": 3}},
		toolCall{"debug_breakpoint", map[string]any{"op": "set", "path": "c.txt", "line": 1}},
		toolCall{"rename", map[string]any{"from": "b.txt", "to": "b2.txt"}},
		toolCall{"rename", map[string]any{"from": "notes", "to": "memo"}},
		toolCall{"delete", map[string]any{"path": "c.txt"}},
		toolCall{"debug_breakpoint", map[string]any{"op": "list"}},
	)
	d := aiDriver(t, srv.URL, nil, false, "fileWrite")
	withDap(d, false)
	open(d, "b.txt")
	askAI(d, "move them")
	if !d.WaitForText("fake-done", 15*time.Second) {
		t.Fatalf("no final answer:\n%s\nresults %q", screen(d), s.toolResults())
	}
	r := s.toolResults()
	if len(r) != 7 {
		t.Fatalf("results %q", r)
	}
	list := r[6]
	for _, want := range []string{"2 breakpoints:", "- b2.txt:2: two", "- memo/todo.md:3: - milk"} {
		if !strings.Contains(list, want) {
			t.Errorf("list has no %q:\n%s", want, list)
		}
	}
	for _, gone := range []string{"b.txt:", "notes/", "c.txt"} {
		if strings.Contains(list, "- "+gone) {
			t.Errorf("list still has %q:\n%s", gone, list)
		}
	}
	if got := signsAt(d, "b2.txt"); got != "DapBreakpoint@2" {
		t.Errorf("gutter of b2.txt: %q", got)
	}
	stored := storedBreakpoints(d)
	if !strings.Contains(stored, filepath.Join(d.Dir, "b2.txt")) || !strings.Contains(stored, filepath.Join(d.Dir, "memo", "todo.md")) ||
		strings.Contains(stored, filepath.Join(d.Dir, "b.txt")) || strings.Contains(stored, filepath.Join(d.Dir, "notes")) ||
		strings.Contains(stored, filepath.Join(d.Dir, "c.txt")) {
		t.Errorf("breakpoints.json: %s", stored)
	}
}

// k7Main is a tiny Go program; line 8 is "s := a + b" in add.
const k7Main = `package main

import "fmt"

type point struct{ X, Y int }

func add(a, b int) int {
	s := a + b
	return s
}

func main() {
	p := point{X: 1, Y: 2}
	x := add(p.X, p.Y)
	fmt.Println("result", x)
}
`

// leftovers lists the processes started for this workspace (dlv, the
// debug binary) that are still alive. Linux only (it reads /proc); it
// matches the workspace path, never a name alone.
func leftovers(dir string) map[int]string {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	out := map[int]string{}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		cmd, _ := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		cwd, _ := os.Readlink(filepath.Join("/proc", e.Name(), "cwd"))
		line := strings.TrimSpace(strings.ReplaceAll(string(cmd), "\x00", " "))
		if !strings.Contains(line, dir) && cwd != dir && !strings.HasPrefix(cwd, dir+string(os.PathSeparator)) {
			continue
		}
		if strings.Contains(line, "dlv") || strings.Contains(line, "__debug_bin") {
			out[pid] = line
		}
	}
	return out
}

// Spec: Given a Go program, the real nvim-dap and dlv, when the model sets
// a breakpoint on line 8, starts debugging main.go, reads the state, steps
// over and stops, then the session stops at main.go:8 in main.add (the
// editor shows that line, the user's focus stays in the chat), the state
// lists the locals (a = 1, b = 2), step_over moves to line 9, and stop
// ends the session with no dlv left running.
func TestAIDebugGoSession(t *testing.T) {
	s, d := goDebugAI(t, k7Main,
		toolCall{"debug_breakpoint", map[string]any{"op": "set", "path": "main.go", "line": 8}},
		toolCall{"debug_start", map[string]any{"path": "main.go", "wait_ms": 90000}},
		toolCall{"debug_state", map[string]any{}},
		toolCall{"debug_evaluate", map[string]any{"expr": "a + b"}},
		toolCall{"debug_control", map[string]any{"op": "step_over"}},
		toolCall{"debug_control", map[string]any{"op": "stop"}},
		toolCall{"debug_state", map[string]any{}},
	)
	r := s.toolResults()
	if !strings.Contains(r[0], "set a breakpoint at main.go:8") {
		t.Errorf("set: %q", r[0])
	}
	checkGoSession(t, d, r)
}

// goDebugAI starts the app on a Go workspace (main is the main.go text)
// with a fake model that makes calls; "run" and "ide" are auto-approved.
// It skips without the real nvim-dap, dlv or go, and kills (and reports)
// any dlv or debug binary still alive at the end.
func goDebugAI(t *testing.T, main string, calls ...toolCall) (*aiScript, *Driver) {
	t.Helper()
	if realNvimDap == "" {
		t.Skip("nvim-dap is not downloaded on this machine: no real debug session (breakpoint rows still run with a fake)")
	}
	if _, err := exec.LookPath("dlv"); err != nil {
		t.Skip("dlv is not on PATH: the Go debug session can not run (install: go install github.com/go-delve/delve/cmd/dlv@latest)")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH")
	}
	// A dlv older than the Go toolchain reads no locals (dlv 1.25 with Go
	// 1.25 / 1.26); pin the toolchain it builds the program with, e.g.
	// TERMOCODE_DLV_GOTOOLCHAIN=go1.24.6. go test sets GOROOT for its
	// children; drop it so the pinned toolchain is used.
	if tc := os.Getenv("TERMOCODE_DLV_GOTOOLCHAIN"); tc != "" {
		t.Setenv("GOTOOLCHAIN", tc)
		t.Setenv("GOROOT", "")
	} else {
		t.Setenv("GOTOOLCHAIN", "local")
	}
	if realGoCache != "" {
		t.Setenv("GOCACHE", realGoCache)
	}
	if realGoPath != "" {
		t.Setenv("GOPATH", realGoPath)
		t.Setenv("GOMODCACHE", realGoModCache)
	}
	t.Setenv("GOFLAGS", "")
	s, srv := fakeToolModel(t, calls...)
	files := map[string]string{"go.mod": "module k7\n\ngo 1.21\n", "main.go": main, "a.txt": "alpha\n"}
	d := aiDriver(t, srv.URL, files, false, "run", "ide")
	t.Cleanup(func() { // no process may outlive the test
		for pid, line := range leftovers(d.Dir) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Errorf("killed a process still running after the test: %s", line)
		}
	})
	withDap(d, true)
	// A dlv older than the Go toolchain refuses to build without this.
	lua(d, `local a = require('dap').adapters.delve; if a then table.insert(a.executable.args, '--check-go-version=false') end; return ''`)
	open(d, "a.txt")
	askAI(d, "debug it")
	if !d.WaitForText("fake-done", 150*time.Second) {
		t.Fatalf("no final answer:\n%s\nresults %q", screen(d), s.toolResults())
	}
	if r := s.toolResults(); len(r) != len(calls) {
		t.Fatalf("results %q", r)
	}
	return s, d
}

// checkGoSession checks the results of TestAIDebugGoSession's calls.
func checkGoSession(t *testing.T, d *Driver, r []string) {
	if !strings.Contains(r[1], "Paused at main.go:8") || !strings.Contains(r[1], "Stopped at main.go:8 in main.add") {
		t.Fatalf("start did not stop at the breakpoint: %q", r[1])
	}
	readable := strings.Contains(r[2], "a int = 1")
	if !readable {
		t.Logf("dlv read no locals (a Go toolchain newer than dlv? set TERMOCODE_DLV_GOTOOLCHAIN=go1.24.6); value checks skipped:\n%s", r[2])
	}
	if !strings.Contains(r[2], "Locals") || !strings.Contains(r[2], "#1 main.main (main.go:14)") {
		t.Errorf("state: %q", r[2])
	}
	if readable && (!strings.Contains(r[2], "b int = 2") || !strings.HasPrefix(r[3], "a + b = 3")) {
		t.Errorf("values: state %q evaluate %q", r[2], r[3])
	}
	if !strings.Contains(r[4], "Paused at main.go:9") {
		t.Errorf("step_over did not move to line 9: %q", r[4])
	}
	if !strings.Contains(r[5], "The debug session ended.") || !strings.Contains(r[6], "No debug session is running.") {
		t.Errorf("stop: %q / %q", r[5], r[6])
	}
	// The editor followed the stops: main.go is the active tab at line 9.
	if _, a := tabs(d); a != "main.go" {
		t.Errorf("active tab %q, want main.go", a)
	}
	var cur []int
	_ = json.Unmarshal([]byte(lua(d, `return vim.json.encode(vim.api.nvim_win_get_cursor(0))`)), &cur)
	if len(cur) != 2 || cur[0] != 9 {
		t.Errorf("editor cursor %v, want line 9", cur)
	}
	if !strings.Contains(screen(d), "fake-done") {
		t.Errorf("the chat is gone:\n%s", screen(d))
	}
	if !d.WaitFor(func() bool { return len(leftovers(d.Dir)) == 0 }, 10*time.Second) {
		t.Errorf("dlv still runs after stop: %v", leftovers(d.Dir))
	}
}

// Spec: continue runs the program to its end: the session ends and the
// state has the program's output (dlv sends no exit code). debug_start with no
// config or path debugs the open file's package.
func TestAIDebugContinueToEnd(t *testing.T) {
	s, _ := goDebugAI(t, k7Main,
		toolCall{"debug_breakpoint", map[string]any{"op": "set", "path": "main.go", "line": 8}},
		toolCall{"open_file", map[string]any{"path": "main.go"}},
		toolCall{"debug_start", map[string]any{"wait_ms": 90000}},
		toolCall{"debug_control", map[string]any{"op": "continue"}},
	)
	r := s.toolResults()
	if !strings.Contains(r[2], "Paused at main.go:8") {
		t.Fatalf("start: %q", r[2])
	}
	for _, want := range []string{"The debug session ended.", "Program output", "result 3"} {
		if !strings.Contains(r[3], want) {
			t.Errorf("continue has no %q: %q", want, r[3])
		}
	}
}

// Spec: a program that does not build is a clear "did not start" with the
// compiler error, not a long wait.
func TestAIDebugStartBuildError(t *testing.T) {
	start := time.Now()
	s, _ := goDebugAI(t, "package main\n\nfunc main() { undefinedK7() }\n",
		toolCall{"debug_start", map[string]any{"path": "main.go", "wait_ms": 60000}},
	)
	r := s.toolResults()
	if !strings.Contains(r[0], "did not start") || !strings.Contains(r[0], "undefined") {
		t.Errorf("result %q", r[0])
	}
	if time.Since(start) > 40*time.Second {
		t.Errorf("waited %s for a build error", time.Since(start))
	}
}
