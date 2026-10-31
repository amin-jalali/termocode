package agentrun

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

const onePlan = "```json\n" + `{"nodes": [{"id": "fix", "title": "Fix", "prompt": "fix it"}]}` + "\n```"
const twoPlan = "```json\n" + `{"nodes": [{"id": "map", "title": "Map", "prompt": "map it"}, {"id": "fix", "title": "Fix", "prompt": "fix it", "deps": ["map"]}]}` + "\n```"

// OMX-14: with plan review on, a reviewer reads the plan; a reject sends
// its reasons back to the planner, whose new plan is read again, at
// most PlanReviewRounds times.
func TestPlanReviewRounds(t *testing.T) {
	h := newHarness(map[string]*fakeProv{"planner": {replies: []ai.Chunk{{Delta: onePlan}, {Delta: twoPlan}}}, "map": {}, "fix": {}})
	o := planOptions(h, 1)
	var mu sync.Mutex
	var seen []string
	o.PlanReviewRounds = 2
	o.PlanReview = func(_ context.Context, plan string) (bool, string, *ai.Usage, error) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, plan)
		if len(seen) == 1 {
			return false, "map the code first", usage(5, 1), nil
		}
		return true, "", usage(5, 1), nil
	}
	run, err := Start(context.Background(), planRun(), o)
	if err != nil {
		t.Fatal(err)
	}
	if end := wait(t, run); end.Kind != RunDone {
		t.Fatalf("end %+v", end)
	}
	if len(seen) != 2 || !strings.Contains(seen[1], `"map"`) {
		t.Fatalf("reviews %q", seen)
	}
	p := h.provs["planner"]
	p.mu.Lock()
	last := p.reqs[1][len(p.reqs[1])-1].Content
	p.mu.Unlock()
	if !strings.Contains(last, "map the code first") {
		t.Fatalf("the planner did not get the reasons: %q", last)
	}
	var verdicts []string
	h.mu.Lock()
	for _, e := range h.events {
		if e.Kind == NodePlanReview {
			verdicts = append(verdicts, e.Verdict+":"+e.Err)
		}
	}
	h.mu.Unlock()
	if strings.Join(verdicts, "|") != "reject:map the code first|approve:" {
		t.Fatalf("review events %q", verdicts)
	}
	if got := states(run.Snapshot()); !strings.Contains(got, "map=completed") {
		t.Fatalf("states %s", got)
	}
}

// OMX-14: when the rounds run out the last plan goes on (the user's
// approval still applies), and the event says so.
func TestPlanReviewRoundsRunOut(t *testing.T) {
	h := newHarness(map[string]*fakeProv{"planner": {replies: []ai.Chunk{{Delta: onePlan}}}, "fix": {}})
	o := planOptions(h, 1)
	o.PlanReviewRounds = 1
	o.PlanReview = func(context.Context, string) (bool, string, *ai.Usage, error) { return false, "too small", nil, nil }
	run, _ := Start(context.Background(), planRun(), o)
	if end := wait(t, run); end.Kind != RunDone {
		t.Fatalf("end %+v", end)
	}
	ev, ok := h.find(NodePlanReview, "planner")
	if !ok || ev.Verdict != "reject" || !strings.Contains(ev.Output, "rounds") {
		t.Fatalf("event %+v", ev)
	}
}
