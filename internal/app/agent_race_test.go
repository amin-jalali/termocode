package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/raceview"
)

// raceEnv is a model on a temp repo with made-up agents on PATH; lanes
// open fake tabs and what would be typed into them is recorded. No CLI
// ever runs.
type raceEnv struct {
	m     *Model
	dir   string
	typed map[int][]string // buf → keys sent
	next  int
}

func newRaceEnv(t *testing.T, bins ...string) *raceEnv {
	t.Helper()
	m, dir := covGitRepo(t)
	t.Chdir(dir)
	fakeAgentLookPath(t, bins...)
	e := &raceEnv{m: m, dir: dir, typed: map[int][]string{}, next: 100}
	oldL, oldS, oldD := raceLaunchLane, raceSendKeys, raceSubmitDelay
	t.Cleanup(func() { raceLaunchLane, raceSendKeys, raceSubmitDelay = oldL, oldS, oldD })
	raceSubmitDelay = 0
	raceLaunchLane = func(m *Model, l agentrace.Lane, a agents.Agent) (int, error) {
		e.next++
		m.terminalTabs = append(m.terminalTabs, terminalTab{BufID: e.next, Cwd: l.Worktree, Agent: a.ID, AgentState: agents.StartState(a.ID)})
		return e.next, nil
	}
	raceSendKeys = func(_ *Model, buf int, text string) { e.typed[buf] = append(e.typed[buf], text) }
	return e
}

// feed runs cmd and gives every message back to the race handler (the
// Bubble Tea loop in small).
func (e *raceEnv) feed(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			e.feed(c)
		}
	default:
		if c, ok := e.m.handleRaceMsg(msg); ok {
			e.feed(c)
		}
	}
}

func (e *raceEnv) race(t *testing.T) *agentrace.Race {
	t.Helper()
	r := e.m.raceS().current()
	if r == nil {
		t.Fatal("no race")
	}
	return r
}

func (e *raceEnv) start(t *testing.T, task, verify string, lanes ...string) *agentrace.Race {
	t.Helper()
	e.feed(func() tea.Msg { return raceview.StartMsg{Task: task, Verify: verify, Lanes: lanes} })
	return e.race(t)
}

// Spec (AGX-4, plan 2.5): F1 → Agent: Race… opens the race card with the
// installed agents as lanes; one whose screen termocode cannot read is
// listed but cannot be picked (the task could not be typed safely). The
// AI cannot open it (it has agent_race_start); the compare page is a
// view it may open.
func TestAgentRacePaletteAndCard(t *testing.T) {
	e := newRaceEnv(t, "claude", "codex", "amp")
	if p, ok := aiActionPolicyFor("agent-race"); !ok || p.Gate != aiGateNever {
		t.Errorf("agent-race policy %+v %v", p, ok)
	}
	if p, ok := aiActionPolicyFor("agent-race-compare"); !ok || p.Gate != aiGateAuto {
		t.Errorf("agent-race-compare policy %+v %v", p, ok)
	}
	titles := map[string]string{}
	for _, it := range tasksPaletteItems() {
		titles[it.ID] = it.Title
	}
	if titles["agent-race"] != "Agent: Race…" || titles["agent-race-compare"] != "Agent: Race Compare" {
		t.Errorf("palette rows %q %q", titles["agent-race"], titles["agent-race-compare"])
	}
	if _, ok := e.m.dispatchTasksPalette("agent-race"); !ok || !e.m.raceFormOpen() {
		t.Fatal("the race card did not open")
	}
	var got []string
	for _, o := range e.m.raceS().opts {
		got = append(got, o.ID+map[bool]string{true: "(off)"}[o.Disabled])
	}
	if strings.Join(got, ",") != "claude,codex,amp(off)" {
		t.Errorf("lane options %v", got)
	}
	e.feed(func() tea.Msg { return raceview.CancelMsg{} })
	if e.m.raceFormOpen() {
		t.Error("Cancel left the card open")
	}
	if _, err := os.Stat(filepath.Join(e.dir, ".git", "termocode", agentrace.FileName)); err == nil {
		t.Error("Cancel wrote a race")
	}
}

