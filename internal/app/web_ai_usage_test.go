package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (MOB-73): ai.usage gives the phone this session's usage per model
// and per agent step from the USAGE view's log: a priced step has its
// cost, a subscription step none; names and numbers only.
func TestWebAIUsage(t *testing.T) {
	m, _ := aiItermocTestModel(t)
	cost := 0.02
	m.ai.usageLog = []aiUsageEntry{
		{Agent: "Planner", Model: "gpt-x", U: ai.Usage{Input: 1000, Output: 200, Requests: 1}, Cost: &cost},
		{Agent: "Claude Code", Model: "claude-code", U: ai.Usage{Input: 500, Output: 50, Requests: 1}, Sub: true},
	}
	u := m.webAIUsageData()
	if len(u.ByModel) != 2 || len(u.ByAgent) != 2 {
		t.Fatalf("lines: %+v", u)
	}
	byName := map[string]int{}
	for i, l := range u.ByModel {
		byName[l.Name] = i
	}
	gx := u.ByModel[byName["gpt-x"]]
	if !gx.Priced || gx.CostUSD != 0.02 || gx.Input != 1000 || gx.Output != 200 || gx.Billing != "per token" {
		t.Errorf("gpt-x: %+v", gx)
	}
	cc := u.ByModel[byName["claude-code"]]
	if cc.Priced || cc.CostUSD != 0 || cc.Billing != "subscription" {
		t.Errorf("subscription: %+v", cc)
	}
	if u.Total.Input != 1500 || u.Total.Calls != 2 {
		t.Errorf("total: %+v", u.Total)
	}
	b, _ := json.Marshal(u)
	for _, bad := range []string{"http", "key", "prompt"} {
		if strings.Contains(strings.ToLower(string(b)), bad) {
			t.Errorf("ai.usage carries %q: %s", bad, b)
		}
	}
	m.ai.usageLog = nil
	if u := m.webAIUsageData(); len(u.ByModel) != 0 || u.Total.Calls != 0 {
		t.Errorf("empty log: %+v", u)
	}
}
