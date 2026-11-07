package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webRaceRig is a race env (temp repo, made-up agents, fake tabs) with a
// hub and one controller connection: the phone.
func webRaceRig(t *testing.T, bins ...string) (*raceEnv, *webFilesRig) {
	t.Helper()
	e := newRaceEnv(t, bins...)
	h := newWebHub()
	t.Cleanup(h.stop)
	e.m.web = h
	r := &webFilesRig{t: t, m: e.m, h: h, root: e.dir, kept: map[*webConn][]map[string]any{}}
	r.c = r.conn("c-1", webproto.RoleController)
	return e, r
}

// Spec (MOB-62, mobile plan 2.6): Start a race from the phone waits for
// a laptop card (Cancel first) that names the lanes, the task and the
// device; nothing starts before the laptop says yes. Then the lanes get
// their worktrees and tabs as from Agent: Race….
func TestWebAIRaceStartThroughTheLaptop(t *testing.T) {
	e, r := webRaceRig(t, "claude", "codex")
	m := e.m
	f := r.call(webproto.OpAIRaceStart, &webproto.AIRaceStartArgs{Task: "fix  the\tlogin", Agents: []string{"claude", "codex"}, Verify: "go test ./..."})
	var ref webproto.AskRef
	webResData(t, f, &ref)
	if ref.Card == "" || !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.Buttons()[0].ID != webproto.ChoiceCancel {
		t.Fatalf("start asked: %v open %v", f, m.confirmOpen)
	}
	body := m.web.asks.shown.body
	for _, want := range []string{"2 lanes", "Claude Code", "Codex", "fix the login", "go test ./...", "Requested from"} {
		if !strings.Contains(body, want) {
			t.Errorf("card body lacks %q:\n%s", want, body)
		}
	}
	if m.race != nil && len(m.race.races) != 0 {
		t.Fatal("started before the laptop said yes")
	}
	m.confirmOpen = false
	e.feed(m.answerWebAsk(webproto.ChoiceConfirm))
	race := e.race(t)
	if race.Prompt != "fix the login" || race.Verify != "go test ./..." || len(race.Lanes) != 2 || race.Lanes[0].Worktree == "" {
		t.Fatalf("race %+v", race)
	}

	// Cancel starts nothing.
	r.call(webproto.OpAIRaceStart, &webproto.AIRaceStartArgs{Task: "other", Agents: []string{"claude", "codex"}})
	m.confirmOpen = false
	e.feed(m.answerWebAsk(webproto.ChoiceCancel))
	if n := len(m.raceS().races); n != 1 {
		t.Errorf("%d races after cancel", n)
	}
}

// Spec (MOB-62): the laptop checks the lanes before any card: an agent
// that is not installed, or one whose screen termocode cannot read, is
// refused with the reason.
func TestWebAIRaceStartRefusals(t *testing.T) {
	e, r := webRaceRig(t, "claude")
	for _, agents := range [][]string{{"claude", "codex"}, {"claude", "nobody"}} {
		f := r.call(webproto.OpAIRaceStart, &webproto.AIRaceStartArgs{Task: "x", Agents: agents})
		if webErrCode(f) != string(webproto.CodeBadRequest) {
			t.Errorf("%v: %v", agents, f)
		}
	}
	if e.m.confirmOpen {
		t.Error("a card opened for a bad race")
	}
}

