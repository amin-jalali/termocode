package agentrun

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// silentProv never answers its first `silent` requests (they wait for
// their context); later ones answer "ok".
type silentProv struct {
	silent int32
	calls  atomic.Int32
}

func (p *silentProv) Name() string        { return "silent" }
func (p *silentProv) Configured() bool    { return true }
func (p *silentProv) SupportsTools() bool { return true }
func (p *silentProv) Stream(ctx context.Context, _ ai.Request) <-chan ai.Chunk {
	n := p.calls.Add(1)
	ch := make(chan ai.Chunk, 2)
	go func() {
		defer close(ch)
		if n <= p.silent {
			<-ctx.Done()
			ch <- ai.Chunk{Done: true, Err: ctx.Err()}
			return
		}
		ch <- ai.Chunk{Delta: "ok"}
		ch <- ai.Chunk{Done: true}
	}()
	return ch
}

func stallOptions(h *harness, prov ai.Provider) Options {
	o := h.options(1)
	build := o.Build
	o.Build = func(ctx context.Context, n agentdag.Node, input string) (*ai.Agent, []ai.Message, error) {
		a, msgs, err := build(ctx, n, input)
		if a != nil && n.ID == "a" {
			a.Provider = prov
		}
		return a, msgs, err
	}
	o.StallAfter = 30 * time.Millisecond
	return o
}

// OMX-23: a step with no activity for the stall limit is stopped and
// started once more; the second start answers.
func TestStalledNodeRunsOnceMore(t *testing.T) {
	h := newHarness(map[string]*fakeProv{"a": {}, "b": {}})
	p := &silentProv{silent: 1}
	r, err := Start(context.Background(), chain("a", "b"), stallOptions(h, p))
	if err != nil {
		t.Fatal(err)
	}
	if end := wait(t, r); end.Kind != RunDone {
		t.Fatalf("end %+v", end)
	}
	if p.calls.Load() != 2 {
		t.Fatalf("a was asked %d times, want 2", p.calls.Load())
	}
	ev, ok := h.find(NodeStalled, "a")
	if !ok || !strings.Contains(ev.Err, "no activity") {
		t.Fatalf("NodeStalled %+v %v", ev, ok)
	}
	if n := r.Snapshot().Nodes[0]; n.Attempt != 2 {
		t.Fatalf("attempt %d", n.Attempt)
	}
}

// OMX-23: a step that stalls twice fails.
func TestStalledTwiceFails(t *testing.T) {
	h := newHarness(map[string]*fakeProv{"a": {}, "b": {}})
	r, err := Start(context.Background(), chain("a", "b"), stallOptions(h, &silentProv{silent: 9}))
	if err != nil {
		t.Fatal(err)
	}
	end := wait(t, r)
	if end.Kind != RunFailed || !strings.Contains(end.Err, "no activity") {
		t.Fatalf("end %+v", end)
	}
	if got := states(r.Snapshot()); got != "a=failed b=blocked" {
		t.Fatalf("states %s", got)
	}
}

// OMX-23: a tool call that takes long is no stall.
func TestLongToolIsNoStall(t *testing.T) {
	h := newHarness(map[string]*fakeProv{
		"a": {replies: []ai.Chunk{{ToolCalls: []ai.ToolCall{{ID: "1", Name: "slow"}}}, {Delta: "done"}}},
	})
	h.tools = []ai.Tool{{Name: "slow", Run: func(ctx context.Context, _ map[string]any) (string, error) {
		select {
		case <-time.After(150 * time.Millisecond):
		case <-ctx.Done():
		}
		return "ok", ctx.Err()
	}}}
	o := h.options(1)
	o.StallAfter = 30 * time.Millisecond
	r, err := Start(context.Background(), chain("a"), o)
	if err != nil {
		t.Fatal(err)
	}
	if end := wait(t, r); end.Kind != RunDone {
		t.Fatalf("end %+v", end)
	}
	if _, ok := h.find(NodeStalled, "a"); ok {
		t.Fatal("a long tool call counted as a stall")
	}
}

