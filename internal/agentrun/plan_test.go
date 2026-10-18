package agentrun

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// U20 AP-8: planned runs (a planner node grows the run), waves, the
// confirm queue, the review gate and the budget pause.

const diamondPlan = "Here is the plan.\n```json\n" + `{"nodes": [
  {"id": "map", "title": "Map", "prompt": "List the files.", "category": "quick"},
  {"id": "left", "title": "Left", "prompt": "Fix the left part.", "deps": ["map"]},
  {"id": "right", "title": "Right", "prompt": "Fix the right part.", "category": "quick", "deps": ["map"]},
  {"id": "verify", "title": "Verify", "prompt": "Check both fixes.", "category": "deep", "deps": ["left", "right"]}
]}` + "\n```\n"

func planRun() agentdag.Run {
	return agentdag.Run{ID: "plan1", Nodes: []agentdag.Node{{ID: "planner", Title: "Planner", Category: agentdag.CategoryDeep, Plan: true}}}
}

func planOptions(h *harness, parallel int) Options {
	o := h.options(parallel)
	o.Categories = map[string]string{agentdag.CategoryQuick: "cheap", agentdag.CategoryDeep: "strong"}
	return o
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// Spec (F5): a planner node answers a diamond plan; the run grows by its
// nodes (slots from the categories), wave 2 runs both middle nodes at
// once, the last node's text is the answer, each node gets its task and
// its own deps' outputs (not the plan JSON).
func TestPlannerDiamond(t *testing.T) {
	gate := make(chan struct{})
	var active, peak atomic.Int32
	h := newHarness(map[string]*fakeProv{
		"planner": {replies: []ai.Chunk{{Delta: diamondPlan, Usage: usage(10, 5)}}},
		"map":     {replies: []ai.Chunk{{Delta: "files: a.go b.go"}}},
		"left":    {replies: []ai.Chunk{{Delta: "left fixed"}}, gate: gate, active: &active, peak: &peak},
		"right":   {replies: []ai.Chunk{{Delta: "right fixed"}}, gate: gate, active: &active, peak: &peak},
		"verify":  {replies: []ai.Chunk{{Delta: "all good"}}},
	})
	run, err := Start(context.Background(), planRun(), planOptions(h, 3))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "both middle nodes", func() bool { return peak.Load() == 2 })
	close(gate)
	end := wait(t, run)
	if end.Kind != RunDone || end.Output != "all good" {
		t.Fatalf("end %+v", end)
	}
	ev, ok := h.find(RunPlanned, "planner")
	if !ok || len(ev.Nodes) != 4 {
		t.Fatalf("planned event %+v", ev)
	}
	slots := map[string]string{}
	for _, n := range ev.Nodes {
		slots[n.ID] = n.Slot + "/" + n.Category
	}
	if slots["map"] != "cheap/quick" || slots["left"] != "cheap/quick" || slots["verify"] != "strong/deep" {
		t.Errorf("slots %v", slots)
	}
	snap := run.Snapshot()
	if got := states(snap); got != "planner=completed map=completed left=completed right=completed verify=completed" {
		t.Errorf("states %s", got)
	}
	if deps := agentdag.Deps(snap, "map"); len(deps) != 1 || deps[0] != "planner" {
		t.Errorf("map deps %v", deps)
	}
	if deps := agentdag.Deps(snap, "verify"); strings.Join(deps, ",") != "left,right" {
		t.Errorf("verify deps %v", deps)
	}
	in := h.input("verify")
	for _, want := range []string{"fix the bug", "### Task\nCheck both fixes.", "### Output of step \"Left\"\nleft fixed", "### Output of step \"Right\"\nright fixed"} {
		if !strings.Contains(in, want) {
			t.Errorf("verify input lacks %q:\n%s", want, in)
		}
	}
	if strings.Contains(h.input("map"), "\"nodes\"") || strings.Contains(in, "files: a.go") {
		t.Errorf("a node got outputs it does not depend on:\nmap: %s\nverify: %s", h.input("map"), in)
	}
	if i, j := h.index(NodeDone, "planner"), h.index(RunPlanned, "planner"); i < 0 || j < i {
		t.Errorf("RunPlanned (%d) must follow the planner's NodeDone (%d)", j, i)
	}
}