// Spec (MOB-62, plan 2.6): Summarise from the phone asks the quick model
// for the 3-line difference summary of the finished lanes (the laptop's
// AGX-6 summary: diffs capped and redacted), answers with the lines and
// cost and stores them in the race record. Fewer than two finished
// lanes, or no quick model: nothing is sent and the phone is told why.
func TestWebAIRaceSummary(t *testing.T) {
	e, r := webRaceRig(t, "claude", "codex")
	race := e.start(t, "change a", "", "claude", "codex")
	e.finishLane(t, 0, map[string]string{"a.txt": "one\nTWO\n"})
	p := &raceRecProvider{reply: "- Claude Code edits a.txt.\n- Codex is not done.\n"}
	e.m.aiS().slotFake = map[string]aiSlot{ai.SlotQuick: {p: p, cfg: ai.Config{Kind: ai.KindOllama, Model: "fake-1"}}}
	if f := r.call(webproto.OpAIRaceSummary, &webproto.AIRaceArgs{Race: race.ID}); webErrCode(f) == "" || len(p.reqs) != 0 {
		t.Fatalf("one finished lane: %v, %d sent", f, len(p.reqs))
	}
	e.finishLane(t, 1, map[string]string{"b.txt": "BEE\n"})

	old := raceQuickSlot
	raceQuickSlot = func(*aiState) (aiSlot, error) { return aiSlot{}, ai.ErrNotConfigured }
	f := r.call(webproto.OpAIRaceSummary, &webproto.AIRaceArgs{Race: race.ID})
	raceQuickSlot = old
	if msg, _ := f["msg"].(string); webErrCode(f) == "" || !strings.Contains(msg, "quick model") || len(p.reqs) != 0 {
		t.Fatalf("no quick model: %v", f)
	}

	var sum webproto.AIRaceSummary
	webResData(t, r.call(webproto.OpAIRaceSummary, &webproto.AIRaceArgs{Race: race.ID}), &sum)
	if len(p.reqs) != 1 || sum.Race != race.ID || strings.Join(sum.Lines, "|") != "Claude Code edits a.txt.|Codex is not done." || !strings.Contains(sum.Cost, "fake-1") {
		t.Fatalf("summary %+v (%d sent)", sum, len(p.reqs))
	}
	if got := e.race(t).Summary; len(got) != 2 {
		t.Errorf("not stored: %q", got)
	}
	if code := webErrCode(r.call(webproto.OpAIRaceSummary, &webproto.AIRaceArgs{Race: "race-99"})); code != string(webproto.CodeNotFound) {
		t.Errorf("unknown race: %s", code)
	}
}

// Spec (MOB-62): Verify from the phone runs the race's own verify
// command (from its start card) in the finished lanes only; a race
// without one says so. Stop sends Ctrl+C into the lanes that work and
// keeps their worktrees; with none working the phone is told.
func TestWebAIRaceVerifyAndStop(t *testing.T) {
	e, r := webRaceRig(t, "claude", "codex")
	race := e.start(t, "change a", "go test ./...", "claude", "codex")
	e.finishLane(t, 0, map[string]string{"a.txt": "x\n"})
	var jobs []string
	old := raceSpawnVerify
	t.Cleanup(func() { raceSpawnVerify = old })
	raceSpawnVerify = func(_ *Model, l agentrace.Lane, line string) (int, error) {
		jobs = append(jobs, l.ID+" "+line)
		return 700 + len(jobs), nil
	}
	var done webproto.AIRaceDone
	webResData(t, r.call(webproto.OpAIRaceVerify, &webproto.AIRaceArgs{Race: race.ID}), &done)
	if done.Lanes != 1 || len(jobs) != 1 || jobs[0] != race.Lanes[0].ID+" go test ./..." {
		t.Fatalf("verify %+v jobs %v", done, jobs)
	}

	webResData(t, r.call(webproto.OpAIRaceStop, &webproto.AIRaceArgs{Race: race.ID}), &done)
	if done.Lanes != 1 || e.race(t).Lanes[1].State != agentrace.Stopped || e.race(t).Lanes[0].State != agentrace.Done {
		t.Fatalf("stop %+v lanes %+v", done, e.race(t).Lanes)
	}
	if code := webErrCode(r.call(webproto.OpAIRaceStop, &webproto.AIRaceArgs{Race: race.ID})); code != string(webproto.CodeBusy) {
		t.Errorf("nothing to stop: %s", code)
	}

	none := e.start(t, "no verify", "", "claude", "codex")
	jobs = nil
	if f := r.call(webproto.OpAIRaceVerify, &webproto.AIRaceArgs{Race: none.ID}); webErrCode(f) == "" || len(jobs) != 0 {
		t.Errorf("verify without a command: %v %v", f, jobs)
	}
}

// Spec (MOB-62): the phone may act on a race the laptop has not read
// yet in this session: the records are read first (a lane read from disk
// has no live tab, so it counts as stopped), and the laptop's compare
// page does not open.
func TestWebAIRaceReadsTheRecords(t *testing.T) {
	e, r := webRaceRig(t, "claude", "codex")
	race := e.start(t, "change a", "", "claude", "codex")
	e.m.race = nil
	if code := webErrCode(r.call(webproto.OpAIRaceStop, &webproto.AIRaceArgs{Race: race.ID})); code != string(webproto.CodeBusy) {
		t.Fatalf("stop after reading: %s", code)
	}
	if e.m.racePageOpen() {
		t.Error("the compare page opened on the laptop")
	}
	if got := e.m.raceS().find(race.ID); got == nil || got.Lanes[0].State != agentrace.Stopped {
		t.Errorf("records %+v", got)
	}
	if code := webErrCode(r.call(webproto.OpAIRaceStop, &webproto.AIRaceArgs{Race: "race-42"})); code != string(webproto.CodeNotFound) {
		t.Errorf("unknown race: %s", code)
	}
}
