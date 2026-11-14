package app

import (
	"strings"
	"testing"
)

// Coverage of debug_state.go: events from Lua, snapshots, the async variable
// and evaluate requests, watches. Real nvim with a recording fake dap table.

func debugEventModel(t *testing.T) Model {
	t.Helper()
	m, _ := goRepoModel(t)
	if err := m.nvim.ExecLua(`_G._reqs = {}
_G._termocode_dap = {
  variables = function(id, ref) table.insert(_G._reqs, 'vars:' .. id .. ':' .. ref) end,
  evaluate = function(id, e, ctx) table.insert(_G._reqs, 'eval:' .. id .. ':' .. e .. ':' .. ctx) end,
  select_frame = function(id) table.insert(_G._reqs, 'frame:' .. id) end,
  state = function() return vim.json.encode({ available = true, active = true, stopped = true, name = 'x',
    thread = 1, frame = 2, frames = { { id = 2, name = 'main', path = '/a.go', line = 1 } },
    scopes = { { name = 'Locals', ref = 5, loaded = false } } }) end,
}`); err != nil {
		t.Fatal(err)
	}
	return m
}

func reqs(t *testing.T, m *Model) string {
	t.Helper()
	s, _ := m.nvim.EvalLuaString(`return table.concat(_G._reqs or {}, '|')`)
	return s
}

func TestCovDebugEvents(t *testing.T) {
	m := debugEventModel(t)
	d := m.ensureDebug()
	// output / messages
	m.onDebugEvent("output", `{"category":"stdout","output":"hello\n"}`)
	if m.onDebugEvent("message", `{"text":"warn me\nmore","level":3}`) == nil {
		t.Error("warnings toast")
	}
	m.onDebugEvent("message", `{"text":"fine","level":1}`)
	if !strings.Contains(consoleText(d), "hello") || !strings.Contains(consoleText(d), "warn me") {
		t.Errorf("console %q", consoleText(d))
	}
	// session start (twice: only the first prints the banner)
	m.onDebugEvent("session", `{"active":false}`)
	m.onDebugEvent("session", `{"active":true,"name":"dbg"}`)
	m.onDebugEvent("initialized", `{"name":"dbg"}`)
	if !m.dapSessionActive || d.status != "Running" || strings.Count(consoleText(d), "── dbg ──") != 1 {
		t.Errorf("session: active=%v status=%q", m.dapSessionActive, d.status)
	}
	// stop, continue, exit
	for _, reason := range []string{"breakpoint", "step", "exception", "pause", "entry", "goto", "weird"} {
		if m.onDebugEvent("stopped", `{"reason":"`+reason+`","text":"bad thing\nline2"}`) == nil {
			t.Error("stopped returns commands")
		}
		if !d.snap.Stopped || !strings.HasPrefix(d.status, "Paused") {
			t.Errorf("%s: %q", reason, d.status)
		}
	}
	m.onDebugEvent("stopped", `{"reason":"exception"}`)
	if d.status != "Paused on exception" {
		t.Error(d.status)
	}
	m.onDebugEvent("continued", "")
	if d.snap.Stopped || d.status != "Running" {
		t.Error("continued")
	}
	m.onDebugEvent("continued", "") // not stopped: only the status
	m.onDebugEvent("exited", `{"code":3}`)
	m.onDebugEvent("terminated", "")
	m.onDebugEvent("refresh", "")
	m.onDebugEvent("breakpoints", "")
	if !strings.Contains(consoleText(d), "exited with code 3") {
		t.Error("exit code line")
	}
	if dbgFirstLine("  a\nb ") != "a" || dbgFirstLine("solo") != "solo" || orDefault("", "d") != "d" || orDefault("x", "d") != "x" {
		t.Error("helpers")
	}
	// the notify handlers
	notify := notifyHandlers["termocode_dap"]
	if notify == nil {
		t.Fatal("handler not registered")
	}
	notify(&m, []any{"output", `{"output":"via notify\n"}`})
	notifyHandlers["termocode_dap_result"](&m, []any{int64(999), `{"result":"x"}`})
}

