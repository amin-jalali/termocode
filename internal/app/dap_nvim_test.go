package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// Group D — integration tests for the DAP Lua (dap_lua.go) against a real
// headless nvim with the bootstrapped nvim-dap. Skipped when nvim or the
// plugin is missing; the session test also needs dlv + go.

func nvimDapPluginPath(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	root, err := pluginsDir()
	if err != nil {
		t.Skip("no plugins dir")
	}
	p := filepath.Join(root, "nvim-dap")
	if _, err := os.Stat(filepath.Join(p, "lua", "dap.lua")); err != nil {
		t.Skip("nvim-dap not bootstrapped at " + p)
	}
	return p
}

// dapTestClient starts nvim, loads the DAP chunk and collects every
// termocode_dap / termocode_dap_result notification on the returned
// channel (the redraw stream is drained so nothing is dropped).
func dapTestClient(t *testing.T, plugin string) (*nvim.Client, chan nvim.NotifyMsg) {
	t.Helper()
	c, err := nvim.New()
	if err != nil {
		t.Fatalf("nvim.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	for _, m := range []string{"termocode_dap", "termocode_dap_result"} {
		if err := c.RegisterNotify(m, nil); err != nil {
			t.Fatalf("RegisterNotify: %v", err)
		}
	}
	if err := c.Attach(120, 40); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	events := make(chan nvim.NotifyMsg, 1024)
	go func() {
		for {
			msg := c.Next()()
			if msg == nil {
				return
			}
			if n, ok := msg.(nvim.NotifyMsg); ok {
				select {
				case events <- n:
				default:
				}
			}
		}
	}()
	if err := c.ExecLua(dapSetupLua(plugin)); err != nil {
		t.Fatalf("dapSetupLua: %v", err)
	}
	return c, events
}

func TestDapBreakpointRestoreNvim(t *testing.T) {
	plugin := nvimDapPluginPath(t)
	c, events := dapTestClient(t, plugin)

	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	src := "package main\n\n// moved down by two lines\n\nfunc main() {\n\tx := 1\n\n\t_ = x\n}\n"
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// Stored before the file grew: "x := 1" was on line 4, a blank line 5
	// (now line 7 is blank) and a line past EOF.
	stored := map[string][]storedBreakpoint{file: {
		{Line: 4, Text: "x := 1"},
		{Line: 7, Text: "gone()"},
		{Line: 99, Text: "nowhere()"},
		{Line: 8, Text: "_ = x", Condition: "x > 0"},
	}}
	data, _ := json.Marshal(stored)
	if err := c.ExecLuaArgs(`_G._termocode_dap.set_stored(...)`, string(data)); err != nil {
		t.Fatal(err)
	}
	if err := c.ExecLuaArgs(`vim.cmd('edit ' .. vim.fn.fnameescape(...))`, file); err != nil {
		t.Fatal(err)
	}
	raw, err := c.EvalLuaString(`return _G._termocode_dap.breakpoints()`)
	if err != nil {
		t.Fatal(err)
	}
	var live liveBreakpoints
	if err := json.Unmarshal([]byte(raw), &live); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	got := live.Bps[file]
	// "x := 1" follows its text 4 → 6; the conditional stays on 8 (exact
	// match wins); "gone()" (now a blank line 7) would move to 8, which is
	// taken, so it is dropped; line 99 is past EOF.
	if len(got) != 2 {
		t.Fatalf("restored %+v, want 2 breakpoints (raw %s)", got, raw)
	}
	lines := map[int]liveBreakpoint{}
	for _, bp := range got {
		lines[bp.Line] = bp
	}
	if _, ok := lines[6]; !ok || lines[6].Text != "x := 1" {
		t.Errorf("x := 1 not relocated to line 6: %+v", got)
	}
	if bp, ok := lines[8]; !ok || bp.Condition != "x > 0" {
		t.Errorf("conditional breakpoint lost: %+v", got)
	}
	if _, ok := lines[99]; ok {
		t.Errorf("line past EOF must be dropped: %+v", got)
	}

	// A save of a buffer with breakpoints notifies Go.
	if err := c.Command("write"); err != nil {
		t.Fatal(err)
	}
	waitDapEvent(t, events, "breakpoints", 3*time.Second)

	// Merge into the store and toggle one off through the helper.
	merged, changed := mergeBreakpoints(stored, live)
	if !changed || len(merged[file]) != 2 {
		t.Errorf("merge: changed=%v %+v", changed, merged[file])
	}
	_ = c.ExecLuaArgs(`local p, l = ...; _G._termocode_dap.remove(p, l)`, file, 6)
	raw, _ = c.EvalLuaString(`return _G._termocode_dap.breakpoints()`)
	live = liveBreakpoints{}
	_ = json.Unmarshal([]byte(raw), &live)
	if len(live.Bps[file]) != 1 {
		t.Errorf("after remove: %+v", live.Bps[file])
	}
}

func TestDapRelocateNvim(t *testing.T) {
	plugin := nvimDapPluginPath(t)
	c, _ := dapTestClient(t, plugin)
	cases := []struct {
		line int
		text string
		want int
	}{
		{2, "b", 2},   // unchanged
		{1, "c", 3},   // text moved down
		{5, "a", 1},   // text moved up
		{3, "zzz", 3}, // text gone, line still valid
		{4, "", 5},    // blank line → next code line
		{9, "q", 0},   // past EOF, text unknown → dropped
	}
	for _, tc := range cases {
		out, err := c.EvalLuaStringArgs(`local l, t = ...
			local r = _G._termocode_dap.relocate({ 'a', 'b', 'c', '', 'e' }, l, t)
			return tostring(r or 0)`, tc.line, tc.text)
		if err != nil {
			t.Fatal(err)
		}
		if out != itoaTest(tc.want) {
			t.Errorf("relocate(%d, %q) = %s, want %d", tc.line, tc.text, out, tc.want)
		}
	}
}

func itoaTest(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// waitDapEvent waits for a termocode_dap event of the given kind.
func waitDapEvent(t *testing.T, events chan nvim.NotifyMsg, kind string, d time.Duration) nvim.NotifyMsg {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case n := <-events:
			if n.Method == "termocode_dap" && notifyArgString(n.Args, 0) == "message" {
				t.Logf("dap message: %s", notifyArgString(n.Args, 1))
			}
			if n.Method == "termocode_dap" && notifyArgString(n.Args, 0) == kind {
				return n
			}
		case <-deadline:
			t.Fatalf("no %q event within %v", kind, d)
		}
	}
}

