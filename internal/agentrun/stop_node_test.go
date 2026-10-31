package agentrun

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// OMX-22: the user stops one step; the steps beside it go on, the steps
// after it cannot run, and the run ends as failed with the reason.
func TestStopNodeStopsOneStep(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan struct{}, 8)
	h := newHarness(map[string]*fakeProv{
		"a": {replies: []ai.Chunk{{Delta: "a"}}},
		"b": {replies: []ai.Chunk{{Delta: "b"}}, gate: gate, started: started},
		"c": {replies: []ai.Chunk{{Delta: "c"}}},
		"d": {replies: []ai.Chunk{{Delta: "d"}}},
	})
	r := agentdag.Run{ID: "run2"}
	for _, id := range []string{"a", "b", "c", "d"} {
		r.Nodes = append(r.Nodes, agentdag.Node{ID: id, Title: strings.ToUpper(id)})
	}
	r.Edges = []agentdag.Edge{{From: "a", To: "b"}, {From: "a", To: "c"}, {From: "b", To: "d"}}
	run, err := Start(context.Background(), r, h.options(2))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("b did not start")
	}
	if err := run.StopNode("d"); err == nil {
		t.Fatal("d does not run: StopNode must refuse")
	}
	if err := run.StopNode("b"); err != nil {
		t.Fatal(err)
	}
	end := wait(t, run)
	if end.Kind != RunFailed || !strings.Contains(end.Err, `"B"`) || !strings.Contains(end.Err, "stopped") {
		t.Fatalf("end %+v", end)
	}
	if got := states(run.Snapshot()); got != "a=completed b=cancelled c=completed d=blocked" {
		t.Fatalf("states %s", got)
	}
	if ev, ok := h.find(NodeCancelled, "b"); !ok || ev.Err == "" {
		t.Fatalf("b's NodeCancelled %+v %v", ev, ok)
	}
}