// Spec: the planner's dependents run after the plan's last node.
func TestPlannerDependentsWaitForThePlan(t *testing.T) {
	h := newHarness(map[string]*fakeProv{
		"planner": {replies: []ai.Chunk{{Delta: diamondPlan}}},
		"map":     {replies: []ai.Chunk{{Delta: "m"}}},
		"left":    {replies: []ai.Chunk{{Delta: "l"}}},
		"right":   {replies: []ai.Chunk{{Delta: "r"}}},
		"verify":  {replies: []ai.Chunk{{Delta: "v"}}},
		"summary": {replies: []ai.Chunk{{Delta: "the summary"}}},
	})
	r := planRun()
	r.Nodes = append(r.Nodes, agentdag.Node{ID: "summary", Title: "Summary"})
	r.Edges = agentdag.Chain("planner", "summary")
	run, err := Start(context.Background(), r, planOptions(h, 3))
	if err != nil {
		t.Fatal(err)
	}
	end := wait(t, run)
	if end.Kind != RunDone || end.Output != "the summary" {
		t.Fatalf("end %+v", end)
	}
	if deps := agentdag.Deps(run.Snapshot(), "summary"); strings.Join(deps, ",") != "verify" {
		t.Errorf("summary deps %v", deps)
	}
	if h.index(NodeStart, "summary") < h.index(NodeDone, "verify") {
		t.Error("summary started before the plan finished")
	}
}

// Spec: a bad plan gets one repair attempt with the validation error.
func TestPlannerRepairsAnInvalidPlan(t *testing.T) {
	bad := "```json\n" + `{"nodes": [{"id": "a", "prompt": "x", "deps": ["b"]}, {"id": "b", "prompt": "y", "deps": ["a"]}]}` + "\n```"
	good := "```json\n" + `{"nodes": [{"id": "a", "title": "A", "prompt": "do it"}]}` + "\n```"
	h := newHarness(map[string]*fakeProv{
		"planner": {replies: []ai.Chunk{{Delta: bad, Usage: usage(10, 1)}, {Delta: good, Usage: usage(20, 2)}}},
		"a":       {replies: []ai.Chunk{{Delta: "done a"}}},
	})
	var reqs []ai.Request
	var mu sync.Mutex
	o := planOptions(h, 3)
	build := o.Build
	o.Build = func(ctx context.Context, n agentdag.Node, input string) (*ai.Agent, []ai.Message, error) {
		a, hist, err := build(ctx, n, input)
		if a != nil && n.ID == "planner" {
			a.Provider = recProv{inner: a.Provider, mu: &mu, reqs: &reqs}
		}
		return a, hist, err
	}
	run, err := Start(context.Background(), planRun(), o)
	if err != nil {
		t.Fatal(err)
	}
	end := wait(t, run)
	if end.Kind != RunDone || end.Output != "done a" {
		t.Fatalf("end %+v", end)
	}
	if n := h.provs["planner"].callCount(); n != 2 {
		t.Fatalf("planner requests %d", n)
	}
	rep, ok := h.find(NodeRepair, "planner")
	if !ok || !strings.Contains(rep.Err, "cycle") {
		t.Errorf("repair event %+v", rep)
	}
	mu.Lock()
	last := reqs[len(reqs)-1].Messages
	mu.Unlock()
	if got := last[len(last)-1].Content; last[len(last)-1].Role != ai.RoleUser || !strings.Contains(got, "cycle") || !strings.Contains(got, "not valid") {
		t.Errorf("repair request %q", got)
	}
	if done, _ := h.find(NodeDone, "planner"); done.Usage == nil || done.Usage.Input != 30 {
		t.Errorf("planner usage must sum both requests: %+v", done.Usage)
	}
}

