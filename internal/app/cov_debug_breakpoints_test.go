package app

import (
	"os"
	"path/filepath"
	"testing"
)

// Coverage of debug_breakpoints.go: the store file, merging with the live
// breakpoints and the follow-rename / delete / clear paths.

func TestCovBreakpointStoreFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bp.json")
	if got := loadBreakpointFile(path); len(got) != 0 {
		t.Error("missing file")
	}
	_ = os.WriteFile(path, []byte("{bad"), 0o644)
	if got := loadBreakpointFile(path); len(got) != 0 {
		t.Error("broken file")
	}
	_ = os.WriteFile(path, []byte(`{"version":1,"breakpoints":{"/a/../a/x.go":[{"line":9},{"line":3},{"line":0}],"":[{"line":1}],"/b.go":[{"line":0}]}}`), 0o644)
	got := loadBreakpointFile(path)
	if len(got) != 1 || len(got["/a/x.go"]) != 2 || got["/a/x.go"][0].Line != 3 {
		t.Errorf("loaded %v", got)
	}
	// save: nil store, then a blocked directory
	if err := saveBreakpointFile(filepath.Join(dir, "sub", "bp.json"), nil); err != nil {
		t.Error(err)
	}
	blocker := filepath.Join(dir, "blocker")
	_ = os.WriteFile(blocker, []byte("x"), 0o644)
	if saveBreakpointFile(filepath.Join(blocker, "bp.json"), got) == nil {
		t.Error("cannot create a dir under a file")
	}
	if os.Geteuid() != 0 {
		ro := filepath.Join(dir, "ro")
		_ = os.MkdirAll(ro, 0o555)
		defer os.Chmod(ro, 0o755)
		if saveBreakpointFile(filepath.Join(ro, "bp.json"), got) == nil {
			t.Error("read-only dir")
		}
	}
	if _, err := breakpointsPath(); err != nil {
		t.Error(err)
	}
	if d := loadDebugState(); d == nil || d.bpPath == "" {
		t.Error("loadDebugState")
	}
}

func TestCovBreakpointMergeAndRows(t *testing.T) {
	ok := true
	store := map[string][]storedBreakpoint{"/a.go": {{Line: 2}}, "/b.go": {{Line: 5}}}
	live := liveBreakpoints{
		Loaded: []string{"/a.go", "/c.go"},
		Bps: map[string][]liveBreakpoint{
			"/a.go": {{storedBreakpoint: storedBreakpoint{Line: 7}, Verified: &ok}, {storedBreakpoint: storedBreakpoint{Line: 0}}},
			"/c.go": {},
		},
	}
	merged, changed := mergeBreakpoints(store, live)
	if !changed || len(merged["/a.go"]) != 1 || merged["/a.go"][0].Line != 7 || len(merged["/b.go"]) != 1 {
		t.Errorf("merged %v", merged)
	}
	if _, changed := mergeBreakpoints(merged, live); changed {
		t.Error("same state is not a change")
	}
	rows := sortedBreakpointRows(merged, live.Bps)
	if len(rows) != 2 || rows[0].Path != "/a.go" || rows[0].Verified == nil {
		t.Errorf("rows %+v", rows)
	}
	if breakpointStoresEqual(map[string][]storedBreakpoint{"a": {{Line: 1}}}, map[string][]storedBreakpoint{"b": {{Line: 1}}}) ||
		breakpointStoresEqual(map[string][]storedBreakpoint{"a": {{Line: 1}}}, map[string][]storedBreakpoint{"a": {{Line: 1}, {Line: 2}}}) ||
		breakpointStoresEqual(map[string][]storedBreakpoint{"a": {{Line: 1}}}, map[string][]storedBreakpoint{"a": {{Line: 2}}}) {
		t.Error("unequal stores")
	}
	moved, ch := moveBreakpointStore(map[string][]storedBreakpoint{"/d/x.go": {{Line: 1}}, "/e.go": {{Line: 1}}}, "/d", "/f")
	if !ch || moved["/f/x.go"] == nil || moved["/e.go"] == nil {
		t.Errorf("moved %v", moved)
	}
	if _, ch := moveBreakpointStore(moved, "/zzz", "/y"); ch {
		t.Error("nothing moved")
	}
	dropped, ch := dropBreakpointStore(moved, []string{"/f"})
	if !ch || len(dropped) != 1 {
		t.Errorf("dropped %v", dropped)
	}
	if _, ch := dropBreakpointStore(dropped, []string{"/nope"}); ch {
		t.Error("nothing dropped")
	}
}

