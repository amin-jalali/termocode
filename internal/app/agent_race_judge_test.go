package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/raceview"
)

// Spec (CMP-9): p on the compare page asks the main model to rank the
// finished lanes (diffs capped and redacted, verify results included).
// The ranking, the pick and the cost are stored with the race and shown.
// Then a card offers to merge the pick with Cancel first: Cancel changes
// nothing; Merge runs the normal merge of that lane. Without a main
// model nothing is sent and the page says how to set one.
func TestAgentRaceJudge(t *testing.T) {
	for _, merge := range []bool{false, true} {
		e := newRaceEnv(t, "claude", "codex")
		e.start(t, "change a", "", "claude", "codex")
		e.finishLane(t, 0, map[string]string{"a.txt": "one\nTWO\nthree\n", "key.txt": "token sk-" + "ant-api03-abcdefghijklmnopqrstuvwxyz0123456789\n"})
		e.finishLane(t, 1, map[string]string{"b.txt": "BEE\n"})

		oldSlot := raceJudgeSlot
		t.Cleanup(func() { raceJudgeSlot = oldSlot })
		raceJudgeSlot = func(*aiState) (aiSlot, error) { return aiSlot{}, ai.ErrNotConfigured }
		e.act(raceview.ActJudge, "")
		if page := ansi.Strip(e.m.raceS().page.Box()); !strings.Contains(page, "set a main model") {
			t.Errorf("no hint:\n%s", page)
		}
		raceJudgeSlot = oldSlot

		p := &raceRecProvider{reply: "RANK 1: Codex | small and complete\nRANK 2: Claude Code | also edits a key file\nPICK: Codex\n"}
		e.m.aiS().slotFake = map[string]aiSlot{ai.SlotMain: {p: p, cfg: ai.Config{Kind: ai.KindOllama, Model: "fake-1"}}}
		e.act(raceview.ActJudge, "")
		if len(p.reqs) != 1 {
			t.Fatalf("%d requests", len(p.reqs))
		}
		var all strings.Builder
		for _, m := range p.reqs[0].Messages {
			all.WriteString(m.Content)
		}
		if sent := all.String(); !strings.Contains(sent, "Lane Claude Code") || !strings.Contains(sent, "+BEE") || strings.Contains(sent, "abcdefghijklmnop") {
			t.Errorf("prompt:\n%s", sent)
		}
		r := e.race(t)
		codex := r.Lanes[1]
		if r.Judge == nil || r.Judge.Pick != codex.ID || len(r.Judge.Ranks) != 2 || !strings.Contains(r.Judge.Cost, "fake-1") {
			t.Fatalf("judge %+v", r.Judge)
		}
		if page := ansi.Strip(e.m.raceS().page.Box()); !strings.Contains(page, "JUDGE") || !strings.Contains(page, "small and complete") {
			t.Errorf("page:\n%s", page)
		}
		rs, _ := agentrace.Load(filepath.Join(e.dir, ".git", "termocode"))
		if len(rs) != 1 || rs[0].Judge == nil || rs[0].Judge.Pick != codex.ID {
			t.Errorf("not stored: %+v", rs)
		}
		if !e.m.confirmOpen || e.m.confirmKind != confirmKindRaceJudgePick || e.m.confirm.SafeID() != "cancel" {
			t.Fatalf("no Cancel-first card: open %v kind %v", e.m.confirmOpen, e.m.confirmKind)
		}
		if !merge {
			e.answer("cancel")
			if got := e.race(t).Lanes[1].State; got != agentrace.Done {
				t.Errorf("cancel changed the lane: %s", got)
			}
			if fileExists(filepath.Join(e.dir, "b.txt")) {
				t.Error("cancel merged")
			}
			continue
		}
		e.answer("merge")
		if got := e.race(t).Lanes[1].State; got != agentrace.Merged {
			t.Errorf("merge: lane %s", got)
		}
		if !fileExists(filepath.Join(e.dir, "b.txt")) {
			t.Error("b.txt not merged")
		}
	}
}

// Spec (CMP-9): judging needs two finished lanes; the answer "PICK:
// none" stores the ranking and opens no card.
func TestAgentRaceJudgeNoPick(t *testing.T) {
	e := newRaceEnv(t, "claude", "codex")
	e.start(t, "change a", "", "claude", "codex")
	e.finishLane(t, 0, map[string]string{"a.txt": "x\n"})
	p := &raceRecProvider{reply: "RANK 1: Codex | wrong\nRANK 2: Claude Code | wrong\nPICK: none"}
	e.m.aiS().slotFake = map[string]aiSlot{ai.SlotMain: {p: p, cfg: ai.Config{Kind: ai.KindOllama, Model: "fake-1"}}}
	e.act(raceview.ActJudge, "")
	if len(p.reqs) != 0 || !strings.Contains(ansi.Strip(e.m.raceS().page.Box()), "two finished lanes") {
		t.Fatalf("sent %d", len(p.reqs))
	}
	e.finishLane(t, 1, map[string]string{"b.txt": "y\n"})
	e.act(raceview.ActJudge, "")
	if r := e.race(t); r.Judge == nil || r.Judge.Pick != "" || e.m.confirmOpen {
		t.Errorf("judge %+v card %v", r.Judge, e.m.confirmOpen)
	}
	if !strings.Contains(ansi.Strip(e.m.raceS().page.Box()), "no lane") {
		t.Errorf("page:\n%s", ansi.Strip(e.m.raceS().page.Box()))
	}
}