// Spec (AGX-4): Start makes one agent worktree per lane from HEAD
// (race-<n>-<k>-<agent>, hidden in the git dir), opens a tab in it that
// runs the agent, records the race and opens the compare page. The task
// is typed only once the agent's input box is on screen (Done), never
// while it asks the user (Waiting), and only once; the lane is done when
// the agent is back at its input box after it worked.
func TestAgentRaceStartAndTyping(t *testing.T) {
	e := newRaceEnv(t, "claude", "codex")
	covWrite(t, e.dir, "dirty.txt", "not committed\n") // stays out of the lanes (from HEAD)
	r := e.start(t, "fix the toast race", "go test ./...", "claude", "codex")
	if r.ID != "race-1" || len(r.Lanes) != 2 {
		t.Fatalf("race %+v", r)
	}
	for _, l := range r.Lanes {
		if l.State != agentrace.Starting || l.Worktree == "" || !strings.Contains(filepath.ToSlash(l.Worktree), "/.git/termocode/worktrees/"+l.ID) {
			t.Errorf("lane %+v", l)
		}
		if _, err := os.Stat(filepath.Join(l.Worktree, "a.txt")); err != nil {
			t.Errorf("lane %s has no checkout: %v", l.ID, err)
		}
		if _, err := os.Stat(filepath.Join(l.Worktree, "dirty.txt")); err == nil {
			t.Errorf("lane %s has the uncommitted file", l.ID)
		}
	}
	if !e.m.racePageOpen() || len(e.typed) != 0 {
		t.Errorf("page open %v, typed %v", e.m.racePageOpen(), e.typed)
	}
	if rs, err := agentrace.Load(filepath.Join(e.dir, ".git", "termocode")); err != nil || len(rs) != 1 || len(rs[0].Lanes) != 2 {
		t.Errorf("records %+v %v", rs, err)
	}
	buf := e.m.raceS().bufOf(r.Lanes[0].ID)
	state := func(st agents.State) {
		t.Helper()
		tab, _ := e.m.termTabByBuf(buf)
		tab.AgentState = st
		e.feed(e.m.raceOnAgentState(buf, st))
	}
	state(agents.Waiting) // "Do you trust this folder?": the user answers
	if len(e.typed[buf]) != 0 {
		t.Fatalf("typed into a question: %v", e.typed[buf])
	}
	state(agents.Done) // the input box
	if got := strings.Join(e.typed[buf], "|"); got != "fix the toast race|\r" {
		t.Errorf("typed %q", got)
	}
	if l := e.race(t).Lanes[0]; l.State != agentrace.Working || !l.Typed {
		t.Errorf("after typing %+v", l)
	}
	state(agents.Done) // the old input box, before the agent reacts
	if l := e.race(t).Lanes[0]; l.State != agentrace.Working {
		t.Errorf("done before any work: %s", l.State)
	}
	state(agents.Working)
	state(agents.Done)
	if l := e.race(t).Lanes[0]; l.State != agentrace.Done {
		t.Errorf("after the work: %s", l.State)
	}
	if n := len(e.typed[buf]); n != 2 {
		t.Errorf("typed %d times", n)
	}
	// A second race gets the next number; the first stays recorded.
	r2 := e.start(t, "again", "", "codex", "claude")
	if r2.ID != "race-2" || r2.Lanes[0].ID != "race-2-1-codex" {
		t.Errorf("second race %s %s", r2.ID, r2.Lanes[0].ID)
	}
	if rs, _ := agentrace.Load(filepath.Join(e.dir, ".git", "termocode")); len(rs) != 2 {
		t.Errorf("%d races recorded", len(rs))
	}
}

// Spec (AGX-4): x stops every lane that still works (Ctrl+C into its
// tab, once; a lane that was never given the task gets nothing typed);
// a lane whose tab was closed is stopped; the work stays in its
// worktree. A lane whose worktree cannot be made fails with the reason
// and the others still start.
func TestAgentRaceStopAndFailures(t *testing.T) {
	e := newRaceEnv(t, "claude", "codex", "gemini")
	r := e.start(t, "task", "", "claude", "codex", "gemini")
	b0, b1, b2 := e.m.raceS().bufOf(r.Lanes[0].ID), e.m.raceS().bufOf(r.Lanes[1].ID), e.m.raceS().bufOf(r.Lanes[2].ID)
	tab, _ := e.m.termTabByBuf(b0)
	tab.AgentState = agents.Done
	e.feed(e.m.raceOnAgentState(b0, agents.Done)) // lane 0 typed
	// Lane 2's tab is closed by the user.
	for i, tt := range e.m.terminalTabs {
		if tt.BufID == b2 {
			e.m.terminalTabs = append(e.m.terminalTabs[:i], e.m.terminalTabs[i+1:]...)
		}
	}
	e.feed(e.m.raceAct(raceview.ActionMsg{Action: raceview.ActStop}))
	r = e.race(t)
	for i, l := range r.Lanes {
		if l.State != agentrace.Stopped {
			t.Errorf("lane %d %s", i, l.State)
		}
	}
	if got := strings.Join(e.typed[b0], "|"); got != "task|\r|\x03" {
		t.Errorf("lane 0 typed %q", got)
	}
	if len(e.typed[b1]) != 0 || len(e.typed[b2]) != 0 {
		t.Errorf("typed into lanes without the task: %v %v", e.typed[b1], e.typed[b2])
	}
	if _, err := os.Stat(r.Lanes[0].Worktree); err != nil {
		t.Errorf("stop removed the worktree: %v", err)
	}

	// A worktree that cannot be made.
	old := raceCreateWorktree
	t.Cleanup(func() { raceCreateWorktree = old })
	raceCreateWorktree = func(dir, id string) (git.AgentWorktree, error) {
		if strings.HasSuffix(id, "-codex") {
			return git.AgentWorktree{}, os.ErrPermission
		}
		return git.CreateAgentWorktreeFromHead(dir, id)
	}
	r = e.start(t, "task two", "", "claude", "codex")
	if r.Lanes[1].State != agentrace.Failed || !strings.Contains(r.Lanes[1].Note, "permission") || r.Lanes[0].State != agentrace.Starting {
		t.Errorf("lanes %+v", r.Lanes)
	}
	if e.m.raceS().bufOf(r.Lanes[1].ID) != 0 {
		t.Error("a failed lane got a tab")
	}
}

// Spec (AGX-4): outside a git repository the card does not open; it says
// why (a lane needs its own worktree).
func TestAgentRaceNeedsRepo(t *testing.T) {
	covGitEnv(t)
	fakeAgentLookPath(t, "claude", "codex")
	m := covModelAt(t, t.TempDir())
	m.dispatchTasksPalette("agent-race")
	if m.raceFormOpen() {
		t.Error("the card opened outside a repository")
	}
}