// waitDapResult waits for termocode_dap_result(id, …).
func waitDapResult(t *testing.T, events chan nvim.NotifyMsg, id int, d time.Duration) dbgResult {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case n := <-events:
			if n.Method == "termocode_dap_result" && notifyArgInt(n.Args, 0) == id {
				var res dbgResult
				_ = json.Unmarshal([]byte(notifyArgString(n.Args, 1)), &res)
				return res
			}
		case <-deadline:
			t.Fatalf("no result %d within %v", id, d)
		}
	}
}

func TestDapSessionDelveNvim(t *testing.T) {
	plugin := nvimDapPluginPath(t)
	if _, err := exec.LookPath("dlv"); err != nil {
		t.Skip("dlv not installed")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not installed")
	}
	if testing.Short() {
		t.Skip("short mode")
	}
	// Optional: pin the toolchain dlv builds with (e.g. when the default Go
	// is newer than dlv supports): TERMOCODE_DLV_GOTOOLCHAIN=go1.25.12.
	if tc := os.Getenv("TERMOCODE_DLV_GOTOOLCHAIN"); tc != "" {
		t.Setenv("GOTOOLCHAIN", tc)
	}
	c, events := dapTestClient(t, plugin)

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module dbgtest\n\ngo 1.21\n"), 0o644)
	file := filepath.Join(dir, "main.go")
	src := `package main

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
	_ = os.WriteFile(file, []byte(src), 0o644)
	if err := c.ExecLuaArgs(`local d, f = ...; vim.cmd('cd ' .. vim.fn.fnameescape(d)); vim.cmd('edit ' .. vim.fn.fnameescape(f)); vim.api.nvim_win_set_cursor(0, { 8, 0 })`, dir, file); err != nil {
		t.Fatal(err)
	}
	_ = c.ExecLua(`_G._termocode_dap.toggle()`)
	// A dlv older than the Go toolchain refuses to build; the check is
	// irrelevant for this tiny program.
	_ = c.ExecLua(`local a = require('dap').adapters.delve; if a then table.insert(a.executable.args, '--check-go-version=false') end`)

	cfg, _ := json.Marshal(map[string]any{"type": "go", "request": "launch", "name": "dbgtest", "program": dir})
	res, err := c.EvalLuaStringArgs(`return _G._termocode_dap.run(...)`, string(cfg))
	if err != nil || res != "" {
		t.Fatalf("run: %q %v", res, err)
	}
	stopped := waitDapEvent(t, events, "stopped", 60*time.Second)
	var ev debugEvent
	_ = json.Unmarshal([]byte(notifyArgString(stopped.Args, 1)), &ev)
	if ev.Reason != "breakpoint" || stopStatus(ev.Reason, ev.Text) != "Paused on breakpoint" {
		t.Errorf("stop reason = %+v", ev)
	}

	// Poll the snapshot until frames + scopes + variables are in.
	var snap dbgSnapshot
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := c.EvalLuaString(`return _G._termocode_dap.state()`)
		if err != nil {
			t.Fatal(err)
		}
		snap = dbgSnapshot{}
		if err := json.Unmarshal([]byte(raw), &snap); err != nil {
			t.Fatalf("decode state %q: %v", raw, err)
		}
		if len(snap.Frames) > 0 && len(snap.Scopes) > 0 && snap.Scopes[0].Loaded {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !snap.Active || !snap.Stopped || len(snap.Frames) == 0 {
		t.Fatalf("snapshot = %+v", snap)
	}
	if top := snap.Frames[0]; !strings.HasSuffix(top.Name, "add") || top.Line != 8 || top.Path != file {
		t.Errorf("top frame = %+v", top)
	}
	if snap.Frame != snap.Frames[0].ID {
		t.Errorf("current frame %d, want top %d", snap.Frame, snap.Frames[0].ID)
	}
	// A Delve older than the Go toolchain stops fine but reads no locals;
	// then only the request plumbing is checked, not the values.
	readable := len(snap.Scopes[0].Variables) > 0
	if !readable {
		t.Log("dlv returned no locals (toolchain newer than dlv?) — skipping value checks")
	}
	vars := map[string]string{}
	for _, v := range snap.Scopes[0].Variables {
		vars[v.Name] = v.Value
	}
	if readable && (vars["a"] != "1" || vars["b"] != "2") {
		t.Errorf("locals = %v (scopes %+v)", vars, snap.Scopes)
	}
	var frames []dbgFrame
	raw, _ := c.EvalLuaString(`return _G._termocode_dap.frames()`)
	if err := json.Unmarshal([]byte(raw), &frames); err != nil || len(frames) < 2 {
		t.Errorf("frames() = %s (%v)", raw, err)
	}
	var threads []dbgThread
	raw, _ = c.EvalLuaString(`return _G._termocode_dap.threads()`)
	if err := json.Unmarshal([]byte(raw), &threads); err != nil || len(threads) == 0 {
		t.Errorf("threads() = %s (%v)", raw, err)
	}

	// evaluate (REPL / watch).
	_ = c.ExecLuaArgs(`_G._termocode_dap.evaluate(...)`, 7, "a + b", "repl")
	if r := waitDapResult(t, events, 7, 5*time.Second); readable && (r.Error != "" || r.Result != "3") {
		t.Errorf("evaluate = %+v", r)
	}
	_ = c.ExecLuaArgs(`_G._termocode_dap.evaluate(...)`, 8, "nope_undefined", "watch")
	if r := waitDapResult(t, events, 8, 5*time.Second); r.Error == "" {
		t.Errorf("evaluate of an unknown name should fail: %+v", r)
	}
	// variables() on a scope reference answers without error.
	_ = c.ExecLuaArgs(`_G._termocode_dap.variables(...)`, 10, snap.Scopes[0].Ref)
	if r := waitDapResult(t, events, 10, 5*time.Second); r.Error != "" {
		t.Errorf("variables(scope) = %+v", r)
	}

	// Select the caller frame → it becomes current and its scopes load.
	caller := snap.Frames[1]
	_ = c.ExecLuaArgs(`_G._termocode_dap.select_frame(...)`, caller.ID)
	var pRef int
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := c.EvalLuaString(`return _G._termocode_dap.state()`)
		snap = dbgSnapshot{}
		_ = json.Unmarshal([]byte(raw), &snap)
		if snap.Frame == caller.ID && len(snap.Scopes) > 0 && snap.Scopes[0].Loaded {
			for _, v := range snap.Scopes[0].Variables {
				if v.Name == "p" {
					pRef = v.Ref
				}
			}
			if pRef > 0 || !readable {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if snap.Frame != caller.ID {
		t.Fatalf("select_frame: current frame %d, want %d", snap.Frame, caller.ID)
	}
	if readable {
		if pRef == 0 {
			t.Fatalf("caller frame scopes never showed p: %+v", snap)
		}
		_ = c.ExecLuaArgs(`_G._termocode_dap.variables(...)`, 9, pRef)
		r := waitDapResult(t, events, 9, 5*time.Second)
		fields := map[string]string{}
		for _, v := range r.Variables {
			fields[v.Name] = v.Value
		}
		if fields["X"] != "1" || fields["Y"] != "2" {
			t.Errorf("children of p = %+v", r)
		}
	}

	// Terminate → session goes away.
	_ = c.ExecLua(`require('dap').terminate()`)
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := c.EvalLuaString(`return _G._termocode_dap.state()`)
		snap = dbgSnapshot{}
		_ = json.Unmarshal([]byte(raw), &snap)
		if !snap.Active {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if snap.Active {
		t.Fatal("session still active after terminate")
	}
}
