package agentrun

import (
	"context"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// OMX-6 / OMX-22: a message sent to a running step reaches its agent in
// the same turn (NodeMessage says it was read); a step that does not run
// refuses one.
func TestSendReachesARunningNode(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan struct{}, 8)
	pa := &fakeProv{replies: []ai.Chunk{{Delta: "working"}, {Delta: "done with the note"}}, gate: gate, started: started}
	h := newHarness(map[string]*fakeProv{"a": pa, "b": {}})
	r, err := Start(context.Background(), chain("a", "b"), h.options(1))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("a did not start")
	}
	if err := r.Send("b", "hi"); err == nil {
		t.Fatal("b does not run yet: Send must refuse")
	}
	if err := r.Send("a", "also check the tests"); err != nil {
		t.Fatal(err)
	}
	close(gate)
	if end := wait(t, r); end.Kind != RunDone {
		t.Fatalf("end %+v", end)
	}
	pa.mu.Lock()
	reqs := pa.reqs
	pa.mu.Unlock()
	if len(reqs) != 2 {
		t.Fatalf("a made %d requests, want 2", len(reqs))
	}
	if last := reqs[1][len(reqs[1])-1]; last.Role != ai.RoleUser || last.Content != SendPrefix+"also check the tests" {
		t.Fatalf("second request ends with %+v", last)
	}
	ev, ok := h.find(NodeMessage, "a")
	if !ok || ev.Output != "also check the tests" || ev.Err != "" {
		t.Fatalf("NodeMessage %+v %v", ev, ok)
	}
	if got := r.Output("a"); got != "done with the note" {
		t.Fatalf("a's output %q", got)
	}
	if err := r.Send("a", "late"); err == nil {
		t.Fatal("after the end Send must refuse")
	}
}

// A step takes messages from the moment it shows as running: the UI
// learns "running" from NodeStart, and a message sent right then was
// refused ("the step does not run now") when the mailbox opened after
// that event (NEW-3: TestAgentsSendMessageToStep on macOS).
func TestSendRightAtNodeStart(t *testing.T) {
	pa := &fakeProv{replies: []ai.Chunk{{Delta: "working"}, {Delta: "done with the note"}}}
	h := newHarness(map[string]*fakeProv{"a": pa})
	opt := h.options(1)
	started, sent := make(chan struct{}), make(chan struct{})
	emit := opt.Emit
	opt.Emit = func(e Event) {
		emit(e)
		if e.Kind == NodeStart && e.NodeID == "a" {
			close(started)
			<-sent // the loop waits here, right after the event
		}
	}
	r, err := Start(context.Background(), chain("a"), opt)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	err = r.Send("a", "also check the tests")
	close(sent)
	if err != nil {
		t.Fatalf("Send at NodeStart: %v", err)
	}
	if end := wait(t, r); end.Kind != RunDone {
		t.Fatalf("end %+v", end)
	}
	if ev, ok := h.find(NodeMessage, "a"); !ok || ev.Err != "" {
		t.Fatalf("NodeMessage %+v %v", ev, ok)
	}
}
