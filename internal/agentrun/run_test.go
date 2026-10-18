package agentrun

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// fakeProv is a scripted provider: each request answers with the next
// reply (the last one repeats). With gate set, a request waits until the
// gate closes or its context ends.
type fakeProv struct {
	replies []ai.Chunk
	gate    chan struct{}
	started chan struct{} // gets one value per request (when not nil)
	active  *atomic.Int32 // requests in flight, shared by the run's providers
	peak    *atomic.Int32 // the most requests in flight at once

	mu    sync.Mutex
	calls int
}

func (p *fakeProv) Name() string        { return "fake" }
func (p *fakeProv) Configured() bool    { return true }
func (p *fakeProv) SupportsTools() bool { return true }
func (p *fakeProv) Stream(ctx context.Context, _ ai.Request) <-chan ai.Chunk {
	p.mu.Lock()
	r := ai.Chunk{Delta: "ok"}
	if len(p.replies) > 0 {
		r = p.replies[min(p.calls, len(p.replies)-1)]
	}
	p.calls++
	p.mu.Unlock()
	ch := make(chan ai.Chunk, 2)
	go func() {
		defer close(ch)
		if p.active != nil {
			n := p.active.Add(1)
			defer p.active.Add(-1)
			for {
				old := p.peak.Load()
				if n <= old || p.peak.CompareAndSwap(old, n) {
					break
				}
			}
		}
		if p.started != nil {
			p.started <- struct{}{}
		}
		if p.gate != nil {
			select {
			case <-p.gate:
			case <-ctx.Done():
				ch <- ai.Chunk{Done: true, Err: ctx.Err()}
				return
			}
		}
		if r.Delta != "" {
			ch <- ai.Chunk{Delta: r.Delta}
		}
		ch <- ai.Chunk{Done: true, Err: r.Err, ToolCalls: r.ToolCalls, Usage: r.Usage}
	}()
	return ch
}

func (p *fakeProv) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// harness builds agents from per-node providers and records the inputs
// and events.
type harness struct {
	provs   map[string]*fakeProv
	tools   []ai.Tool
	confirm ai.ConfirmFunc

	mu     sync.Mutex
	inputs map[string]string
	events []Event
}

func newHarness(provs map[string]*fakeProv) *harness {
	return &harness{provs: provs, inputs: map[string]string{}}
}

func (h *harness) options(parallel int) Options {
	return Options{
		Parallel: parallel,
		User:     "fix the bug",
		Build: func(_ context.Context, n agentdag.Node, input string) (*ai.Agent, []ai.Message, error) {
			h.mu.Lock()
			h.inputs[n.ID] = input
			h.mu.Unlock()
			p, ok := h.provs[n.ID]
			if !ok {
				return nil, nil, errors.New("no provider for " + n.ID)
			}
			a := &ai.Agent{Provider: p, Native: true, Tools: h.tools, Confirm: h.confirm}
			return a, []ai.Message{{Role: ai.RoleSystem, Content: "sys"}, {Role: ai.RoleUser, Content: input}}, nil
		},
		Emit: func(e Event) {
			h.mu.Lock()
			h.events = append(h.events, e)
			h.mu.Unlock()
		},
	}
}

func (h *harness) input(id string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.inputs[id]
}

func (h *harness) kinds(node string) []EventKind {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []EventKind
	for _, e := range h.events {
		if e.NodeID == node {
			out = append(out, e.Kind)
		}
	}
	return out
}

func (h *harness) find(kind EventKind, node string) (Event, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range h.events {
		if e.Kind == kind && e.NodeID == node {
			return e, true
		}
	}
	return Event{}, false
}

func (h *harness) index(kind EventKind, node string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, e := range h.events {
		if e.Kind == kind && e.NodeID == node {
			return i
		}
	}
	return -1
}

func chain(ids ...string) agentdag.Run {
	r := agentdag.Run{ID: "run1", Edges: agentdag.Chain(ids...)}
	for _, id := range ids {
		r.Nodes = append(r.Nodes, agentdag.Node{ID: id, Title: strings.ToUpper(id)})
	}
	return r
}

