package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/raceview"
)

// Spec (CMP-56): b on a finished lane opens a Cancel-first card; Cancel
// changes nothing. "Squash into one commit" asks for the message (the
// task, prefilled) and then puts the lane's work on top of your current
// commit as one clean commit; your branch and files do not change, and
// the lane's files are read again.
func TestAgentRaceRestackSquash(t *testing.T) {
	e := newRaceEnv(t, "claude", "codex")
	e.start(t, "change a", "", "claude", "codex")
	e.finishLane(t, 0, map[string]string{"a.txt": "one\nTWO\nthree\n", "new.go": "package x\n"})
	covWrite(t, e.dir, "c.txt", "sea\n")
	covCommit(t, e.dir, "user moves on")
	head := covGit(t, e.dir, "rev-parse", "HEAD")
	l0 := e.race(t).Lanes[0]

	e.act(raceview.ActRestack, l0.ID)
	if !e.m.confirmOpen || e.m.confirmKind != confirmKindRaceRestack || e.m.confirm.SafeID() != "cancel" {
		t.Fatalf("no Cancel-first restack card: %v %v %q", e.m.confirmOpen, e.m.confirmKind, e.m.confirm.SafeID())
	}
	if box := ansi.Strip(e.m.confirm.Box()); !strings.Contains(box, l0.Branch) || !strings.Contains(box, "Your files") {
		t.Errorf("card:\n%s", box)
	}
	e.answer("cancel")
	if got := covGit(t, e.dir, "rev-parse", l0.Branch+"^"); got == head {
		t.Fatal("Cancel restacked the lane")
	}

	e.act(raceview.ActRestack, l0.ID)
	e.answer("squash")
	if !e.m.promptOpen || e.m.promptKind != promptKindRaceRestackMsg {
		t.Fatalf("no message prompt: %v %v", e.m.promptOpen, e.m.promptKind)
	}
	if v := e.m.prompt.Value(); v != "change a" {
		t.Errorf("prefilled message %q", v)
	}
	e.m.promptOpen = false
	cmd, ok := e.m.handleAIPromptSubmit("feat: change a")
	if !ok {
		t.Fatal("the restack prompt is not routed")
	}
	e.feed(cmd)
	if p := covGit(t, e.dir, "rev-parse", l0.Branch+"^"); p != head {
		t.Errorf("the lane's clean commit sits on %s, not on your HEAD %s", p, head)
	}
	if msg := covGit(t, e.dir, "log", "-1", "--format=%s", l0.Branch); msg != "feat: change a" {
		t.Errorf("message %q", msg)
	}
	if got := covGit(t, e.dir, "rev-parse", "HEAD"); got != head {
		t.Errorf("your HEAD moved to %s", got)
	}
	l := e.race(t).Lanes[0]
	if l.State != agentrace.Done || !strings.Contains(l.Note, "restacked") {
		t.Errorf("lane %s note %q", l.State, l.Note)
	}
	if f, _, _ := l.Totals(); f != 2 {
		t.Errorf("lane files after restack %d (c.txt must not count)", f)
	}
}

// Spec (CMP-56): a lane that clashes with your commit is left as it was
// and its note says which files clash.
func TestAgentRaceRestackConflict(t *testing.T) {
	e := newRaceEnv(t, "claude", "codex")
	e.start(t, "change a", "", "claude", "codex")
	e.finishLane(t, 0, map[string]string{"a.txt": "one\nTWO\nthree\n"})
	covWrite(t, e.dir, "a.txt", "one\nMINE\nthree\n")
	covCommit(t, e.dir, "user edits a")
	l0 := e.race(t).Lanes[0]
	e.act(raceview.ActRestack, l0.ID)
	e.answer("keep")
	if p := covGit(t, e.dir, "rev-parse", l0.Branch+"^"); p == covGit(t, e.dir, "rev-parse", "HEAD") {
		t.Error("a clashing lane was restacked")
	}
	if l := e.race(t).Lanes[0]; l.State != agentrace.Done || !strings.Contains(l.Note, "a.txt") {
		t.Errorf("lane %s note %q (the clash must be named)", l.State, l.Note)
	}
}
