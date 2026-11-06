package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/raceview"
)

// finishLane plays a lane's agent: the task is typed at the input box,
// the agent works, writes files into its worktree and is back at the box.
func (e *raceEnv) finishLane(t *testing.T, i int, files map[string]string) {
	t.Helper()
	l := e.race(t).Lanes[i]
	for name, body := range files {
		covWrite(t, l.Worktree, name, body)
	}
	buf := e.m.raceS().bufOf(l.ID)
	for _, st := range []agents.State{agents.Done, agents.Working, agents.Done} {
		if tab, _ := e.m.termTabByBuf(buf); tab != nil {
			tab.AgentState = st
		}
		e.feed(e.m.raceOnAgentState(buf, st))
	}
	if got := e.race(t).Lanes[i].State; got != agentrace.Done {
		t.Fatalf("lane %d %s", i, got)
	}
}

func (e *raceEnv) act(a raceview.Action, lane string) { e.feed(e.m.raceAct(raceview.ActionMsg{Action: a, Lane: lane})) }

// answer presses a button of the open card.
func (e *raceEnv) answer(id string) {
	e.m.confirmOpen = false
	cmd, _ := e.m.handleAIConfirm(id)
	e.feed(cmd)
}

func branchExists(t *testing.T, dir, branch string) bool {
	t.Helper()
	return covGit(t, dir, "branch", "--list", branch) != ""
}

// Spec (AGX-5, plan 2.5): a finished lane's files and +/- show on the
// page (read after the lane ends); m merges the selected lane into your
// files with the existing merge, removes its worktree and branch, then a
// card asks about the other lanes with Keep first and focused: Keep
// leaves them, Discard removes their worktrees and keeps a branch that
// has work on it.
func TestAgentRaceMergeWinner(t *testing.T) {
	for _, discard := range []bool{false, true} {
		e := newRaceEnv(t, "claude", "codex", "gemini")
		r := e.start(t, "change a", "", "claude", "codex", "gemini")
		e.finishLane(t, 0, map[string]string{"a.txt": "one\nTWO\nthree\n", "new.go": "package x\n"})
		e.finishLane(t, 1, map[string]string{"a.txt": "ONE\ntwo\nthree\n"})
		l0 := e.race(t).Lanes[0]
		if f, a, d := l0.Totals(); !l0.Stats || f != 2 || a != 2 || d != 1 {
			t.Fatalf("lane 0 stats %v %d +%d -%d", l0.Stats, f, a, d)
		}
		page := ansi.Strip(e.m.raceS().page.Box())
		if !strings.Contains(page, "+2 -1") || !strings.Contains(page, "new.go (new)") {
			t.Errorf("page:\n%s", page)
		}
		e.act(raceview.ActMerge, l0.ID)
		if got := covGit(t, e.dir, "show", ":a.txt"); got != "one\ntwo\nthree" {
			t.Errorf("the merge touched the index: %q", got)
		}
		if b, _ := os.ReadFile(filepath.Join(e.dir, "a.txt")); string(b) != "one\nTWO\nthree\n" {
			t.Errorf("a.txt %q", b)
		}
		r = e.race(t)
		if r.Lanes[0].State != agentrace.Merged || r.Winner != l0.ID {
			t.Errorf("lane 0 %s winner %q", r.Lanes[0].State, r.Winner)
		}
		if _, err := os.Stat(l0.Worktree); err == nil || branchExists(t, e.dir, l0.Branch) {
			t.Errorf("the winner's worktree or branch is still there")
		}
		if !e.m.confirmOpen || e.m.confirmKind != confirmKindRaceOthers || e.m.confirm.SafeID() != "keep" {
			t.Fatalf("no Keep-first card: %v %v %q", e.m.confirmOpen, e.m.confirmKind, e.m.confirm.SafeID())
		}
		// Lane 2 still works: it is not offered (never discarded under its agent).
		if box := ansi.Strip(e.m.confirm.Box()); !strings.Contains(box, "Discard the other lane") || !strings.Contains(box, "Gemini CLI still works") {
			t.Errorf("card:\n%s", box)
		}
		l1, l2 := r.Lanes[1], r.Lanes[2]
		if !discard {
			e.answer("keep")
			if r := e.race(t); r.Lanes[1].State != agentrace.Done || r.Lanes[2].State == agentrace.Discarded {
				t.Errorf("Keep changed the lanes: %s %s", r.Lanes[1].State, r.Lanes[2].State)
			}
			continue
		}
		e.answer("discard")
		r = e.race(t)
		if r.Lanes[1].State != agentrace.Discarded {
			t.Errorf("lane 1 %s", r.Lanes[1].State)
		}
		if _, err := os.Stat(l1.Worktree); err == nil {
			t.Error("lane 1's worktree is still there")
		}
		if !branchExists(t, e.dir, l1.Branch) {
			t.Error("lane 1's branch with work on it was removed")
		}
		// Lane 2 was still working: a working lane is never discarded under the agent.
		if r.Lanes[2].State == agentrace.Discarded {
			t.Errorf("a working lane was discarded")
		}
		if _, err := os.Stat(l2.Worktree); err != nil {
			t.Errorf("the working lane's worktree went: %v", err)
		}
	}
}

