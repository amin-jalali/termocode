package run

import (
	"context"
	"slices"
	"testing"
	"time"
)

// The user's pause (the view's p): the step that works now finishes, no
// new step starts until Continue; the run file says held; a cancel
// while held ends the run cancelled.

// gated is a Runner whose step "one" waits for release.
type gated struct {
	*fakes
	started chan struct{}
	release chan struct{}
}

func (g *gated) Run(ctx context.Context, c RunCall) (Output, error) {
	if stepKey(c.Step) == "one" {
		g.started <- struct{}{}
		<-g.release
	}
	return g.fakes.Run(ctx, c)
}

const holdFile = `{ "version": 1, "name": "hold", "steps": [
  { "id": "one", "type": "shell", "argv": ["x"] },
  { "id": "two", "type": "shell", "argv": ["x"] } ] }`

func startHeld(t *testing.T) (*Handle, *gated, *fakes, *events) {
	t.Helper()
	f := newFakes()
	g := &gated{fakes: f, started: make(chan struct{}, 1), release: make(chan struct{})}
	ev := &events{}
	opts := f.options()
	opts.Run, opts.OnEvent = g, ev.on
	h := Start(context.Background(), mustParse(t, holdFile), opts)
	select {
	case <-g.started:
	case <-time.After(5 * time.Second):
		t.Fatal("step one did not start")
	}
	h.Pause()
	close(g.release)
	return h, g, f, ev
}

func TestPauseHoldsTheNextStep(t *testing.T) {
	h, _, f, ev := startHeld(t)
	if !h.Held() || !h.Snapshot().Held {
		t.Fatal("not held")
	}
	deadline := time.Now().Add(5 * time.Second)
	for h.Snapshot().Steps["one"].State != StateDone {
		if time.Now().After(deadline) {
			t.Fatal("step one did not finish while held")
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	for _, c := range f.all() {
		if c == `run two prompt="" wt=` {
			t.Fatal("step two started while held")
		}
	}
	h.Continue()
	rf := wait(t, h)
	if rf.State != RunOK || rf.Held {
		t.Fatalf("state %s held %v", rf.State, rf.Held)
	}
	k := ev.kinds()
	if !slices.Contains(k, string(RunHeld)) || !slices.Contains(k, string(RunContinued)) {
		t.Errorf("events %v", k)
	}
}

func TestCancelWhileHeld(t *testing.T) {
	h, _, _, _ := startHeld(t)
	h.Cancel()
	if rf := wait(t, h); rf.State != RunCancel {
		t.Fatalf("state %s", rf.State)
	}
	h.Pause() // after the end: nothing
	h.Continue()
}
