package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// webTestWT puts the WORKTREES fixture (wtFixture) in the Model.
func webTestWT(t *testing.T, m *Model) *wtState {
	t.Helper()
	st, _ := wtFixture(t)
	st.common = t.TempDir()
	m.wt = st
	m.w, m.h = 150, 42
	return st
}

func webTestWTSnap(t *testing.T, frames []map[string]any) (webproto.WorktreesSnap, int) {
	t.Helper()
	var s webproto.WorktreesSnap
	n := 0
	for _, f := range frames {
		if f["topic"] == "worktrees" {
			s = webproto.WorktreesSnap{}
			webTestData(t, f, &s)
			n++
		}
	}
	return s, n
}

func webTestWTItem(s webproto.WorktreesSnap, path string) (webproto.WorktreeItem, bool) {
	for _, w := range s.Worktrees {
		if w.ID == webWorktreeID(path) {
			return w, true
		}
	}
	return webproto.WorktreeItem{}, false
}

// Spec (WTX-16): the worktrees topic is the laptop's WORKTREES list:
// each worktree with an id (never its path), group, agent state, branch
// status, note and link; the archived row too. A change sends one new
// snapshot; no change sends none.
func TestWorktreesTopicSnapshot(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	st := webTestWT(t, &m)
	c := webTestSub(t, &m, "worktrees")
	frames := webTestNow(t, c)
	s, _ := webTestWTSnap(t, frames)
	if s.State != "ready" || len(s.Worktrees) != 8 {
		t.Fatalf("snap %+v", s)
	}
	side, ok := webTestWTItem(s, "/src/app.worktrees/wt-sidebar")
	if !ok || side.Group != "pinned" || !side.Pinned || side.State != "idle" || side.Ahead != 3 || side.Dirty != 4 || !side.Status ||
		side.Note != "adding the pin row" || side.Link == "" || side.Kind != "user" || len(side.ID) != webproto.WorktreeIDLen {
		t.Fatalf("wt-sidebar %+v", side)
	}
	if toast, _ := webTestWTItem(s, "/src/app.worktrees/toast"); toast.State != "working" || toast.Agent != "Codex" {
		t.Errorf("toast %+v", toast)
	}
	if main, _ := webTestWTItem(s, "/src/app"); !main.Here || main.Kind != "main" {
		t.Errorf("main %+v", main)
	}
	if old, _ := webTestWTItem(s, "/src/app.worktrees/old"); old.Group != "archived" || old.State != "archived" {
		t.Errorf("archived %+v", old)
	}
	b, _ := json.Marshal(frames)
	if strings.Contains(string(b), `"id":"/`) || strings.Contains(string(b), `"parent":"/`) {
		t.Errorf("a path names a worktree on the page: %s", b)
	}
	st.entries[1].Meta.Pinned = false
	st.touch()
	m.webAfterUpdate(nil)
	if _, n := webTestWTSnap(t, webTestNow(t, c)); n != 1 {
		t.Fatalf("%d frames after a change", n)
	}
	m.webAfterUpdate(nil)
	if _, n := webTestWTSnap(t, webTestNow(t, c)); n != 0 {
		t.Fatalf("%d frames without a change", n)
	}
}

