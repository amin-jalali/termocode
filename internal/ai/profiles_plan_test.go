package ai

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
)

// Spec (AP-8): the built-in plan profile is one planner node on the deep
// category with read-only tools; its run is a planned run.
func TestBuiltinPlanProfile(t *testing.T) {
	var plan Profile
	for _, p := range BuiltinProfiles() {
		if p.Name == ProfilePlan {
			plan = p
		}
	}
	if !plan.Planned() {
		t.Fatalf("plan profile %+v", plan)
	}
	r := plan.Run("r")
	if len(r.Nodes) != 1 || !r.Nodes[0].Plan || r.Nodes[0].Category != agentdag.CategoryDeep ||
		r.Nodes[0].Slot != SlotMain || r.Nodes[0].Tools.Preset != agentdag.PresetReadOnly {
		t.Errorf("plan run %+v", r)
	}
	for _, p := range BuiltinProfiles() {
		if p.Name != ProfilePlan && p.Name != ProfileUntilDone && p.Planned() {
			t.Errorf("%s must not be planned", p.Name)
		}
	}
	cats := plan.CategorySlots()
	if cats[agentdag.CategoryQuick] != SlotQuick || cats[agentdag.CategoryDeep] != SlotMain {
		t.Errorf("default category slots %v", cats)
	}
	mine := Profile{Categories: map[string]string{"quick": "gpt"}}
	if c := mine.CategorySlots(); c["quick"] != "gpt" || c["deep"] != SlotMain {
		t.Errorf("profile category slots %v", c)
	}
}

// Spec (AP-8): plan and budget_usd in profiles.json.
func TestParseProfilesPlanAndBudget(t *testing.T) {
	ok := `{"profiles": {"big": {"budget_usd": 0.5, "steps": [{"id": "p", "plan": true}, {"id": "sum"}]}}}`
	ps, err := ParseProfiles([]byte(ok), "f.json", ProfileSourceUser, testSlots, 1)
	if err != nil || len(ps) != 1 || ps[0].BudgetUSD != 0.5 || !ps[0].Planned() || !ps[0].Run("r").Nodes[0].Plan {
		t.Fatalf("parse %+v %v", ps, err)
	}
	for _, tc := range []struct{ body, want string }{
		{`{"profiles": {"x": {"budget_usd": -1, "steps": [{"id": "a"}]}}}`, "budget_usd"},
		{`{"profiles": {"x": {"steps": [{"id": "a", "plan": true}, {"id": "b", "plan": true}]}}}`, "one planner"},
	} {
		if _, err := ParseProfiles([]byte(tc.body), "f.json", ProfileSourceUser, testSlots, 1); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err %v, want %q", tc.body, err, tc.want)
		}
	}
}

// Spec (AP-8): planned runs run up to 3 nodes at once unless
// ai_parallel_max says otherwise; other runs keep the default of 1.
func TestSettingsPlannedParallel(t *testing.T) {
	if (Settings{}).PlannedParallel() != 3 || (Settings{}).Parallel() != 1 {
		t.Error("defaults")
	}
	if (Settings{ParallelMax: 1}).PlannedParallel() != 1 || (Settings{ParallelMax: 5}).PlannedParallel() != 5 {
		t.Error("ai_parallel_max must win")
	}
	if (Settings{ParallelMax: 99}).PlannedParallel() != MaxParallel {
		t.Error("cap")
	}
}
