package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// agent_run_* checks and cards (ai_agent_run_tools.go): every refusal of
// a start, what the start card says in each setting, the stop checks
// and the "by AI" lines.

func covRunSnap() aiRunSnap {
	return aiRunSnap{
		profiles:  map[string]ai.Profile{"review": covBuiltinProfile("review"), "house": {Name: "house", Source: ai.ProfileSourceProject}},
		untrusted: map[string]bool{"house": true},
		slots:     map[string]bool{ai.SlotMain: true, ai.SlotQuick: true},
		slotModel: map[string]string{ai.SlotMain: "claude-sonnet-5-5"},
		parallel:  2, worktrees: true, aiCostOK: true,
		runs: map[string]string{"r1": "It runs now."},
	}
}

func TestCovAIRunStartRefusals(t *testing.T) {
	sn := covRunSnap()
	step := []any{map[string]any{"title": "Do", "prompt": "Do it."}}
	for _, c := range []struct {
		a    map[string]any
		want string
	}{
		{map[string]any{"task": strings.Repeat("x", aiRunTaskMax+1), "profile": "review"}, "keep it under"},
		{map[string]any{"task": "t", "profile": "review", "worktree": "maybe"}, `worktree must be "auto" or "off"`},
		{map[string]any{"task": "t", "profile": "review", "steps": step}, "not both"},
		{map[string]any{"task": "t", "profile": "nope"}, `there is no profile "nope"`},
		{map[string]any{"task": "t", "profile": "house"}, "has not trusted"},
		{map[string]any{"task": "t"}, "profile or steps is required"},
		{map[string]any{"task": "t", "steps": []any{"bad"}}, "step 1 is not an object"},
		{map[string]any{"task": "t", "steps": []any{map[string]any{"title": "Do", "prompt": "x", "slot": "nope"}}}, "models_list names the slots"},
	} {
		if _, err := sn.checkStart(c.a); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v", c.a, err)
		}
		if got := sn.describeStart(c.a); !strings.HasPrefix(got, "Start an agent run (refused: ") {
			t.Errorf("refused card = %q", got)
		}
	}
	sn.active = aiRunMaxActive
	if _, err := sn.checkStart(map[string]any{"task": "t", "profile": "review"}); err == nil || !strings.Contains(err.Error(), "wait or run already") {
		t.Errorf("limit: %v", err)
	}
}

func TestCovAIRunStartCards(t *testing.T) {
	sn := covRunSnap()
	long := strings.Repeat("line\n", 20)
	steps := []any{map[string]any{"title": "Plan", "prompt": "Make a plan.\nThen more.", "planner": true}, map[string]any{"prompt": "Do it.", "after": []any{"step-1"}}}
	got := sn.describeStart(map[string]any{"task": long, "steps": steps, "worktree": "off", "budget_usd": 2})
	covAIMustContain(t, "steps card", got, "steps the AI wrote (2 steps)", "1 · Plan · ", " · planner", "    Make a plan.", "2 · step-2 · ",
		"  …\n", "Worktrees: off (every step works in this folder)", "Budget: $2", "Started by AI in this chat.")
	if !strings.Contains(got, "→ claude-sonnet-5-5") {
		t.Errorf("the card does not name the model:\n%s", got)
	}
	sn.worktrees = false
	sn.caller = aiRunCaller{budget: 1.5, parentRun: "r0", depth: 1}
	sn.aiCost, sn.aiCostOK, sn.chatTitle = 0.25, false, "fix the parser"
	got = sn.describeStart(map[string]any{"task": "go", "profile": "review"})
	covAIMustContain(t, "profile card", got, "Start an agent run with profile review", "Worktrees: off in your settings",
		"Budget: what is left of this run's $1.50", "cost $0.250 (some models have no price) so far", `Started by AI in the chat "fix the parser", from run r0.`)
	sn.caller.depth = aiRunMaxDepth
	if _, err := sn.checkStart(map[string]any{"task": "go", "profile": "review"}); err == nil || !strings.Contains(err.Error(), "at most 2 levels") {
		t.Errorf("depth: %v", err)
	}
	if got := sn.describeStart(map[string]any{"task": "go", "profile": "review", "note": "sk-ant-api03-" + strings.Repeat("b", 40)}); got != aiSecretCardText {
		t.Errorf("secret card = %q", got)
	}
}

func TestCovAIRunStopChecksAndByLines(t *testing.T) {
	sn := covRunSnap()
	sn.caller.parentRun = "r1"
	for id, want := range map[string]string{"": "run_id is required", "r1": "cannot stop its own run", "r9": `run "r9" does not wait or run`} {
		if err := sn.checkStop(map[string]any{"run_id": id}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", id, err)
		}
	}
	if got := sn.describeStop(map[string]any{"run_id": "r9"}); !strings.Contains(got, "refused") {
		t.Errorf("stop card = %q", got)
	}
	m := aiTestModel(t)
	if _, err := aiRunStopTool(covSyncCall(&m), sn).Run(context.Background(), map[string]any{"run_id": "r1"}); err == nil {
		t.Error("a step stopped its own run")
	}
	if aiRunByChip(nil) != "" || aiRunByLine(&ai.StoredRun{}) != "" {
		t.Error("a user's run has a by-AI chip")
	}
	by := &ai.StoredRun{By: &ai.RunBy{ChatTitle: "fix it", ParentRun: "r1"}}
	if got := aiRunByLine(by); got != `started by AI in the chat "fix it" · from run r1` {
		t.Errorf("by line = %q", got)
	}
	if cost, ok := aiRunMsgCost(ai.StoredMessage{Steps: []ai.StoredStep{{Usage: &ai.Usage{Input: 1}}}}); cost != 0 || ok {
		t.Errorf("unpriced step: %v %v", cost, ok)
	}
}

func covBuiltinProfile(name string) ai.Profile {
	for _, p := range ai.BuiltinProfiles() {
		if p.Name == name {
			return p
		}
	}
	return ai.Profile{}
}