// Spec (WTX-16): pin, note and sleep run as on the laptop; a sleep that
// would end agent tabs asks on the laptop (Keep running first) and says
// so; an unknown id is not found.
func TestWorktreesPinNoteSleep(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	st := webTestWT(t, &m)
	c := webTestSub(t, &m)
	md := func(p string) worktrees.Meta { all, _ := worktrees.Load(st.common); return all[p] }

	const api = "/elsewhere/api-v2"
	var done webproto.WorktreeDone
	f, _ := webTestCall(t, &m, c, webproto.OpWorktreesPin, `{"id":"`+webWorktreeID(api)+`","on":true}`)
	webTestData(t, f, &done)
	if !md(api).Pinned {
		t.Fatalf("pin: %+v %v", done, f)
	}
	webTestCall(t, &m, c, webproto.OpWorktreesNote, `{"id":"`+webWorktreeID(api)+`","text":"key sk-`+`test1234567890abcdef1234 here"}`)
	if n, ok := md(api).LastNote(); !ok || strings.Contains(n.Text, "1234567890abcdef") {
		t.Fatalf("note not stored or not redacted: %+v", md(api).Notes)
	}
	f, _ = webTestCall(t, &m, c, webproto.OpWorktreesSleep, `{"id":"`+webWorktreeID(api)+`","on":true}`)
	if md(api).State != worktrees.StateAsleep {
		t.Fatalf("sleep: %v", f)
	}
	webTestCall(t, &m, c, webproto.OpWorktreesSleep, `{"id":"`+webWorktreeID(api)+`","on":false}`)
	if md(api).State != "" {
		t.Fatal("wake")
	}

	// An agent tab works in wt-sidebar: the laptop asks first.
	m.terminalTabs = []terminalTab{{BufID: 5, Agent: "claude", Cwd: "/src/app.worktrees/wt-sidebar", AgentState: agents.Working}}
	stopped := wtStopSeam(t)
	done = webproto.WorktreeDone{}
	f, _ = webTestCall(t, &m, c, webproto.OpWorktreesSleep, `{"id":"`+webWorktreeID("/src/app.worktrees/wt-sidebar")+`","on":true}`)
	webTestData(t, f, &done)
	if !done.Asked || !m.confirmOpen || m.confirmKind != confirmKindWTSleep || m.confirm.SafeID() != "keep" || len(*stopped) != 0 {
		t.Fatalf("sleep with an agent: %+v open %v kind %d", done, m.confirmOpen, m.confirmKind)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpWorktreesPin, `{"id":"000000000000","on":true}`); webTestErr(f) != "not_found" {
		t.Fatalf("unknown id: %v", f)
	}
}

// Spec (WTX-16): archive waits for a laptop card (Cancel first); after
// yes the laptop's own archive flow starts on that worktree (its card,
// Cancel first) even when the laptop's filter hides it; nothing is
// removed by the page.
func TestWorktreesArchiveNeedsLaptopCard(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	st := webTestWT(t, &m)
	st.off = true      // no git: the laptop's flow shows its card
	st.filter = "grpc" // the laptop's list hides toast
	c := webTestSub(t, &m)
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpWorktreesArchive, `{"id":"`+webWorktreeID("/src/app.worktrees/toast")+`"}`), &ref)
	if !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.SafeID() != "cancel" {
		t.Fatal("no laptop card")
	}
	m.confirmOpen = false
	cmd := m.handleConfirmSelect("confirm")
	wtStep(t, &m, cmd)
	if !m.confirmOpen || m.confirmKind != confirmKindWTArchive || !strings.Contains(wtBox(&m), "fix/toast-race") {
		t.Fatalf("the laptop's archive card is not on toast: open %v kind %d\n%s", m.confirmOpen, m.confirmKind, wtBox(&m))
	}
	for _, p := range []string{"/src/app", "/elsewhere/api-v2"} { // the main checkout; a locked one
		if f := webTestCallBg(t, &m, c, webproto.OpWorktreesDelete, `{"id":"`+webWorktreeID(p)+`"}`); webTestErr(f) != "conflict" {
			t.Errorf("delete %s: %v", p, f)
		}
	}
}

// Spec (WTX-16): the worktrees replay golden: a pin and a sleep replay to
// the final snapshot.
func TestWorktreesReplayGolden(t *testing.T) {
	m := aiTestModel(t)
	h := webTestHub(t, &m)
	st := webTestWT(t, &m)
	c := webReplayRecord(t, &m, h, "worktrees", func() {
		for i := range st.entries {
			switch st.entries[i].Path {
			case "/elsewhere/api-v2":
				st.entries[i].Meta.Pinned = true
			case "/src/app.worktrees/toast":
				st.entries[i].Meta.State = worktrees.StateAsleep
			}
		}
		st.touch()
		m.webAfterUpdate(nil)
	})
	webReplayCheck(t, h.topics["worktrees"], c)
	webReplayGolden(t, webReplayPath("worktrees", "pin_sleep"), c)
}