func diamond() agentdag.Run {
	r := agentdag.Run{ID: "run2"}
	for _, id := range []string{"a", "b", "c", "d"} {
		r.Nodes = append(r.Nodes, agentdag.Node{ID: id, Title: strings.ToUpper(id)})
	}
	r.Edges = []agentdag.Edge{{From: "a", To: "b"}, {From: "a", To: "c"}, {From: "b", To: "d"}, {From: "c", To: "d"}}
	return r
}

func wait(t *testing.T, h *Handle) Event {
	t.Helper()
	select {
	case <-h.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("run did not end")
	}
	return h.Wait()
}

func states(r agentdag.Run) string {
	var b []string
	for _, n := range r.Nodes {
		b = append(b, n.ID+"="+string(n.State))
	}
	return strings.Join(b, " ")
}

func usage(in, out int) *ai.Usage {
	return &ai.Usage{Model: "m", Input: in, Output: out, Requests: 1, PeakContext: in}
}

func TestChainPassesOutputAndSumsUsage(t *testing.T) {
	h := newHarness(map[string]*fakeProv{
		"a": {replies: []ai.Chunk{{Delta: "alpha found a bug in quit.go", Usage: usage(100, 20)}}},
		"b": {replies: []ai.Chunk{{Delta: "fixed it", Usage: usage(300, 40)}}},
	})
	run, err := Start(context.Background(), chain("a", "b"), h.options(1))
	if err != nil {
		t.Fatal(err)
	}
	end := wait(t, run)
	if end.Kind != RunDone || end.RunID != "run1" || end.Output != "fixed it" {
		t.Fatalf("end = %+v", end)
	}
	if h.input("a") != "fix the bug" {
		t.Errorf("first step gets the user message as is: %q", h.input("a"))
	}
	want := "fix the bug\n\n### Output of step \"A\"\nalpha found a bug in quit.go"
	if got := h.input("b"); got != want {
		t.Errorf("b input =\n%q\nwant\n%q", got, want)
	}
	if h.index(NodeStart, "b") < h.index(NodeDone, "a") {
		t.Error("b started before a was done")
	}
	da, _ := h.find(NodeDone, "a")
	if da.Output != "alpha found a bug in quit.go" || da.Usage == nil || da.Usage.Input != 100 || da.RunID != "run1" {
		t.Errorf("a done = %+v", da)
	}
	if end.Usage == nil || end.Usage.Input != 400 || end.Usage.Output != 60 || end.Usage.Requests != 2 || end.Usage.PeakContext != 300 {
		t.Errorf("run usage = %+v", end.Usage)
	}
	if got := states(run.Snapshot()); got != "a=completed b=completed" {
		t.Errorf("states %s", got)
	}
	if got := run.Output("a"); got != "alpha found a bug in quit.go" {
		t.Errorf("output a %q", got)
	}
}

func TestNodeEventsToolAndTextTail(t *testing.T) {
	call := ai.ToolCall{ID: "c1", Name: "echo", Args: map[string]any{"x": "1"}}
	long := strings.Repeat("y", 300) + "\x1b[31mEND"
	h := newHarness(map[string]*fakeProv{
		"a": {replies: []ai.Chunk{{ToolCalls: []ai.ToolCall{call}}, {Delta: long}}},
	})
	h.tools = []ai.Tool{{Name: "echo", Run: func(context.Context, map[string]any) (string, error) { return "echoed", nil }}}
	run, err := Start(context.Background(), chain("a"), h.options(1))
	if err != nil {
		t.Fatal(err)
	}
	wait(t, run)
	var toolKinds []ai.EventKind
	var tail string
	h.mu.Lock()
	for _, e := range h.events {
		switch e.Kind {
		case NodeTool:
			toolKinds = append(toolKinds, e.AI.Kind)
			if e.NodeID != "a" || e.AI.Tool != "echo" {
				t.Errorf("tool event %+v", e)
			}
		case NodeText:
			tail = e.Tail
		}
	}
	h.mu.Unlock()
	if len(toolKinds) != 2 || toolKinds[0] != ai.EventToolStart || toolKinds[1] != ai.EventToolResult {
		t.Errorf("tool events %v", toolKinds)
	}
	if n := len([]rune(tail)); n != TailLen || !strings.HasSuffix(tail, "yEND") || strings.Contains(tail, "\x1b") {
		t.Errorf("tail (%d) %q", n, tail)
	}
	k := h.kinds("a")
	if k[0] != NodeStart || k[len(k)-1] != NodeDone {
		t.Errorf("a kinds %v", k)
	}
}

