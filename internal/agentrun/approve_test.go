package agentrun

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// G-PLAN: the plan is approved before a step that may write files or run
// commands starts.

func (h *harness) count(kind EventKind) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, e := range h.events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

// last returns the last event of kind.
func (h *harness) last(kind EventKind) (Event, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.events) - 1; i >= 0; i-- {
		if h.events[i].Kind == kind {
			return h.events[i], true
		}
	}
	return Event{}, false
}

func nodeIDs(ns []agentdag.Node) string {
	var out []string
	for _, n := range ns {
		out = append(out, n.ID)
	}
	return strings.Join(out, ",")
}

// reviewRun is a read-only step, then a writing one.
func reviewRun() agentdag.Run {
	r := chain("read", "write")
	r.Nodes[0].Tools.Preset = agentdag.PresetReadOnly
	return r
}

func approvalOptions(h *harness, mode string) Options {
	o := planOptions(h, 1)
	o.Approval = mode
	return o
}

func twoProvs() map[string]*fakeProv {
	return map[string]*fakeProv{"read": {replies: []ai.Chunk{{Delta: "found it"}}}, "write": {replies: []ai.Chunk{{Delta: "fixed"}}}}
}

// Spec: with ask, a fixed plan that writes is shown before its first
// step; nothing runs until Approve; then every step is approved and runs.
func TestApprovalAsksBeforeTheFirstStep(t *testing.T) {
	h := newHarness(twoProvs())
	run, err := Start(context.Background(), reviewRun(), approvalOptions(h, ApprovalAsk))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the approval card", func() bool { return h.count(RunAwaitApproval) == 1 })
	ev, _ := h.last(RunAwaitApproval)
	if got := nodeIDs(ev.Nodes); got != "read,write" || ev.Err != "" {
		t.Fatalf("await %q err %q", got, ev.Err)
	}
	if h.provs["read"].callCount() != 0 || states(run.Snapshot()) != "read=pending write=pending" {
		t.Fatalf("a step ran before approval: %s", states(run.Snapshot()))
	}
	run.Approve()
	if end := wait(t, run); end.Kind != RunDone || end.Output != "fixed" {
		t.Fatalf("end %+v", end)
	}
	if got := strings.Join(run.Snapshot().Approved, ","); got != "read,write" {
		t.Errorf("approved %q", got)
	}
	if h.count(RunApproved) != 1 || h.count(RunAwaitApproval) != 1 {
		t.Errorf("approved %d awaits %d", h.count(RunApproved), h.count(RunAwaitApproval))
	}
}

// Spec: a read-only plan runs without the card under ask; always shows
// it; off (auto) never does.
func TestApprovalReadOnlyPlanSkipsTheCard(t *testing.T) {
	ro := chain("a", "b")
	for i := range ro.Nodes {
		ro.Nodes[i].Tools.Preset = agentdag.PresetReadOnly
	}
	for _, c := range []struct {
		mode string
		run  agentdag.Run
		ask  bool
	}{{ApprovalAsk, ro, false}, {ApprovalAlways, ro, true}, {ApprovalOff, chain("a", "b"), false}} {
		h := newHarness(map[string]*fakeProv{"a": {}, "b": {}})
		run, err := Start(context.Background(), c.run, approvalOptions(h, c.mode))
		if err != nil {
			t.Fatal(err)
		}
		if c.ask {
			waitFor(t, "the card", func() bool { return h.count(RunAwaitApproval) == 1 })
			run.Approve()
		}
		if end := wait(t, run); end.Kind != RunDone {
			t.Fatalf("%q: end %+v", c.mode, end)
		}
		if got := h.count(RunAwaitApproval) > 0; got != c.ask {
			t.Errorf("%q: asked %v", c.mode, got)
		}
	}
}

