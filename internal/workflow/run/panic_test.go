package run

import (
	"context"
	"testing"
)

// panicRunner panics in every run step.
type panicRunner struct{}

func (panicRunner) Run(ctx context.Context, c RunCall) (Output, error) { panic("adapter bug") }

func TestPanickingAdapterEndsTheRunFailed(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir()) // the crash report goes here
	fk := newFakes()
	var ev events
	var st memStore
	opts := fk.options()
	opts.Run, opts.OnEvent, opts.Store = panicRunner{}, ev.on, &st
	h := Start(context.Background(), mustParse(t, `{"version": 1, "name": "w", "steps": [{"id": "t", "type": "tests", "task": "test"}]}`), opts)
	rf := wait(t, h)
	if rf.State != RunFail || rf.Error != "the engine stopped on an internal error" {
		t.Fatalf("state %q %q", rf.State, rf.Error)
	}
	if k := ev.kinds(); k[len(k)-1] != "run_failed" || st.last(t).State != RunFail {
		t.Fatalf("events %q", k)
	}
	h.Snapshot() // the lock is free
}