func TestDiamondRunsInParallel(t *testing.T) {
	var active, peak atomic.Int32
	gate := make(chan struct{})
	started := make(chan struct{}, 4)
	mk := func(text string, gated bool) *fakeProv {
		p := &fakeProv{replies: []ai.Chunk{{Delta: text, Usage: usage(10, 1)}}, active: &active, peak: &peak}
		if gated {
			p.gate, p.started = gate, started
		}
		return p
	}
	h := newHarness(map[string]*fakeProv{"a": mk("from a", false), "b": mk("from b", true), "c": mk("from c", true), "d": mk("from d", false)})
	run, err := Start(context.Background(), diamond(), h.options(2))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // b and c both reach the model before either ends
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of b, c started", i)
		}
	}
	if s := states(run.Snapshot()); s != "a=completed b=running c=running d=pending" {
		t.Errorf("mid-run states %s", s)
	}
	close(gate)
	end := wait(t, run)
	if end.Kind != RunDone || end.Output != "from d" {
		t.Fatalf("end %+v", end)
	}
	if peak.Load() != 2 {
		t.Errorf("peak %d, want 2", peak.Load())
	}
	want := "fix the bug\n\n### Output of step \"B\"\nfrom b\n\n### Output of step \"C\"\nfrom c"
	if got := h.input("d"); got != want {
		t.Errorf("d input %q", got)
	}
	if end.Usage.Requests != 4 {
		t.Errorf("usage %+v", end.Usage)
	}
}

func TestDiamondParallelOneIsSerial(t *testing.T) {
	var active, peak atomic.Int32
	mk := func() *fakeProv { return &fakeProv{active: &active, peak: &peak} }
	h := newHarness(map[string]*fakeProv{"a": mk(), "b": mk(), "c": mk(), "d": mk()})
	o := h.options(0) // 0 means 1
	run, err := Start(context.Background(), diamond(), o)
	if err != nil {
		t.Fatal(err)
	}
	if end := wait(t, run); end.Kind != RunDone {
		t.Fatalf("end %+v", end)
	}
	if peak.Load() != 1 {
		t.Errorf("peak %d, want 1", peak.Load())
	}
}

func TestFailureBlocksDownstream(t *testing.T) {
	h := newHarness(map[string]*fakeProv{
		"a": {replies: []ai.Chunk{{Err: errors.New("bad request"), Usage: usage(5, 0)}}},
		"b": {},
	})
	run, err := Start(context.Background(), chain("a", "b"), h.options(1))
	if err != nil {
		t.Fatal(err)
	}
	end := wait(t, run)
	if end.Kind != RunFailed || !strings.Contains(end.Err, "bad request") {
		t.Fatalf("end %+v", end)
	}
	f, ok := h.find(NodeFailed, "a")
	if !ok || !strings.Contains(f.Err, "bad request") || f.Usage == nil || f.Usage.Input != 5 {
		t.Errorf("a failed event %+v", f)
	}
	if _, ok := h.find(NodeBlocked, "b"); !ok {
		t.Error("b must get a blocked event")
	}
	if h.provs["b"].callCount() != 0 {
		t.Error("b must not run")
	}
	snap := run.Snapshot()
	if got := states(snap); got != "a=failed b=blocked" {
		t.Errorf("states %s", got)
	}
	if !strings.Contains(snap.Nodes[0].Error, "bad request") || snap.Nodes[0].Attempt != 1 {
		t.Errorf("a node %+v", snap.Nodes[0])
	}
	if end.Usage == nil || end.Usage.Input != 5 {
		t.Errorf("failed run still sums usage: %+v", end.Usage)
	}
}