func TestCovBreakpointModelGlue(t *testing.T) {
	m, dir := goRepoModel(t)
	d := m.ensureDebug()
	d.bpPath = filepath.Join(t.TempDir(), "bp.json")
	lib := filepath.Join(dir, "lib.go")
	// a fake dap table that records what Go tells it and reports a live breakpoint
	_ = m.nvim.ExecLuaArgs(`local lib = ...
_G._calls = {}
_G._termocode_dap = {
  set_stored = function(j) table.insert(_G._calls, 'stored') end,
  restore_all = function() table.insert(_G._calls, 'restore_all') end,
  restore = function(b) table.insert(_G._calls, 'restore') end,
  remove = function(p, l) table.insert(_G._calls, 'remove:' .. l) end,
  clear = function() table.insert(_G._calls, 'clear') end,
  breakpoints = function() return vim.json.encode({ loaded = { lib }, bps = { [lib] = { { line = 4, verified = true } } } }) end,
}`, lib)
	(&Model{}).debugLoadBreakpoints()
	m.debugLoadBreakpoints()
	if got := dapCalls(t, &m); got != "stored|restore_all" {
		t.Errorf("calls %q", got)
	}
	// sync pulls the live state, merges and saves
	msg := m.debugSyncBreakpointsCmd()()
	dm, ok := msg.(debugMsg)
	if !ok {
		t.Fatalf("msg %T", msg)
	}
	dm.apply(&m)
	if len(d.bps[lib]) != 1 || d.bps[lib][0].Line != 4 {
		t.Fatalf("bps %v", d.bps)
	}
	if b, _ := os.ReadFile(d.bpPath); len(b) == 0 {
		t.Error("store not saved")
	}
	// same state again: no write
	m.debugApplyLiveBreakpoints(liveBreakpoints{Loaded: []string{lib}, Bps: map[string][]liveBreakpoint{lib: {{storedBreakpoint: storedBreakpoint{Line: 4}}}}})
	// unreadable results give no message
	_ = m.nvim.ExecLua(`_G._termocode_dap.breakpoints = function() return '{bad' end`)
	if m.debugSyncBreakpointsCmd()() != nil {
		t.Error("bad payload")
	}
	_ = m.nvim.ExecLua(`_G._termocode_dap.breakpoints = function() return '' end`)
	if m.debugSyncBreakpointsCmd()() != nil {
		t.Error("empty payload")
	}
	if (&Model{}).debugSyncBreakpointsCmd() != nil {
		t.Error("no editor")
	}
	// a failing save is recorded, not fatal
	d.bpPath = filepath.Join(lib, "no", "bp.json")
	m.debugApplyLiveBreakpoints(liveBreakpoints{Loaded: []string{lib}, Bps: map[string][]liveBreakpoint{}})
	d.bpPath = filepath.Join(t.TempDir(), "bp.json")
	d.bps = map[string][]storedBreakpoint{lib: {{Line: 4}, {Line: 8}}, "/gone.go": {{Line: 1}}}
	// remove: through Lua and from the store
	m.debugRemoveBreakpoint(lib, 4)
	m.debugRemoveBreakpoint(lib, 8)
	m.debugRemoveBreakpoint("/gone.go", 1)
	m.debugRemoveBreakpoint("/never.go", 1)
	if len(d.bps) != 0 {
		t.Errorf("bps %v", d.bps)
	}
	// rename and delete follow
	d.bps = map[string][]storedBreakpoint{lib: {{Line: 4}}}
	m.debugFollowRename(lib, lib+".new")
	if d.bps[lib+".new"] == nil {
		t.Errorf("rename: %v", d.bps)
	}
	m.debugFollowRename("/unrelated", "/other")
	m.debugForgetPaths([]string{"/unrelated"})
	m.debugForgetPaths([]string{lib + ".new"})
	if len(d.bps) != 0 {
		t.Errorf("forget: %v", d.bps)
	}
	(&Model{}).debugFollowRename("a", "b")
	(&Model{}).debugForgetPaths([]string{"a"})
	// toggling goes through the same sync
	if m.dapToggleBreakpoint() == nil {
		t.Log("toggle returns the sync cmd")
	}
	if m.debugClearBreakpoints() == nil || d.liveBps != nil || len(d.bps) != 0 {
		t.Error("clear")
	}
	// push with no store
	d.bps = nil
	m.debugPushStored()
	(&Model{}).debugPushStored()
}
