package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (AH-12): usage_summary gives this session's totals per model and
// per agent step from the USAGE view's log; a subscription step says
// "subscription" and has no dollar cost; no URL, key or prompt is in it.
func TestAIUsageSummaryTool(t *testing.T) {
	m, call := aiItermocTestModel(t)
	cost := 0.02
	m.ai.usageLog = []aiUsageEntry{
		{Agent: "Planner", Model: "gpt-x", U: ai.Usage{Input: 1000, Output: 200, Requests: 1}, Cost: &cost},
		{Agent: "Claude Code", Model: "claude-code", U: ai.Usage{Input: 500, Output: 50, Requests: 1}, Sub: true},
	}
	out := aiItermocRun(t, m, call, "usage_summary", nil)
	var v struct {
		ByModel []struct {
			Name      string   `json:"name"`
			TokensIn  int      `json:"input"`
			TokensOut int      `json:"output"`
			CostUSD   *float64 `json:"cost_usd"`
			Billing   string   `json:"billing"`
		} `json:"by_model"`
		ByAgent []struct {
			Name string `json:"name"`
		} `json:"by_agent"`
		Total struct {
			TokensIn int `json:"input"`
		} `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(v.ByModel) != 2 || len(v.ByAgent) != 2 || v.Total.TokensIn != 1500 {
		t.Fatalf("shape:\n%s", out)
	}
	for _, r := range v.ByModel {
		switch r.Name {
		case "gpt-x":
			if r.CostUSD == nil || *r.CostUSD != 0.02 || r.Billing != "per token" || r.TokensIn != 1000 || r.TokensOut != 200 {
				t.Errorf("gpt-x: %+v", r)
			}
		case "claude-code":
			if r.CostUSD != nil || r.Billing != "subscription" {
				t.Errorf("subscription row has a price: %+v", r)
			}
		default:
			t.Errorf("row %q", r.Name)
		}
	}
	for _, bad := range []string{"http", "sk-", "$"} {
		if strings.Contains(out, bad) {
			t.Errorf("usage_summary holds %q:\n%s", bad, out)
		}
	}
	m.ai.usageLog = nil
	if out := aiItermocRun(t, m, call, "usage_summary", nil); !strings.Contains(out, `"by_model": []`) {
		t.Errorf("empty session:\n%s", out)
	}
}

// Spec (AH-12): usage_summary reads only, so a read-only step is offered it.
func TestAIUsageSummaryOfferedToReadOnlySteps(t *testing.T) {
	_, call := aiItermocTestModel(t)
	tl, ok := ai.FindTool(aiItermocTools(call), "usage_summary")
	if !ok || tl.Mutating || tl.Gate != nil || tl.Class != "" {
		t.Fatalf("usage_summary is not a plain read tool: %+v", tl)
	}
	got := aiFilterTools(aiItermocTools(call), agentdag.ToolRule{Preset: agentdag.PresetReadOnly})
	if _, ok := ai.FindTool(got, "usage_summary"); !ok {
		t.Error("a read-only step does not get usage_summary")
	}
}
