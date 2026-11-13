//go:build unix

package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Races on the phone (MOB-61, mobile plan §3.9): the races topic lists
// the races newest first with each lane's state, files, lines, verify
// result and cost; it never sends a lane's folder, a file's text or the
// verify command line, and it redacts the task line.

// webRacesFixture is two races: race-1 closed (one lane merged), race-2
// with one lane working and one done.
func webRacesFixture() []agentrace.Race {
	cost := 0.42
	return []agentrace.Race{
		{ID: "race-1", Number: 1, Name: "race/fix-login", Prompt: "fix login", StartedMs: 1793966000000,
			Summary: []string{"Claude changes one file; Codex changes none"}, SummaryCost: "$0.01", Winner: "race-1-1-claude",
			Lanes: []agentrace.Lane{
				{ID: "race-1-1-claude", Agent: "claude", Title: "Claude", State: agentrace.Merged, Branch: "termocode/agent-race-1-1-claude",
					Stats: true, Files: []agentrace.FileStat{{Status: "M", Path: "internal/auth/login.go", Add: 4, Del: 1}}},
				{ID: "race-1-2-codex", Agent: "codex", Title: "Codex", State: agentrace.Discarded},
			}},
		{ID: "race-2", Number: 2, Name: "race/add-dark-mode", Prompt: "add dark mode", Verify: "go test ./...", StartedMs: 1793966280000,
			Lanes: []agentrace.Lane{
				{ID: "race-2-1-claude", Agent: "claude", Title: "Claude", State: agentrace.Working, Branch: "termocode/agent-race-2-1-claude",
					Worktree: "/home/u/src/app/.git/termocode/worktrees/race-2-1-claude"},
				{ID: "race-2-2-codex", Agent: "codex", Title: "Codex", State: agentrace.Done, Branch: "termocode/agent-race-2-2-codex",
					Worktree: "/home/u/src/app/.git/termocode/worktrees/race-2-2-codex", Stats: true,
					Files:  []agentrace.FileStat{{Status: "A", Path: "web/theme.css", Add: 30}, {Status: "M", Path: "web/app.tsx", Add: 5, Del: 2}},
					Verify: agentrace.Verify{State: agentrace.VerifyPass}, Cost: &cost},
			}},
	}
}

// webRacesModel is a model whose race records are already read.
func webRacesModel(t *testing.T, races []agentrace.Race) *Model {
	t.Helper()
	m := aiTestModel(t)
	webTestHub(t, &m)
	s := m.raceS()
	s.races, s.dir = races, t.TempDir()
	return &m
}

func webRacesSnap(t *testing.T, m *Model) webproto.RacesSnap {
	t.Helper()
	v, err := webRacesSnapshot(m, "")
	if err != nil {
		t.Fatal(err)
	}
	return v.(webproto.RacesSnap)
}

func TestRacesWebTopicListsNewestFirst(t *testing.T) {
	m := webRacesModel(t, webRacesFixture())
	snap := webRacesSnap(t, m)
	if snap.State != "ready" || len(snap.Races) != 2 || snap.Races[0].ID != "race-2" || snap.Races[1].ID != "race-1" {
		t.Fatalf("races %+v", snap)
	}
	r2, r1 := snap.Races[0], snap.Races[1]
	if r2.State != "working" || !r2.Verify || r1.State != "closed" || r1.Winner != "race-1-1-claude" || r1.SummaryCost != "$0.01" || len(r1.Summary) != 1 {
		t.Fatalf("race words %+v / %+v", r2, r1)
	}
	l := r2.Lanes[1]
	if l.State != "done" || !l.Stats || l.Files != 2 || l.Add != 35 || l.Del != 2 || l.Verify != "pass" || l.Cost != "$0.42" || len(l.Changed) != 2 || l.Changed[0].Status != "A" {
		t.Fatalf("lane %+v", l)
	}
	if w := r2.Lanes[0]; w.State != "working" || w.Stats || w.Cost != "" || w.Changed != nil {
		t.Fatalf("working lane %+v", w)
	}
}

func TestRacesWebTopicKeepsLaptopThingsOnTheLaptop(t *testing.T) {
	races := webRacesFixture()
	races[1].Prompt = "use key sk-ant-PLANTEDAPIKEY0123456789 and\nfix\x1b[31m it " + strings.Repeat("x", 400)
	races[1].Verify = "SECRET_TOKEN=abc make test"
	m := webRacesModel(t, races)
	b, err := json.Marshal(webRacesSnap(t, m))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, bad := range []string{"/home/u/src/app", "worktrees/race-2", "SECRET_TOKEN", "make test", "PLANTEDAPIKEY", "\\u001b", "\\n"} {
		if strings.Contains(s, bad) {
			t.Errorf("the topic carries %q: %s", bad, s)
		}
	}
	if p := webRacesSnap(t, m).Races[0].Prompt; len(p) > webproto.MaxRacePrompt+len("…") || !strings.HasSuffix(p, "…") {
		t.Errorf("prompt not cut: %d %q", len(p), p)
	}
}