// Spec: Acts decides what a step does; a step that only runs commands
// asks too.
func TestApprovalActsRunsCommands(t *testing.T) {
	h := newHarness(map[string]*fakeProv{"t": {}})
	r := chain("t")
	r.Nodes[0].Tools.Preset = agentdag.PresetReadOnly
	o := approvalOptions(h, ApprovalAsk)
	o.Acts = func(n agentdag.Node) (bool, bool) { return false, n.ID == "t" }
	run, err := Start(context.Background(), r, o)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the card", func() bool { return h.count(RunAwaitApproval) == 1 })
	run.Approve()
	if end := wait(t, run); end.Kind != RunDone {
		t.Fatalf("end %+v", end)
	}
}

// Spec: Deny stops the run; nothing ran; the end says the plan was
// denied.
func TestApprovalDeny(t *testing.T) {
	h := newHarness(twoProvs())
	run, err := Start(context.Background(), reviewRun(), approvalOptions(h, ApprovalAsk))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the card", func() bool { return h.count(RunAwaitApproval) == 1 })
	run.Deny()
	end := wait(t, run)
	if end.Kind != RunCancelled || end.Err != DeniedErr {
		t.Fatalf("end %+v", end)
	}
	if h.provs["read"].callCount()+h.provs["write"].callCount() != 0 {
		t.Error("a step ran after Deny")
	}
	if got := states(run.Snapshot()); got != "read=cancelled write=cancelled" {
		t.Errorf("states %s", got)
	}
}

// Spec: a planner runs first (it only reads); its plan is shown once it
// is there, then runs after Approve.
func TestApprovalShowsThePlannersPlan(t *testing.T) {
	h := newHarness(map[string]*fakeProv{
		"planner": {replies: []ai.Chunk{{Delta: diamondPlan}}},
		"map":     {}, "left": {}, "right": {}, "verify": {replies: []ai.Chunk{{Delta: "all good"}}},
	})
	run, err := Start(context.Background(), planRun(), approvalOptions(h, ApprovalAsk))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the card", func() bool { return h.count(RunAwaitApproval) == 1 })
	ev, _ := h.last(RunAwaitApproval)
	if got := nodeIDs(ev.Nodes); got != "map,left,right,verify" {
		t.Fatalf("await %q", got)
	}
	if h.provs["planner"].callCount() != 1 || h.provs["map"].callCount() != 0 {
		t.Fatal("the planner must run first and nothing after it")
	}
	run.Approve()
	if end := wait(t, run); end.Kind != RunDone || end.Output != "all good" {
		t.Fatalf("end %+v", end)
	}
}

// Spec: Edit changes a task, drops a step and adds one; the card comes
// back with the new plan; Approve runs it.
func TestApprovalEditPlan(t *testing.T) {
	h := newHarness(map[string]*fakeProv{
		"planner": {replies: []ai.Chunk{{Delta: diamondPlan}}},
		"map":     {}, "left": {}, "right": {}, "verify": {replies: []ai.Chunk{{Delta: "checked"}}}, "docs": {},
	})
	run, err := Start(context.Background(), planRun(), approvalOptions(h, ApprovalAsk))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the card", func() bool { return h.count(RunAwaitApproval) == 1 })
	text := PlanText(run.Snapshot())
	if !strings.Contains(text, `"id": "left"`) || !strings.Contains(text, `"prompt": "Fix the left part."`) || strings.Contains(text, `"planner"`+",") {
		t.Fatalf("plan text:\n%s", text)
	}
	edited := `{"nodes": [
  {"id": "map", "title": "Map", "prompt": "List only Go files.", "deps": ["planner"]},
  {"id": "left", "title": "Left side", "prompt": "Fix the left part.", "deps": ["map"]},
  {"id": "docs", "title": "Docs", "prompt": "Write the notes.", "category": "deep", "deps": ["map"]},
  {"id": "verify", "title": "Verify", "prompt": "Check both fixes.", "deps": ["left", "docs"]}
]}`
	run.EditPlan(edited)
	waitFor(t, "the card again", func() bool { return h.count(RunAwaitApproval) == 2 })
	pe, ok := h.find(RunPlanEdited, "")
	if !ok || nodeIDs(pe.Nodes) != "docs" || pe.Nodes[0].Slot != "strong" {
		t.Fatalf("edited event %+v", pe)
	}
	ev, _ := h.last(RunAwaitApproval)
	if got := nodeIDs(ev.Nodes); got != "map,left,verify,docs" || ev.Err != "" {
		t.Fatalf("await %q err %q", got, ev.Err)
	}
	run.Approve()
	if end := wait(t, run); end.Kind != RunDone || end.Output != "checked" {
		t.Fatalf("end %+v", end)
	}
	if h.provs["right"].callCount() != 0 {
		t.Error("the dropped step ran")
	}
	if strings.Contains(h.input("map"), "\"nodes\"") {
		t.Errorf("an edited step got the plan JSON:\n%s", h.input("map"))
	}
	if !strings.Contains(h.input("map"), "List only Go files.") || !strings.Contains(h.input("verify"), "### Output of step \"Docs\"") {
		t.Errorf("inputs:\nmap %s\nverify %s", h.input("map"), h.input("verify"))
	}
	if got := states(run.Snapshot()); !strings.Contains(got, "right=skipped") || !strings.Contains(got, "docs=completed") {
		t.Errorf("states %s", got)
	}
}

