package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/raceview"
)

// Spec (CMP-20): b on the race page asks for one message and types it
// into every lane whose agent sits at its input box (a finished lane),
// never into a question or a busy agent. A card shows who gets it and
// who is skipped, Cancel first; Cancel types nothing. A lane that got
// the message works again and is done when its agent is back at the box.
func TestAgentRaceBroadcast(t *testing.T) {
	e := newRaceEnv(t, "claude", "codex", "gemini")
	e.start(t, "change a", "", "claude", "codex", "gemini")
	e.finishLane(t, 0, map[string]string{"a.txt": "x\n"})
	e.finishLane(t, 1, map[string]string{"b.txt": "y\n"})
	r := e.race(t)
	l0, l1, l2 := r.Lanes[0], r.Lanes[1], r.Lanes[2]
	b2 := e.m.raceS().bufOf(l2.ID)
	e.feed(e.m.raceOnAgentState(b2, agents.Done)) // the task is typed
	if tab, _ := e.m.termTabByBuf(b2); tab != nil {
		tab.AgentState = agents.Waiting
	}
	e.feed(e.m.raceOnAgentState(b2, agents.Waiting)) // a question
	before := map[int]int{}
	for b, keys := range e.typed {
		before[b] = len(keys)
	}

	e.act(raceview.ActBroadcast, "")
	if !e.m.promptOpen || e.m.promptKind != promptKindRaceBroadcast {
		t.Fatalf("no message prompt (open %v kind %v)", e.m.promptOpen, e.m.promptKind)
	}
	e.m.promptOpen = false
	e.feed(e.m.onRaceBroadcastText("  also add a test  "))
	if !e.m.confirmOpen || e.m.confirmKind != confirmKindRaceBroadcast {
		t.Fatal("no send card")
	}
	if b := e.m.confirm.Buttons(); len(b) == 0 || b[0].ID != "cancel" {
		t.Errorf("buttons %+v (Cancel first)", b)
	}
	p := e.m.raceS().bcast
	if p.text != "also add a test" || strings.Join(p.lanes, ",") != l0.ID+","+l1.ID {
		t.Errorf("plan %+v", p)
	}
	if len(p.skipped) != 1 || !strings.Contains(p.skipped[0], "Gemini CLI") || !strings.Contains(p.skipped[0], "question") {
		t.Errorf("skipped %v", p.skipped)
	}
	e.answer("cancel")
	for b, keys := range e.typed {
		if len(keys) != before[b] {
			t.Fatalf("Cancel typed into buffer %d: %v", b, keys[before[b]:])
		}
	}

	e.feed(e.m.onRaceBroadcastText("also add a test"))
	e.answer("send")
	for _, l := range []agentrace.Lane{l0, l1} {
		b := e.m.raceS().bufOf(l.ID)
		got := strings.Join(e.typed[b][before[b]:], "|")
		if got != "also add a test|\r" {
			t.Errorf("%s typed %q", l.Title, got)
		}
	}
	if got := e.typed[b2][before[b2]:]; len(got) != 0 {
		t.Errorf("the lane with a question got %v", got)
	}
	r = e.race(t)
	if r.Lanes[0].State != agentrace.Working || r.Lanes[1].State != agentrace.Working {
		t.Errorf("states %s %s", r.Lanes[0].State, r.Lanes[1].State)
	}
	b0 := e.m.raceS().bufOf(l0.ID)
	for _, st := range []agents.State{agents.Working, agents.Done} {
		if tab, _ := e.m.termTabByBuf(b0); tab != nil {
			tab.AgentState = st
		}
		e.feed(e.m.raceOnAgentState(b0, st))
	}
	if got := e.race(t).Lanes[0].State; got != agentrace.Done {
		t.Errorf("lane 0 after the follow-up %s", got)
	}

	// No lane at its input box: nothing to ask, the page says why.
	e2 := newRaceEnv(t, "claude", "codex")
	e2.start(t, "change a", "", "claude", "codex")
	e2.feed(e2.m.onRaceBroadcastText("hello"))
	if e2.m.confirmOpen {
		t.Error("a card with no lane to send to")
	}
}

// Spec (CMP-20): c on the race page asks for a command and runs it in
// every open lane's worktree (one task tab each) after a card, Cancel
// first; merged and discarded lanes are left out.
func TestAgentRaceRunAll(t *testing.T) {
	e := newRaceEnv(t, "claude", "codex")
	e.start(t, "change a", "", "claude", "codex")
	e.finishLane(t, 0, map[string]string{"a.txt": "x\n"})
	r := e.race(t)
	r.Lanes[1].State = agentrace.Discarded
	type job struct{ dir, line string }
	var jobs []job
	old := raceSpawnRun
	t.Cleanup(func() { raceSpawnRun = old })
	raceSpawnRun = func(_ *Model, l agentrace.Lane, line string) (int, error) {
		jobs = append(jobs, job{l.Worktree, line})
		return 700 + len(jobs), nil
	}
	e.act(raceview.ActRunAll, "")
	if !e.m.promptOpen || e.m.promptKind != promptKindRaceRun {
		t.Fatal("no command prompt")
	}
	e.m.promptOpen = false
	e.feed(e.m.onRaceRunText("go vet ./..."))
	if !e.m.confirmOpen || e.m.confirmKind != confirmKindRaceRun {
		t.Fatal("no run card")
	}
	if b := e.m.confirm.Buttons(); len(b) == 0 || b[0].ID != "cancel" {
		t.Errorf("buttons %+v (Cancel first)", b)
	}
	e.answer("cancel")
	if len(jobs) != 0 {
		t.Fatalf("Cancel ran %v", jobs)
	}
	e.feed(e.m.onRaceRunText("go vet ./..."))
	e.answer("run")
	if len(jobs) != 1 || jobs[0].dir != r.Lanes[0].Worktree || jobs[0].line != "go vet ./..." {
		t.Errorf("jobs %+v (only the open lane)", jobs)
	}
	e.feed(e.m.onRaceRunText("   "))
	if e.m.confirmOpen {
		t.Error("a card for an empty command")
	}
}