// Spec: a plan that is still bad after the repair fails the planner with
// a clear error; nothing else runs.
func TestPlannerFailsAfterOneRepair(t *testing.T) {
	h := newHarness(map[string]*fakeProv{
		"planner": {replies: []ai.Chunk{{Delta: "I think we should just do it."}}},
	})
	run, err := Start(context.Background(), planRun(), planOptions(h, 3))
	if err != nil {
		t.Fatal(err)
	}
	end := wait(t, run)
	if end.Kind != RunFailed || !strings.Contains(end.Err, "plan is not valid") {
		t.Fatalf("end %+v", end)
	}
	if n := h.provs["planner"].callCount(); n != 2 {
		t.Errorf("planner requests %d (want the answer and one repair)", n)
	}
	if f, ok := h.find(NodeFailed, "planner"); !ok || !strings.Contains(f.Err, "after one repair") {
		t.Errorf("failed event %+v", f)
	}
}

// Spec: a plan over the limits (12 nodes) is refused like any bad plan.
func TestPlannerRefusesATooBigPlan(t *testing.T) {
	var nodes []string
	for i := 1; i <= 13; i++ {
		nodes = append(nodes, fmt.Sprintf(`{"id": "n%d", "prompt": "part %d"}`, i, i))
	}
	nodes = append(nodes[:12], `{"id": "n13", "prompt": "join", "deps": ["n1","n2","n3","n4","n5","n6","n7","n8","n9","n10","n11","n12"]}`)
	big := "```json\n{\"nodes\": [" + strings.Join(nodes, ",") + "]}\n```"
	h := newHarness(map[string]*fakeProv{"planner": {replies: []ai.Chunk{{Delta: big}}}})
	run, err := Start(context.Background(), planRun(), planOptions(h, 3))
	if err != nil {
		t.Fatal(err)
	}
	end := wait(t, run)
	if end.Kind != RunFailed || !strings.Contains(end.Err, "too many nodes") {
		t.Fatalf("end %+v", end)
	}
	if got := len(run.Snapshot().Nodes); got != 1 {
		t.Errorf("the run grew to %d nodes", got)
	}
}

func TestParsePlan(t *testing.T) {
	taken := map[string]bool{"planner": true}
	layers := `{"nodes": [{"id":"a","prompt":"p"},{"id":"b","prompt":"p","deps":["a"]},{"id":"c","prompt":"p","deps":["b"]},` +
		`{"id":"d","prompt":"p","deps":["c"]},{"id":"e","prompt":"p","deps":["d"]},{"id":"f","prompt":"p","deps":["e"]},{"id":"g","prompt":"p","deps":["f"]}]}`
	for _, tc := range []struct{ name, text, want string }{
		{"no json", "just words", "no plan JSON"},
		{"unknown field", `{"nodes": [{"id": "a", "prompt": "p", "model": "x"}]}`, "unknown field"},
		{"no nodes", `{"nodes": []}`, "no nodes"},
		{"bad id", `{"nodes": [{"id": "a b", "prompt": "p"}]}`, "id"},
		{"taken id", `{"nodes": [{"id": "planner", "prompt": "p"}]}`, "already used"},
		{"duplicate", `{"nodes": [{"id": "a", "prompt": "p"}, {"id": "a", "prompt": "q"}]}`, "duplicate"},
		{"empty prompt", `{"nodes": [{"id": "a"}]}`, "prompt"},
		{"bad category", `{"nodes": [{"id": "a", "prompt": "p", "category": "huge"}]}`, "category"},
		{"unknown dep", `{"nodes": [{"id": "a", "prompt": "p", "deps": ["zz"]}]}`, "unknown dep"},
		{"two ends", `{"nodes": [{"id": "a", "prompt": "p"}, {"id": "b", "prompt": "q"}]}`, "one final node"},
		{"too deep", layers, "too many layers"},
	} {
		_, _, err := ParsePlan(tc.text, taken)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err %v, want %q", tc.name, err, tc.want)
		}
	}
	nodes, edges, err := ParsePlan(diamondPlan, taken)
	if err != nil || len(nodes) != 4 || len(edges) != 4 {
		t.Fatalf("diamond: %v %v %v", nodes, edges, err)
	}
	if n := nodes[1]; n.Category != agentdag.CategoryQuick || n.Prompt != "Fix the left part." || n.Title != "Left" || n.Input.Deps == nil {
		t.Errorf("left %+v", n)
	}
}

