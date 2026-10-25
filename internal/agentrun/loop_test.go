package agentrun

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// OM-3: "work until done" — a failing verify adds the fix node and runs
// another round, up to the loop's max rounds and within the budget.

func loopRun(maxRounds int) agentdag.Run {
	return agentdag.Run{ID: "loop1",
		Nodes: []agentdag.Node{{ID: "do", Title: "Do"}, {ID: "verify", Title: "Verify", Category: agentdag.CategoryDeep}},
		Edges: agentdag.Chain("do", "verify"),
		Loop: &agentdag.Loop{Verify: "verify", MaxRounds: maxRounds,
			Fix: agentdag.Node{ID: "fix", Title: "Fix", Category: agentdag.CategoryQuick}}}
}

func roundsOf(h *harness) []int {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []int
	for _, e := range h.events {
		if e.Kind == RunRound {
			out = append(out, e.Round)
		}
	}
	return out
}

// Spec: verify FAIL → the fix node joins the run (RunPlanned), round 2
// starts, fix gets the report, verify runs again with fix's output and
// passes: the run ends with verdict pass.
func TestLoopVerifyFailThenPass(t *testing.T) {
	h := newHarness(map[string]*fakeProv{
		"do":     {replies: []ai.Chunk{{Delta: "did it"}}},
		"verify": {replies: []ai.Chunk{{Delta: "Ran tests.\nVERDICT: FAIL\na.go:3 nil map"}, {Delta: "All green.\nVERDICT: PASS"}}},
		"fix":    {replies: []ai.Chunk{{Delta: "fixed a.go:3"}}},
	})
	run, err := Start(context.Background(), loopRun(3), planOptions(h, 1))
	if err != nil {
		t.Fatal(err)
	}
	end := wait(t, run)
	if end.Kind != RunDone || end.Verdict != VerdictPass || end.Round != 2 || !strings.Contains(end.Output, "All green") {
		t.Fatalf("end %+v", end)
	}
	pl, ok := h.find(RunPlanned, "verify")
	if !ok || len(pl.Nodes) != 1 || pl.Nodes[0].ID != "fix" || pl.Nodes[0].Slot != "cheap" {
		t.Fatalf("planned %+v", pl)
	}
	if got := roundsOf(h); len(got) != 1 || got[0] != 2 {
		t.Errorf("rounds %v", got)
	}
	r, _ := h.find(RunRound, "")
	if r.Verdict != VerdictFail || !strings.Contains(r.Err, "a.go:3 nil map") {
		t.Errorf("round event %+v", r)
	}
	if st, _ := h.find(NodeStart, "fix"); st.Round != 2 {
		t.Errorf("fix start round %d", st.Round)
	}
	if st, _ := h.find(NodeStart, "do"); st.Round != 1 {
		t.Errorf("do start round %d", st.Round)
	}
	if !(h.index(RunPlanned, "verify") < h.index(RunRound, "") && h.index(RunRound, "") < h.index(NodeStart, "fix")) {
		t.Error("want RunPlanned, then RunRound, then fix's NodeStart")
	}
	if in := h.input("fix"); !strings.Contains(in, "### Verify report (round 1)\nRan tests.\na.go:3 nil map") || !strings.Contains(in, "fix the bug") {
		t.Errorf("fix input:\n%s", in)
	}
	if in := h.input("verify"); !strings.Contains(in, "### Output of step \"Fix\"\nfixed a.go:3") || !strings.Contains(in, "### Output of step \"Do\"\ndid it") {
		t.Errorf("verify input (round 2):\n%s", in)
	}
	if h.provs["do"].callCount() != 1 || h.provs["verify"].callCount() != 2 || h.provs["fix"].callCount() != 1 {
		t.Errorf("calls do %d verify %d fix %d", h.provs["do"].callCount(), h.provs["verify"].callCount(), h.provs["fix"].callCount())
	}
	snap := run.Snapshot()
	if got := states(snap); got != "do=completed verify=completed fix=completed" {
		t.Errorf("states %s", got)
	}
	if snap.Loop == nil || snap.Loop.Round != 2 {
		t.Errorf("snapshot loop %+v", snap.Loop)
	}
}