func TestFailureStopsSiblings(t *testing.T) {
	gate := make(chan struct{}) // never closed: c runs until cancelled
	started := make(chan struct{}, 1)
	h := newHarness(map[string]*fakeProv{
		"a": {},
		"b": {replies: []ai.Chunk{{Err: errors.New("boom")}}},
		"c": {gate: gate, started: started},
		"d": {},
	})
	o := h.options(2)
	build := o.Build
	o.Build = func(ctx context.Context, n agentdag.Node, input string) (*ai.Agent, []ai.Message, error) {
		if n.ID == "b" { // fail only once c is running
			<-started
		}
		return build(ctx, n, input)
	}
	run, err := Start(context.Background(), diamond(), o)
	if err != nil {
		t.Fatal(err)
	}
	end := wait(t, run)
	if end.Kind != RunFailed {
		t.Fatalf("end %+v", end)
	}
	if got := states(run.Snapshot()); got != "a=completed b=failed c=cancelled d=blocked" {
		t.Errorf("states %s", got)
	}
}

func TestCancelStopsAll(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan struct{}, 1)
	h := newHarness(map[string]*fakeProv{"a": {gate: gate, started: started}, "b": {}})
	run, err := Start(context.Background(), chain("a", "b"), h.options(1))
	if err != nil {
		t.Fatal(err)
	}
	<-started
	run.Cancel()
	end := wait(t, run)
	if end.Kind != RunCancelled {
		t.Fatalf("end %+v", end)
	}
	if got := states(run.Snapshot()); got != "a=cancelled b=cancelled" {
		t.Errorf("states %s", got)
	}
	if _, ok := h.find(NodeCancelled, "a"); !ok {
		t.Error("a must get a cancelled event")
	}
	if h.provs["b"].callCount() != 0 {
		t.Error("b must not run")
	}
	run.Cancel() // twice is fine
}

func TestParentContextCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	gate := make(chan struct{})
	started := make(chan struct{}, 1)
	h := newHarness(map[string]*fakeProv{"a": {gate: gate, started: started}})
	run, err := Start(ctx, chain("a"), h.options(1))
	if err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()
	if end := wait(t, run); end.Kind != RunCancelled {
		t.Fatalf("end %+v", end)
	}
}

func TestInputRules(t *testing.T) {
	r := agentdag.Run{ID: "r", Nodes: []agentdag.Node{
		{ID: "a", Title: "A"}, {ID: "b", Title: "B"},
		{ID: "c", Title: "C", Input: agentdag.InputRule{NoUser: true, Deps: []string{"b"}}},
	}, Edges: []agentdag.Edge{{From: "a", To: "c"}, {From: "b", To: "c"}}}
	h := newHarness(map[string]*fakeProv{
		"a": {replies: []ai.Chunk{{Delta: "aaa"}}},
		"b": {replies: []ai.Chunk{{Delta: "  "}}}, // empty output
		"c": {},
	})
	run, err := Start(context.Background(), r, h.options(1))
	if err != nil {
		t.Fatal(err)
	}
	wait(t, run)
	if got, want := h.input("c"), "### Output of step \"B\"\n(no output)"; got != want {
		t.Errorf("c input %q, want %q", got, want)
	}
}

func TestInputCap(t *testing.T) {
	h := newHarness(map[string]*fakeProv{"a": {replies: []ai.Chunk{{Delta: strings.Repeat("é", 100)}}}, "b": {}})
	o := h.options(1)
	o.InputMaxBytes = 64
	run, err := Start(context.Background(), chain("a", "b"), o)
	if err != nil {
		t.Fatal(err)
	}
	wait(t, run)
	got := h.input("b")
	if !strings.HasSuffix(got, CutNote) || len(got)-len(CutNote) > 64 || !strings.HasPrefix(got, "fix the bug") {
		t.Errorf("capped input (%d) %q", len(got), got)
	}
	if strings.ContainsRune(got, '�') {
		t.Error("cut inside a rune")
	}
}