// Spec: confirm cards of parallel nodes come one at a time (the gate);
// both nodes still get their answer.
func TestParallelConfirmQueue(t *testing.T) {
	call := func(id string) ai.Chunk {
		return ai.Chunk{ToolCalls: []ai.ToolCall{{ID: id, Name: "edit", Args: map[string]any{"path": id}}}}
	}
	h := newHarness(map[string]*fakeProv{
		"x": {replies: []ai.Chunk{call("cx"), {Delta: "x done"}}},
		"y": {replies: []ai.Chunk{call("cy"), {Delta: "y done"}}},
	})
	h.tools = []ai.Tool{{Name: "edit", Mutating: true, Run: func(context.Context, map[string]any) (string, error) { return "ok", nil }}}
	var open, peak, asked atomic.Int32
	h.confirm = func(context.Context, ai.Tool, string) bool {
		n := open.Add(1)
		defer open.Add(-1)
		if n > peak.Load() {
			peak.Store(n)
		}
		asked.Add(1)
		time.Sleep(60 * time.Millisecond) // the user reads the card
		return true
	}
	o := h.options(2)
	o.Review = &Gate{}
	r := agentdag.Run{ID: "q", Nodes: []agentdag.Node{{ID: "x"}, {ID: "y"}}}
	run, err := Start(context.Background(), r, o)
	if err != nil {
		t.Fatal(err)
	}
	end := wait(t, run)
	if end.Kind != RunDone {
		t.Fatalf("end %+v", end)
	}
	if asked.Load() != 2 || peak.Load() != 1 {
		t.Errorf("confirms asked %d, at once %d (want 2 and 1)", asked.Load(), peak.Load())
	}
}

// Spec: a node that waits for the review gate (another node holds it)
// stops at once when the run is cancelled.
func TestCancelWhileWaitingForReviewGate(t *testing.T) {
	h := newHarness(map[string]*fakeProv{
		"x": {replies: []ai.Chunk{{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "edit"}}}, {Delta: "never"}}},
	})
	waiting := make(chan struct{})
	h.tools = []ai.Tool{{Name: "edit", Run: func(ctx context.Context, _ map[string]any) (string, error) {
		g := ReviewGate(ctx)
		close(waiting)
		if err := g.Acquire(ctx); err != nil {
			return "", err
		}
		defer g.Release()
		return "reviewed", nil
	}}}
	gate := &Gate{}
	if err := gate.Acquire(context.Background()); err != nil { // another node's review is open
		t.Fatal(err)
	}
	o := h.options(2)
	o.Review = gate
	run, err := Start(context.Background(), chain("x"), o)
	if err != nil {
		t.Fatal(err)
	}
	<-waiting
	run.Cancel()
	end := wait(t, run)
	if end.Kind != RunCancelled {
		t.Fatalf("end %+v", end)
	}
	if got := states(run.Snapshot()); got != "x=cancelled" {
		t.Errorf("states %s", got)
	}
	if gate.TryAcquire() {
		t.Error("the waiting node took the gate that another node holds")
	}
	gate.Release()
	if !gate.TryAcquire() {
		t.Error("the gate is not free after the holder released it")
	}
}

func budgetOptions(h *harness, budget float64) Options {
	o := h.options(1)
	o.Budget = budget
	o.Cost = func(_ agentdag.Node, u ai.Usage) (float64, bool) { return float64(u.Input) / 100, true }
	return o
}