// Spec: verify keeps failing → fix runs MaxRounds-1 times and the run
// ends (done, not failed) with verdict fail and the last report.
func TestLoopStopsAtMaxRounds(t *testing.T) {
	h := newHarness(map[string]*fakeProv{
		"do":     {replies: []ai.Chunk{{Delta: "did it"}}},
		"verify": {replies: []ai.Chunk{{Delta: "VERDICT: FAIL\nstill broken"}}},
		"fix":    {replies: []ai.Chunk{{Delta: "tried"}}},
	})
	run, err := Start(context.Background(), loopRun(3), planOptions(h, 1))
	if err != nil {
		t.Fatal(err)
	}
	end := wait(t, run)
	if end.Kind != RunDone || end.Verdict != VerdictFail || end.Round != 3 || !strings.Contains(end.Output, "still broken") {
		t.Fatalf("end %+v", end)
	}
	if n := h.provs["fix"].callCount(); n != 2 {
		t.Errorf("fix ran %d times, want 2", n)
	}
	if n := h.provs["verify"].callCount(); n != 3 {
		t.Errorf("verify ran %d times, want 3", n)
	}
	if got := roundsOf(h); len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Errorf("rounds %v", got)
	}
	n := 0
	for _, k := range h.kinds("verify") {
		if k == RunPlanned {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the fix node was added %d times", n)
	}
	if in := h.input("fix"); !strings.Contains(in, "### Verify report (round 2)") {
		t.Errorf("fix input in round 3:\n%s", in)
	}
}

// Spec: a verify answer without a VERDICT line gets one repair request;
// when it is still missing it counts as a fail and says so.
func TestLoopMissingVerdict(t *testing.T) {
	h := newHarness(map[string]*fakeProv{
		"do":     {replies: []ai.Chunk{{Delta: "did it"}}},
		"verify": {replies: []ai.Chunk{{Delta: "looks fine"}, {Delta: "VERDICT: PASS"}}},
	})
	run, err := Start(context.Background(), loopRun(3), planOptions(h, 1))
	if err != nil {
		t.Fatal(err)
	}
	if end := wait(t, run); end.Verdict != VerdictPass || end.Round != 1 {
		t.Fatalf("repaired: end %+v", end)
	}
	if rep, ok := h.find(NodeRepair, "verify"); !ok || rep.Verdict != VerdictMissing {
		t.Errorf("repair %+v", rep)
	}

	h = newHarness(map[string]*fakeProv{
		"do":     {replies: []ai.Chunk{{Delta: "did it"}}},
		"verify": {replies: []ai.Chunk{{Delta: "looks fine"}}},
		"fix":    {replies: []ai.Chunk{{Delta: "f"}}},
	})
	run, err = Start(context.Background(), loopRun(1), planOptions(h, 1))
	if err != nil {
		t.Fatal(err)
	}
	if end := wait(t, run); end.Kind != RunDone || end.Verdict != VerdictFail {
		t.Fatalf("still missing: end %+v", end)
	}
	if d, _ := h.find(NodeDone, "verify"); d.Verdict != VerdictMissing {
		t.Errorf("verify done %+v", d)
	}
	if n := h.provs["verify"].callCount(); n != 2 {
		t.Errorf("verify requests %d (answer + one repair)", n)
	}
}

