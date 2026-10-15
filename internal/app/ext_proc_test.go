package app

import (
	"encoding/json"
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/ext"
	"github.com/amin-jalali/termocode/internal/extproc"
)

// procModel is a Model with extension state and no nvim / processes.
func procModel(t *testing.T) *Model {
	t.Helper()
	m := &Model{w: 100, h: 30, ext: newExtState(), showExp: true, explorerWidth: 30, activity: activity.New()}
	t.Cleanup(func() { m.ext.proc.Close(); activity.SetExtraItems(nil) })
	return m
}

func procMsg(gen int, method string, params any) extproc.Event {
	b, _ := json.Marshal(params)
	return extproc.Event{Ext: "fake", Kind: extproc.EvMessage, Gen: gen, Msg: extproc.Message{Method: method, Params: b}}
}

func feed(m *Model, evs ...extproc.Event) {
	for _, ev := range evs {
		m.onExtProcEvent(ev)
	}
	m.extRebuild()
}

func paletteIDs(m *Model) map[string]string {
	out := map[string]string{}
	for _, it := range m.extPaletteItems() {
		out[it.ID] = it.Title
	}
	return out
}

// Process registrations land in the same registry as the Lua ones: the
// palette, the activity bar and the status bar see them; an AI run of
// their palette ids asks first (class run), like every ext-… id.
func TestProcRegistrationsMergeIntoRegistry(t *testing.T) {
	m := procModel(t)
	m.ext.luaReg = ext.Registry{
		Commands: []ext.Command{{ID: "lua.cmd", Title: "Lua Cmd", Ext: "lua"}, {ID: "fake.dup", Title: "Lua wins", Ext: "lua"}},
		Panels:   []ext.Panel{{ID: "lua.p", Title: "LuaP", Ext: "lua"}},
	}
	x := ext.Extension{Name: "fake", Manifest: ext.Manifest{
		Run:         &ext.RunSpec{Command: "x"},
		Contributes: ext.Contributes{Commands: []ext.Command{{ID: "later", Title: "Later"}}},
	}}
	m.onExtProcSet(extProcSetMsg{exts: []ext.Extension{x}, base: 4})
	feed(m,
		procMsg(5, "registerCommand", map[string]string{"id": "hello", "title": "Say Hello"}),
		procMsg(5, "registerCommand", map[string]string{"id": "dup", "title": "Proc loses"}),
		procMsg(5, "registerStatusItem", map[string]string{"id": "pid", "text": "fake pid 1", "command": "hello"}),
		procMsg(5, "registerPanel", map[string]string{"id": "main", "title": "Fake", "icon": "Z"}),
		procMsg(5, "panel/setContent", map[string]any{"id": "main", "lines": []string{"a", "b"}}),
		procMsg(4, "registerCommand", map[string]string{"id": "stale", "title": "Stale"}), // before the reload
	)
	ids := paletteIDs(m)
	for id, title := range map[string]string{
		"ext-lua.cmd": "Ext: Lua Cmd", "ext-fake.dup": "Ext: Lua wins",
		"ext-fake.hello": "Ext: Say Hello", "ext-fake.later": "Ext: Later",
	} {
		if ids[id] != title {
			t.Errorf("palette %s = %q, want %q (all: %v)", id, ids[id], title, ids)
		}
		p, ok := aiActionPolicyFor(id)
		if !ok || p.Gate != aiGateConfirm || p.Class != ai.ClassRun {
			t.Errorf("%s: AI policy %+v, want confirm/run", id, p)
		}
	}
	if _, ok := ids["ext-fake.stale"]; ok {
		t.Error("an event from before the reload was applied")
	}
	if len(m.ext.reg.Panels) != 2 || m.ext.reg.Panels[1].ID != "fake.main" || m.ext.procPanel["fake.main"] != "fake" {
		t.Errorf("panels = %+v", m.ext.reg.Panels)
	}
	if i, ok := activity.ExtIndex(activity.ViewExtBase + 1); !ok || i != 1 {
		t.Error("no activity-bar item for the process panel")
	}
	if st := m.extStatusTexts(); len(st) != 1 || st[0] != "fake pid 1" {
		t.Errorf("status texts = %q", st)
	}
	if m.ext.reg.Status[0].Command != "fake.hello" || m.ext.procCmd["fake.hello"] != "fake" {
		t.Errorf("status command = %+v, owners %v", m.ext.reg.Status, m.ext.procCmd)
	}
	if l := m.ext.lines["fake.main"]; len(l) != 2 {
		t.Errorf("panel lines = %q", l)
	}

	// A crash drops what the run registered; the declared command stays.
	feed(m, extproc.Event{Ext: "fake", Kind: extproc.EvExited, Gen: 5, Err: "exit status 3", Restarting: true})
	ids = paletteIDs(m)
	if _, ok := ids["ext-fake.hello"]; ok {
		t.Error("runtime command kept after the crash")
	}
	if _, ok := ids["ext-fake.later"]; !ok {
		t.Error("contributed command lost after the crash")
	}
	if len(m.ext.reg.Panels) != 1 || len(m.extStatusTexts()) != 0 || len(m.ext.lines["fake.main"]) != 0 {
		t.Errorf("after crash: panels %+v status %q", m.ext.reg.Panels, m.extStatusTexts())
	}
	// The restarted run registers again.
	feed(m, procMsg(6, "registerCommand", map[string]string{"id": "hello", "title": "Say Hello"}))
	if _, ok := paletteIDs(m)["ext-fake.hello"]; !ok {
		t.Error("restart registration not applied")
	}
	// A late event of the old run is dropped.
	feed(m, procMsg(5, "registerCommand", map[string]string{"id": "old", "title": "Old"}))
	if _, ok := paletteIDs(m)["ext-fake.old"]; ok {
		t.Error("event of the crashed run applied after the restart")
	}
}

// A prompt from a process is answered once; a second prompt (or a
// reload) cancels the first.
func TestProcPromptPending(t *testing.T) {
	m := procModel(t)
	m.onExtProcSet(extProcSetMsg{exts: []ext.Extension{{Name: "fake", Manifest: ext.Manifest{Run: &ext.RunSpec{Command: "x"}}}}})
	ev := procMsg(1, "prompt", map[string]string{"title": "Name", "label": "Your name:"})
	ev.Msg.ID = json.RawMessage("7")
	feed(m, ev)
	if !m.promptOpen || m.promptKind != promptKindExt || m.ext.pendingProc == nil || string(m.ext.pendingProc.id) != "7" {
		t.Fatalf("prompt not open: open=%v pending=%+v", m.promptOpen, m.ext.pendingProc)
	}
	m.extPromptSubmit("Bob")
	if m.ext.pendingProc != nil {
		t.Error("pending prompt kept after submit")
	}
	// An exit while a pick is open drops it.
	pick := procMsg(1, "pick", map[string]any{"title": "P", "items": []map[string]string{{"title": "a"}}})
	pick.Msg.ID = json.RawMessage("8")
	feed(m, pick)
	if !m.pickerOpen || m.ext.pendingProc == nil || m.ext.pendingProc.kind != "pick" {
		t.Fatalf("pick not open: %+v", m.ext.pendingProc)
	}
	feed(m, extproc.Event{Ext: "fake", Kind: extproc.EvExited, Gen: 1, Restarting: true})
	if m.ext.pendingProc != nil {
		t.Error("pending pick kept after the process exited")
	}
}