// Spec (AGX-5): a merge where you changed the same file stops before any
// file is written and opens the conflict card (Cancel first); Merge with
// markers writes both; d asks first (Cancel first) and Discard removes
// the lane's worktree and branch.
func TestAgentRaceConflictAndDiscard(t *testing.T) {
	e := newRaceEnv(t, "claude", "codex")
	e.start(t, "change a", "", "claude", "codex")
	e.finishLane(t, 0, map[string]string{"a.txt": "one\nTWO\nthree\n"})
	e.finishLane(t, 1, map[string]string{"b.txt": "bee\n"})
	covWrite(t, e.dir, "a.txt", "one\ntwo\nmine\n")
	l0, l1 := e.race(t).Lanes[0], e.race(t).Lanes[1]
	e.act(raceview.ActMerge, l0.ID)
	if !e.m.confirmOpen || e.m.confirmKind != confirmKindRaceConflict || e.m.confirm.SafeID() != "cancel" {
		t.Fatalf("no conflict card: %v %v", e.m.confirmOpen, e.m.confirmKind)
	}
	if b, _ := os.ReadFile(filepath.Join(e.dir, "a.txt")); string(b) != "one\ntwo\nmine\n" {
		t.Errorf("written before the card: %q", b)
	}
	e.answer("cancel")
	if e.race(t).Lanes[0].State != agentrace.Done {
		t.Errorf("Cancel changed the lane: %s", e.race(t).Lanes[0].State)
	}
	e.act(raceview.ActMerge, l0.ID)
	e.answer("markers")
	if b, _ := os.ReadFile(filepath.Join(e.dir, "a.txt")); !strings.Contains(string(b), "<<<<<<<") {
		t.Errorf("no markers: %q", b)
	}
	if st := e.race(t).Lanes[0].State; st != agentrace.Merged {
		t.Errorf("lane 0 %s", st)
	}
	e.answer("keep") // the other-lanes card
	e.act(raceview.ActDiscard, l1.ID)
	if !e.m.confirmOpen || e.m.confirmKind != confirmKindRaceDiscard || e.m.confirm.SafeID() != "cancel" {
		t.Fatalf("no discard card")
	}
	e.answer("cancel")
	if _, err := os.Stat(l1.Worktree); err != nil {
		t.Fatalf("Cancel discarded: %v", err)
	}
	e.act(raceview.ActDiscard, l1.ID)
	e.answer("discard")
	if _, err := os.Stat(l1.Worktree); err == nil || branchExists(t, e.dir, l1.Branch) {
		t.Error("discard left the worktree or the branch")
	}
	if st := e.race(t).Lanes[1].State; st != agentrace.Discarded {
		t.Errorf("lane 1 %s", st)
	}
}

// Spec (AGX-5): Enter opens the lane's diff (the read-only review card);
// v runs the verify command in every finished lane (one task tab each,
// in the lane's worktree) and the exit code is the result; without a
// verify command v says so.
func TestAgentRaceReviewAndVerify(t *testing.T) {
	e := newRaceEnv(t, "claude", "codex")
	e.start(t, "change a", "go test ./...", "claude", "codex")
	e.finishLane(t, 0, map[string]string{"a.txt": "x\n"})
	l0 := e.race(t).Lanes[0]
	cmd := e.m.raceAct(raceview.ActionMsg{Action: raceview.ActReview, Lane: l0.ID})
	msg, ok := cmd().(aiWorktreeReviewMsg)
	if !ok || msg.err != nil || len(msg.files) != 1 || !strings.Contains(msg.title, "Claude Code") {
		t.Fatalf("review %+v", msg)
	}
	type job struct{ dir, line string }
	var jobs []job
	old := raceSpawnVerify
	t.Cleanup(func() { raceSpawnVerify = old })
	next := 500
	raceSpawnVerify = func(_ *Model, l agentrace.Lane, line string) (int, error) {
		jobs = append(jobs, job{l.Worktree, line})
		next++
		return next, nil
	}
	e.act(raceview.ActVerify, "")
	if len(jobs) != 1 || jobs[0].dir != l0.Worktree || jobs[0].line != "go test ./..." {
		t.Fatalf("jobs %+v (only the finished lane)", jobs)
	}
	if v := e.race(t).Lanes[0].Verify; v.State != agentrace.VerifyRunning {
		t.Errorf("verify %+v", v)
	}
	if c, ok := e.m.raceOnVerifyExit(next, 1); !ok {
		t.Fatal("verify exit not ours")
	} else {
		e.feed(c)
	}
	if v := e.race(t).Lanes[0].Verify; v.State != agentrace.VerifyFail || v.Exit != 1 {
		t.Errorf("verify %+v", v)
	}
	if _, ok := e.m.raceOnVerifyExit(9999, 0); ok {
		t.Error("another task's exit was taken")
	}
	e2 := newRaceEnv(t, "claude", "codex")
	e2.start(t, "no verify", "", "claude", "codex")
	jobs = nil
	e2.act(raceview.ActVerify, "")
	if len(jobs) != 0 {
		t.Errorf("verify without a command ran %v", jobs)
	}
}