func TestBuildErrorFailsNode(t *testing.T) {
	h := newHarness(map[string]*fakeProv{"a": {}}) // no provider for b
	run, err := Start(context.Background(), chain("a", "b", "c"), h.options(1))
	if err != nil {
		t.Fatal(err)
	}
	end := wait(t, run)
	if end.Kind != RunFailed || !strings.Contains(end.Err, "no provider for b") {
		t.Fatalf("end %+v", end)
	}
	if got := states(run.Snapshot()); got != "a=completed b=failed c=blocked" {
		t.Errorf("states %s", got)
	}
}

func TestMaxStepsFromNode(t *testing.T) {
	loop := ai.Chunk{ToolCalls: []ai.ToolCall{{ID: "x", Name: "echo"}}}
	h := newHarness(map[string]*fakeProv{"a": {replies: []ai.Chunk{loop}}})
	h.tools = []ai.Tool{{Name: "echo", Run: func(context.Context, map[string]any) (string, error) { return "", nil }}}
	r := chain("a")
	r.Nodes[0].Input.MaxSteps = 2
	run, err := Start(context.Background(), r, h.options(1))
	if err != nil {
		t.Fatal(err)
	}
	wait(t, run)
	if n := h.provs["a"].callCount(); n != 2 {
		t.Errorf("calls %d, want 2 (max_steps)", n)
	}
}

func TestStartRejectsBadRun(t *testing.T) {
	h := newHarness(nil)
	if _, err := Start(context.Background(), agentdag.Run{ID: "x"}, h.options(1)); err == nil {
		t.Error("empty run")
	}
	bad := chain("a", "b")
	bad.Edges = append(bad.Edges, agentdag.Edge{From: "b", To: "a"})
	if _, err := Start(context.Background(), bad, h.options(1)); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Errorf("cycle: %v", err)
	}
	if _, err := Start(context.Background(), chain("a"), Options{}); err == nil {
		t.Error("no Build")
	}
}

// Spec (AP-4 retry from step): a completed node with a saved output does
// not run again; its dependents get the saved output as input.
func TestRetryFromStepUsesSavedOutputs(t *testing.T) {
	h := newHarness(map[string]*fakeProv{
		"a": {replies: []ai.Chunk{{Delta: "new a"}}},
		"b": {replies: []ai.Chunk{{Delta: "fixed"}}},
	})
	r := chain("a", "b")
	r.Nodes[0].State = agentdag.StateCompleted
	o := h.options(1)
	o.Outputs = map[string]string{"a": "saved review"}
	run, err := Start(context.Background(), r, o)
	if err != nil {
		t.Fatal(err)
	}
	end := wait(t, run)
	if end.Kind != RunDone || end.Output != "fixed" {
		t.Fatalf("end %+v", end)
	}
	if n := h.provs["a"].callCount(); n != 0 {
		t.Errorf("a ran again (%d calls)", n)
	}
	if got := h.input("b"); !strings.Contains(got, "### Output of step \"A\"\nsaved review") {
		t.Errorf("b input %q", got)
	}
	if k := h.kinds("a"); len(k) != 0 {
		t.Errorf("a events %v", k)
	}
}

// Spec (AP-4 review gate): every node's tools see the run's review
// gate in their context, so file reviews of parallel nodes take turns.
func TestReviewGateInToolContext(t *testing.T) {
	call := ai.ToolCall{ID: "c1", Name: "edit"}
	h := newHarness(map[string]*fakeProv{
		"a": {replies: []ai.Chunk{{ToolCalls: []ai.ToolCall{call}}, {Delta: "done"}}},
	})
	lk := &Gate{}
	var got *Gate
	h.tools = []ai.Tool{{Name: "edit", Run: func(ctx context.Context, _ map[string]any) (string, error) {
		got = ReviewGate(ctx)
		return "ok", nil
	}}}
	o := h.options(1)
	o.Review = lk
	run, err := Start(context.Background(), chain("a"), o)
	if err != nil {
		t.Fatal(err)
	}
	wait(t, run)
	if got != lk {
		t.Errorf("tool saw gate %v", got)
	}
	if ReviewGate(context.Background()) != nil {
		t.Error("a plain context has no gate")
	}
}