// Spec: the budget pause also comes between rounds: spent adds up over
// the rounds; Continue runs round 2.
func TestLoopBudgetPausesBetweenRounds(t *testing.T) {
	h := newHarness(map[string]*fakeProv{
		"do":     {replies: []ai.Chunk{{Delta: "did it", Usage: usage(40, 1)}}},
		"verify": {replies: []ai.Chunk{{Delta: "VERDICT: FAIL\nx.go:1", Usage: usage(70, 1)}, {Delta: "VERDICT: PASS", Usage: usage(10, 1)}}},
		"fix":    {replies: []ai.Chunk{{Delta: "fixed", Usage: usage(10, 1)}}},
	})
	o := budgetOptions(h, 1.0)
	o.Categories = map[string]string{agentdag.CategoryQuick: "cheap", agentdag.CategoryDeep: "strong"}
	run, err := Start(context.Background(), loopRun(3), o)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pause", func() bool { _, ok := h.find(RunPaused, ""); return ok })
	time.Sleep(30 * time.Millisecond)
	if n := h.provs["fix"].callCount(); n != 0 {
		t.Fatalf("fix ran while paused (%d)", n)
	}
	if h.index(RunRound, "") < 0 || h.index(RunPaused, "") < h.index(RunRound, "") {
		t.Error("the pause must come after the round started, before fix")
	}
	run.Continue()
	end := wait(t, run)
	if end.Kind != RunDone || end.Verdict != VerdictPass || end.Round != 2 {
		t.Fatalf("end %+v", end)
	}
}

// Spec: Cancel during fix ends the run cancelled; verify does not run.
func TestLoopCancelDuringFix(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan struct{}, 1)
	h := newHarness(map[string]*fakeProv{
		"do":     {replies: []ai.Chunk{{Delta: "did it"}}},
		"verify": {replies: []ai.Chunk{{Delta: "VERDICT: FAIL\nx"}}},
		"fix":    {replies: []ai.Chunk{{Delta: "never"}}, gate: gate, started: started},
	})
	run, err := Start(context.Background(), loopRun(3), planOptions(h, 1))
	if err != nil {
		t.Fatal(err)
	}
	<-started
	run.Cancel()
	end := wait(t, run)
	if end.Kind != RunCancelled {
		t.Fatalf("end %+v", end)
	}
	if n := h.provs["verify"].callCount(); n != 1 {
		t.Errorf("verify ran %d times", n)
	}
	if got := states(run.Snapshot()); got != "do=completed verify=cancelled fix=cancelled" {
		t.Errorf("states %s", got)
	}
}

// Spec (OM-6 + OM-3): a resumed run keeps its round: it starts in the
// saved round with the fix node and the saved report.
func TestLoopResumeKeepsRound(t *testing.T) {
	h := newHarness(map[string]*fakeProv{
		"do":     {replies: []ai.Chunk{{Delta: "new"}}},
		"verify": {replies: []ai.Chunk{{Delta: "VERDICT: FAIL\nagain"}}},
		"fix":    {replies: []ai.Chunk{{Delta: "fixed"}}},
	})
	r := loopRun(3)
	r.Nodes[0].State = agentdag.StateCompleted
	r.Nodes = append(r.Nodes, agentdag.Node{ID: "fix", Title: "Fix", Category: agentdag.CategoryQuick, Slot: "cheap"})
	r.Edges = append(r.Edges, agentdag.Edge{From: "fix", To: "verify"})
	r.Loop.Round, r.Loop.Report = 2, "a.go:3 nil map"
	o := planOptions(h, 1)
	o.Outputs = map[string]string{"do": "saved"}
	run, err := Start(context.Background(), r, o)
	if err != nil {
		t.Fatal(err)
	}
	end := wait(t, run)
	if end.Kind != RunDone || end.Verdict != VerdictFail || end.Round != 3 {
		t.Fatalf("end %+v", end)
	}
	if h.provs["do"].callCount() != 0 || h.provs["verify"].callCount() != 2 || h.provs["fix"].callCount() != 2 {
		t.Errorf("calls do %d verify %d fix %d", h.provs["do"].callCount(), h.provs["verify"].callCount(), h.provs["fix"].callCount())
	}
	if _, ok := h.find(RunPlanned, "verify"); ok {
		t.Error("the fix node was added again")
	}
	if st, _ := h.find(NodeStart, "fix"); st.Round != 2 {
		t.Errorf("first fix start round %d", st.Round)
	}
	if got := roundsOf(h); len(got) != 1 || got[0] != 3 {
		t.Errorf("rounds %v", got)
	}
}