// Spec: an edit that does not fit keeps the plan and the card says why.
func TestApprovalBadEditKeepsThePlan(t *testing.T) {
	h := newHarness(twoProvs())
	run, err := Start(context.Background(), reviewRun(), approvalOptions(h, ApprovalAsk))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the card", func() bool { return h.count(RunAwaitApproval) == 1 })
	run.EditPlan(`{"nodes": [{"id": "write", "prompt": "x", "deps": ["nope"]}, {"id": "read", "prompt": ""}]}`)
	waitFor(t, "the card again", func() bool { return h.count(RunAwaitApproval) == 2 })
	ev, _ := h.last(RunAwaitApproval)
	if !strings.Contains(ev.Err, `unknown dep "nope"`) || nodeIDs(ev.Nodes) != "read,write" {
		t.Fatalf("await %+v", ev)
	}
	run.Deny()
	wait(t, run)
}

func TestApplyPlanEditErrors(t *testing.T) {
	lr := loopRun(3)
	cases := []struct {
		run  agentdag.Run
		text string
		want string
	}{
		{reviewRun(), "no json here", "no plan JSON"},
		{reviewRun(), `{"nodes": [{"id": "read", "bogus": 1}]}`, "does not parse"},
		{reviewRun(), `{"nodes": []}`, "no steps left"},
		{reviewRun(), `{"nodes": [{"id": "new", "deps": []}]}`, `new step "new" has an empty prompt`},
		{reviewRun(), `{"nodes": [{"id": "read", "deps": ["write"]}, {"id": "write", "deps": ["read"]}]}`, "cycle"},
		{lr, `{"nodes": [{"id": "do", "deps": []}]}`, `step "verify" checks the work of the loop`},
		{lr, `{"nodes": [{"id": "do"}, {"id": "verify", "deps": ["do"]}, {"id": "fix", "prompt": "x"}]}`, "loop's fix step"},
	}
	for _, c := range cases {
		_, err := ApplyPlanEdit(c.run, c.text, nil)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v, want %q", c.text, err, c.want)
		}
	}
	done := reviewRun()
	done.Nodes[0].State = agentdag.StateCompleted
	if _, err := ApplyPlanEdit(done, `{"nodes": [{"id": "read"}, {"id": "write", "deps": ["read"]}]}`, nil); err == nil || !strings.Contains(err.Error(), "already ran") {
		t.Errorf("a step that ran: %v", err)
	}
	if got := PlanText(done); strings.Contains(got, `"read"`+",\n      \"title\"") || !strings.Contains(got, `"deps": [
        "read"
      ]`) {
		t.Errorf("plan text of a half-run plan:\n%s", got)
	}
}