// Spec: when the run's cost reaches budget_usd it pauses before the next
// node and asks; Continue runs the rest without another stop.
func TestBudgetPauseThenContinue(t *testing.T) {
	h := newHarness(map[string]*fakeProv{
		"a": {replies: []ai.Chunk{{Delta: "a", Usage: usage(60, 1)}}},
		"b": {replies: []ai.Chunk{{Delta: "b", Usage: usage(60, 1)}}},
		"c": {replies: []ai.Chunk{{Delta: "c", Usage: usage(60, 1)}}},
		"d": {replies: []ai.Chunk{{Delta: "d", Usage: usage(60, 1)}}},
	})
	run, err := Start(context.Background(), chain("a", "b", "c", "d"), budgetOptions(h, 1.0))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pause", func() bool { _, ok := h.find(RunPaused, ""); return ok })
	p, _ := h.find(RunPaused, "")
	if p.Budget != 1.0 || p.Cost < 1.19 || p.Cost > 1.21 {
		t.Errorf("pause %+v", p)
	}
	time.Sleep(30 * time.Millisecond)
	if n := h.provs["c"].callCount(); n != 0 {
		t.Fatalf("c ran while paused (%d)", n)
	}
	if got := states(run.Snapshot()); got != "a=completed b=completed c=pending d=pending" {
		t.Errorf("states while paused %s", got)
	}
	run.Continue()
	end := wait(t, run)
	if end.Kind != RunDone || end.Output != "d" {
		t.Fatalf("end %+v", end)
	}
	if _, ok := h.find(RunResumed, ""); !ok {
		t.Error("no resumed event")
	}
	n := 0
	for _, k := range h.kinds("") {
		if k == RunPaused {
			n++
		}
	}
	if n != 1 {
		t.Errorf("paused %d times (Continue lifts the budget for the run)", n)
	}
}

// Spec: Stop on a paused run cancels what did not run.
func TestBudgetPauseThenStop(t *testing.T) {
	h := newHarness(map[string]*fakeProv{
		"a": {replies: []ai.Chunk{{Delta: "a", Usage: usage(200, 1)}}},
		"b": {replies: []ai.Chunk{{Delta: "b"}}},
	})
	run, err := Start(context.Background(), chain("a", "b"), budgetOptions(h, 0.5))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pause", func() bool { _, ok := h.find(RunPaused, ""); return ok })
	run.Cancel()
	end := wait(t, run)
	if end.Kind != RunCancelled {
		t.Fatalf("end %+v", end)
	}
	if got := states(run.Snapshot()); got != "a=completed b=cancelled" {
		t.Errorf("states %s", got)
	}
}

// Spec: no budget, or a run under it, never pauses.
func TestNoBudgetNoPause(t *testing.T) {
	h := newHarness(map[string]*fakeProv{
		"a": {replies: []ai.Chunk{{Delta: "a", Usage: usage(60, 1)}}},
		"b": {replies: []ai.Chunk{{Delta: "b", Usage: usage(60, 1)}}},
	})
	run, err := Start(context.Background(), chain("a", "b"), budgetOptions(h, 0))
	if err != nil {
		t.Fatal(err)
	}
	if end := wait(t, run); end.Kind != RunDone {
		t.Fatalf("end %+v", end)
	}
	if _, ok := h.find(RunPaused, ""); ok {
		t.Error("paused without a budget")
	}
}

// recProv records the requests of a provider.
type recProv struct {
	inner ai.Provider
	mu    *sync.Mutex
	reqs  *[]ai.Request
}

func (p recProv) Name() string        { return p.inner.Name() }
func (p recProv) Configured() bool    { return true }
func (p recProv) SupportsTools() bool { return true }
func (p recProv) Stream(ctx context.Context, r ai.Request) <-chan ai.Chunk {
	p.mu.Lock()
	r.Messages = append([]ai.Message(nil), r.Messages...)
	*p.reqs = append(*p.reqs, r)
	p.mu.Unlock()
	return p.inner.Stream(ctx, r)
}