func TestRacesWebTopicCapsListsAndStates(t *testing.T) {
	var races []agentrace.Race
	for n := 1; n <= webproto.MaxRaces+5; n++ {
		r := agentrace.Race{ID: fmt.Sprintf("race-%d", n), Number: n, Name: "race/x", Prompt: "x"}
		for k := 1; k <= 2; k++ {
			l := agentrace.Lane{ID: agentrace.LaneID(n, k, "codex"), Agent: "codex", Title: "Codex", State: agentrace.Stopped, Stats: true}
			for f := 0; f < webproto.MaxRaceFiles+10; f++ {
				l.Files = append(l.Files, agentrace.FileStat{Status: "R", Path: fmt.Sprintf("f%d.go", f), Add: 1})
			}
			r.Lanes = append(r.Lanes, l)
		}
		races = append(races, r)
	}
	races[len(races)-1].Lanes[0].State = "odd"
	m := webRacesModel(t, races)
	snap := webRacesSnap(t, m)
	if len(snap.Races) != webproto.MaxRaces || snap.Races[0].Number != webproto.MaxRaces+5 {
		t.Fatalf("races %d, first #%d", len(snap.Races), snap.Races[0].Number)
	}
	l := snap.Races[0].Lanes[0]
	if len(l.Changed) != webproto.MaxRaceFiles || l.Files != webproto.MaxRaceFiles+10 || l.Changed[0].Status != "M" || l.State != "stopped" {
		t.Fatalf("lane %d changed, %d files, %q %q", len(l.Changed), l.Files, l.Changed[0].Status, l.State)
	}
	if snap.Races[0].State != "done" {
		t.Fatalf("race of stopped lanes is %q", snap.Races[0].State)
	}
}

func TestRacesWebTopicReadsTheRecordsOnce(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m, "races")
	if snap := webRacesSnap(t, &m); snap.State != "loading" || snap.Races == nil {
		t.Fatalf("before the read %+v", snap)
	}
	if cmd := m.webPublishRaces(nil); cmd == nil || !m.web.races.loading {
		t.Fatal("no read of the records")
	}
	if cmd := m.webPublishRaces(nil); cmd != nil {
		t.Fatal("read twice")
	}
	dir := t.TempDir()
	m.webPublishRaces(webRacesLoadedMsg{races: webRacesFixture(), dir: dir})
	if m.race == nil || m.race.dir != dir || m.race.races[1].Lanes[0].State != agentrace.Stopped {
		t.Fatalf("records not kept (a lane read from disk has no tab: stopped): %+v", m.race)
	}
	var got webproto.RacesSnap
	for _, f := range webTestNow(t, c) {
		if f["topic"] == "races" && f["t"] == "delta" {
			webTestData(t, f, &got)
		}
	}
	if got.State != "ready" || len(got.Races) != 2 {
		t.Fatalf("no ready list sent: %+v", got)
	}
}

func TestRacesWebTopicNoRepoAndReadError(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	webTestSub(t, &m, "races")
	if msg, ok := webRacesLoadCmd(t.TempDir())().(webRacesLoadedMsg); !ok || !msg.none {
		t.Fatalf("a folder without git: %+v", msg)
	}
	m.webPublishRaces(webRacesLoadedMsg{none: true})
	if s := webRacesSnap(t, &m); s.State != "none" || len(s.Races) != 0 {
		t.Fatalf("no repo %+v", s)
	}
	m.web.races = webRacesPub{}
	m.webPublishRaces(webRacesLoadedMsg{err: errors.New("races.json: bad")})
	if s := webRacesSnap(t, &m); s.State != "error" {
		t.Fatalf("read error %+v", s)
	}
	if m.webPublishRaces(nil); m.web.races.loading {
		t.Fatal("a failed read is tried again on every update")
	}
}

func TestRacesReplayGolden(t *testing.T) {
	m := webRacesModel(t, webRacesFixture())
	c := webReplayRecord(t, m, m.web, "races", func() {
		l := &m.race.races[1].Lanes[0]
		l.State, l.Stats = agentrace.Done, true
		l.Files = []agentrace.FileStat{{Status: "M", Path: "web/theme.css", Add: 12, Del: 3}}
		l.Verify = agentrace.VerifyResult(1)
		m.web.races.checked = time.Time{}
		m.webAfterUpdate(nil)
	})
	webReplayCheck(t, m.web.topics["races"], c)
	webReplayGolden(t, webReplayPath("races", "lane_done"), c)
}