// Spec (OM-3): the loop's fix step is part of the approved plan: verify
// fails, round 2 runs the fix without a new card and says so
// (RunPlanKept).
func TestApprovalLoopFixNeedsNoNewCard(t *testing.T) {
	h := newHarness(map[string]*fakeProv{
		"do":     {},
		"verify": {replies: []ai.Chunk{{Delta: "VERDICT: FAIL\nbroken"}, {Delta: "VERDICT: PASS"}}},
		"fix":    {},
	})
	o := approvalOptions(h, ApprovalAsk)
	o.Acts = func(n agentdag.Node) (bool, bool) { return n.ID != "verify", n.ID == "verify" }
	run, err := Start(context.Background(), loopRun(3), o)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the card", func() bool { return h.count(RunAwaitApproval) == 1 })
	ev, _ := h.last(RunAwaitApproval)
	if got := nodeIDs(ev.Nodes); got != "do,verify,fix" {
		t.Fatalf("await %q (the fix step belongs to the plan)", got)
	}
	run.Approve()
	if end := wait(t, run); end.Kind != RunDone || end.Verdict != VerdictPass {
		t.Fatalf("end %+v", end)
	}
	if n := h.count(RunAwaitApproval); n != 1 {
		t.Errorf("asked %d times", n)
	}
	if k, ok := h.last(RunPlanKept); !ok || k.Round != 2 || nodeIDs(k.Nodes) != "fix" {
		t.Errorf("plan kept %+v", k)
	}
}

// Spec: a new plan after the approval that adds writing steps asks
// again.
func TestApprovalNewWritingStepsAskAgain(t *testing.T) {
	plan := "```json\n" + `{"nodes": [{"id": "more", "title": "More", "prompt": "write more"}]}` + "\n```"
	h := newHarness(map[string]*fakeProv{"first": {}, "planner": {replies: []ai.Chunk{{Delta: plan}}}, "more": {replies: []ai.Chunk{{Delta: "more done"}}}})
	r := agentdag.Run{ID: "late", Nodes: []agentdag.Node{{ID: "first", Title: "First"},
		{ID: "planner", Title: "Planner", Plan: true, Tools: agentdag.ToolRule{Preset: agentdag.PresetReadOnly}}},
		Edges: agentdag.Chain("first", "planner")}
	run, err := Start(context.Background(), r, approvalOptions(h, ApprovalAsk))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first card", func() bool { return h.count(RunAwaitApproval) == 1 })
	run.Approve()
	waitFor(t, "the second card", func() bool { return h.count(RunAwaitApproval) == 2 })
	if ev, _ := h.last(RunAwaitApproval); nodeIDs(ev.Nodes) != "more" {
		t.Fatalf("second card %+v", ev.Nodes)
	}
	if h.provs["more"].callCount() != 0 {
		t.Fatal("the new step ran before approval")
	}
	if _, ok := h.find(RunPlanKept, ""); ok {
		t.Error("no plan-kept note when the run asks")
	}
	run.Approve()
	if end := wait(t, run); end.Kind != RunDone || end.Output != "more done" {
		t.Fatalf("end %+v", end)
	}
}

// Spec (OM-6): a resumed run whose plan was approved does not ask
// again; one that was not approved does.
func TestApprovalResumeKeepsTheApproval(t *testing.T) {
	h := newHarness(twoProvs())
	r := reviewRun()
	r.Nodes[0].State = agentdag.StateCompleted
	r.Approved = []string{"read", "write"}
	o := approvalOptions(h, ApprovalAsk)
	o.Outputs = map[string]string{"read": "found it"}
	run, err := Start(context.Background(), r, o)
	if err != nil {
		t.Fatal(err)
	}
	if end := wait(t, run); end.Kind != RunDone || h.count(RunAwaitApproval) != 0 {
		t.Fatalf("end %+v, awaits %d", end, h.count(RunAwaitApproval))
	}

	h2 := newHarness(twoProvs())
	r.Approved = nil
	run2, err := Start(context.Background(), r, approvalOptions(h2, ApprovalAsk))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the card", func() bool { return h2.count(RunAwaitApproval) == 1 })
	if ev, _ := h2.last(RunAwaitApproval); nodeIDs(ev.Nodes) != "write" {
		t.Errorf("await %q", nodeIDs(ev.Nodes))
	}
	run2.Cancel()
	if end := wait(t, run2); end.Kind != RunCancelled || end.Err != "" {
		t.Errorf("a plain cancel is not a deny: %+v", end)
	}
}
