package agentrun

import (
	"context"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// OMX-16: a step that answers "ESCALATE: why" runs once more on the
// escalation category's slot; a step there already keeps its answer.
func TestEscalateRunsTheStepOnDeep(t *testing.T) {
	h := newHarness(map[string]*fakeProv{"a": {replies: []ai.Chunk{{Delta: "ESCALATE: the race needs a stronger model"}, {Delta: "fixed the race"}}}})
	o := h.options(1)
	o.Categories = map[string]string{"quick": "quick", "deep": "main"}
	o.Escalate = "deep"
	r := chain("a")
	r.Nodes[0].Category, r.Nodes[0].Slot = "quick", "quick"
	run, err := Start(context.Background(), r, o)
	if err != nil {
		t.Fatal(err)
	}
	end := wait(t, run)
	if end.Kind != RunDone || end.Output != "fixed the race" {
		t.Fatalf("end %+v", end)
	}
	ev, ok := h.find(NodeEscalated, "a")
	if !ok || ev.Err != "the race needs a stronger model" || len(ev.Nodes) != 1 || ev.Nodes[0].Slot != "main" || ev.Nodes[0].Category != "deep" {
		t.Fatalf("NodeEscalated %+v %v", ev, ok)
	}
	if n := run.Snapshot().Nodes[0]; n.Category != "deep" || n.Slot != "main" {
		t.Fatalf("node %+v", n)
	}

	h2 := newHarness(map[string]*fakeProv{"a": {replies: []ai.Chunk{{Delta: "ESCALATE: still hard"}}}})
	o2 := h2.options(1)
	o2.Categories, o2.Escalate = o.Categories, "deep"
	r2 := chain("a")
	r2.Nodes[0].Category, r2.Nodes[0].Slot = "deep", "main"
	run2, _ := Start(context.Background(), r2, o2)
	if end := wait(t, run2); end.Kind != RunDone || end.Output != "ESCALATE: still hard" {
		t.Fatalf("deep step: %+v", end)
	}
	if _, ok := h2.find(NodeEscalated, "a"); ok {
		t.Fatal("a deep step escalated")
	}
}