func TestCovDebugSnapshotAndRequests(t *testing.T) {
	m := debugEventModel(t)
	d := m.ensureDebug()
	// refresh runs the Lua state() and applies the snapshot
	cmd := m.debugRefresh()
	if cmd == nil {
		t.Fatal("refresh cmd")
	}
	msg := cmd()
	dm, ok := msg.(debugMsg)
	if !ok {
		t.Fatalf("msg %T", msg)
	}
	dm.apply(&m)
	if !m.dapSessionActive || !d.snap.Stopped || d.status != "Paused" {
		t.Fatalf("snapshot: active=%v stopped=%v status=%q", m.dapSessionActive, d.snap.Stopped, d.status)
	}
	// the stopped scope has no loaded variables: children were requested
	if !strings.Contains(reqs(t, &m), "vars:1:5") || !d.inflight["s:Locals"] {
		t.Errorf("reqs %q", reqs(t, &m))
	}
	m.debugRequestChildren("s:Locals", 5) // already in flight
	m.debugRequestChildren("x", 0)
	// the answer arrives: children stored, nested open nodes requested
	d.open["s:Locals/p"] = true
	m.onDebugResult(1, dbgResult{Variables: []dbgVar{{Name: "p", Ref: 9}, {Name: "n"}}})
	if len(d.children["s:Locals"]) != 2 || !strings.Contains(reqs(t, &m), "vars:2:9") {
		t.Errorf("children %v reqs %q", d.children, reqs(t, &m))
	}
	// an error is remembered and not retried
	m.onDebugResult(2, dbgResult{Error: "nope"})
	if d.childErr["s:Locals/p"] != "nope" {
		t.Errorf("childErr %v", d.childErr)
	}
	m.debugRequestOpenChildren()
	// empty variable list from the adapter
	d.pending[50] = dbgPending{kind: "vars", key: "s:Empty"}
	m.onDebugResult(50, dbgResult{})
	if d.children["s:Empty"] == nil {
		t.Error("empty children must be a non-nil list")
	}
	// loaded scope with open nested node
	d.snap.Scopes = []dbgScope{{Name: "Globals", Ref: 7, Loaded: true, Variables: []dbgVar{{Name: "g", Ref: 3}}}}
	d.open["s:Globals/g"] = true
	d.children["s:Globals/g"] = []dbgVar{{Name: "in", Ref: 4}}
	d.open["s:Globals/g/in"] = true
	m.debugRequestOpenChildren()
	if !strings.Contains(reqs(t, &m), ":4") {
		t.Errorf("nested request missing: %q", reqs(t, &m))
	}
	d.snap.Stopped = false
	if m.debugRequestOpenChildren() != nil {
		t.Error("only when stopped")
	}
	// a snapshot of an ended session resets everything and says so
	d.watches = []dbgWatch{{Expr: "a", Value: "1", Ref: 2}}
	m.dapSessionActive = true
	m.debugApplySnapshot(dbgSnapshot{Active: false})
	if m.dapSessionActive || d.watches[0].Value != "" || !strings.Contains(consoleText(d), "session ended") {
		t.Error("ended session")
	}
	m.debugApplySnapshot(dbgSnapshot{Active: true, Stopped: false})
	if d.status != "Running" {
		t.Error(d.status)
	}
	d.status = "Paused on step"
	m.debugApplySnapshot(dbgSnapshot{Active: true, Stopped: true})
	if d.status != "Paused on step" {
		t.Error("keeps a precise pause status")
	}
	// an unreadable state from Lua yields no message
	_ = m.nvim.ExecLua(`_G._termocode_dap.state = function() return '{bad' end`)
	if m.debugRefresh()() != nil {
		t.Error("bad state ignored")
	}
	_ = m.nvim.ExecLua(`_G._termocode_dap.state = function() return '' end`)
	if m.debugRefresh()() != nil {
		t.Error("empty state ignored")
	}
	if (&Model{}).debugRefresh() != nil {
		t.Error("no editor")
	}
	// stale ids are ignored
	if m.onDebugResult(12345, dbgResult{}) != nil {
		t.Error("stale")
	}
}

func TestCovDebugWatchesReplAndFrames(t *testing.T) {
	m := debugEventModel(t)
	d := m.ensureDebug()
	m.debugAddWatch("  ")
	m.debugAddWatch("a+1") // not stopped: no request yet
	m.debugAddWatch("a+1") // duplicate
	if len(d.watches) != 1 || reqs(t, &m) != "" {
		t.Errorf("watches %v reqs %q", d.watches, reqs(t, &m))
	}
	d.snap.Stopped = true
	m.debugAddWatch("b")
	if !strings.Contains(reqs(t, &m), "eval:1:b:watch") {
		t.Errorf("reqs %q", reqs(t, &m))
	}
	m.onDebugResult(1, dbgResult{Result: "5", Ref: 3})
	if d.watches[1].Value != "5" || d.watches[1].Ref != 3 {
		t.Errorf("watch %+v", d.watches[1])
	}
	d.pending[60] = dbgPending{kind: "watch", key: "b"}
	m.onDebugResult(60, dbgResult{Error: "undefined"})
	if !d.watches[1].Err || d.watches[1].Value != "undefined" {
		t.Errorf("watch %+v", d.watches[1])
	}
	// re-evaluation after a stop (debounced)
	cmd := m.debugEvaluateWatchesLater()
	if cmd == nil {
		t.Fatal("cmd")
	}
	if dm, ok := cmd().(debugMsg); ok {
		dm.apply(&m)
		d.snap.Stopped = false
		dm.apply(&m) // not stopped any more: nothing
	}
	d.watches = nil
	if m.debugEvaluateWatchesLater() != nil {
		t.Error("no watches")
	}
	m.debugRemoveWatch(-1)
	d.watches = []dbgWatch{{Expr: "x"}, {Expr: "y"}}
	m.debugRemoveWatch(0)
	if len(d.watches) != 1 || d.watches[0].Expr != "y" {
		t.Errorf("watches %v", d.watches)
	}
	// REPL answers
	m.dapSessionActive = true
	m.debugEvalRepl("1+1")
	m.onDebugResult(d.nextReq, dbgResult{Result: "2"})
	m.debugEvalRepl("bad")
	m.onDebugResult(d.nextReq, dbgResult{Error: "oops"})
	if !strings.Contains(consoleText(d), "2\n") || !strings.Contains(consoleText(d), "oops") {
		t.Errorf("console %q", consoleText(d))
	}
	// frame selection
	m.debugSelectFrame(7)
	if !strings.Contains(reqs(t, &m), "frame:7") || d.snap.Frame != 7 {
		t.Error("select frame")
	}
	(&Model{}).debugSelectFrame(1)
	(&Model{}).debugRequest(dbgPending{}, "x")
	// the toast helpers and the CSI route
	if m.debugToast("x") == nil || m.debugErrorToast("y") == nil {
		t.Error("toasts")
	}
	if _, handled := m.routeDebugMsg(debugMsg{}); !handled {
		t.Error("nil apply is handled")
	}
	if _, handled := m.routeDebugMsg("other"); handled {
		t.Error("strings are not debug messages")
	}
}
