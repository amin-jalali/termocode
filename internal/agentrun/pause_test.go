package agentrun

import (
	"context"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (Runs page, Pause): Pause lets the running node finish, then the
// run pauses before the next one (RunPaused with Budget 0); Continue
// runs the rest.
func TestPauseBeforeNextNodeThenContinue(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan struct{}, 1)
	h := newHarness(map[string]*fakeProv{
		"a": {replies: []ai.Chunk{{Delta: "a"}}, gate: gate, started: started},
		"b": {replies: []ai.Chunk{{Delta: "b"}}},
	})
	run, err := Start(context.Background(), chain("a", "b"), h.options(1))
	if err != nil {
		t.Fatal(err)
	}
	<-started
	run.Pause()
	close(gate) // a finishes after the pause was asked
	waitFor(t, "the pause", func() bool { _, ok := h.find(RunPaused, ""); return ok })
	if p, _ := h.find(RunPaused, ""); p.Budget != 0 {
		t.Errorf("a pause the user asked for says budget %v", p.Budget)
	}
	time.Sleep(30 * time.Millisecond)
	if n := h.provs["b"].callCount(); n != 0 {
		t.Fatalf("b ran while paused (%d)", n)
	}
	if got := states(run.Snapshot()); got != "a=completed b=pending" {
		t.Errorf("states while paused %s", got)
	}
	run.Continue()
	if end := wait(t, run); end.Kind != RunDone || end.Output != "b" {
		t.Fatalf("end %+v", end)
	}
	if _, ok := h.find(RunResumed, ""); !ok {
		t.Error("no resumed event")
	}
}

// Spec: Cancel on a run the user paused cancels what did not run.
func TestPauseThenStop(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan struct{}, 1)
	h := newHarness(map[string]*fakeProv{
		"a": {replies: []ai.Chunk{{Delta: "a"}}, gate: gate, started: started},
		"b": {replies: []ai.Chunk{{Delta: "b"}}},
	})
	run, err := Start(context.Background(), chain("a", "b"), h.options(1))
	if err != nil {
		t.Fatal(err)
	}
	<-started
	run.Pause()
	close(gate)
	waitFor(t, "the pause", func() bool { _, ok := h.find(RunPaused, ""); return ok })
	run.Cancel()
	if end := wait(t, run); end.Kind != RunCancelled {
		t.Fatalf("end %+v", end)
	}
	if got := states(run.Snapshot()); got != "a=completed b=cancelled" {
		t.Errorf("states %s", got)
	}
}
