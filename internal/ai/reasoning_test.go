package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

func reasonBody(t *testing.T, kind, model, level string) string {
	t.Helper()
	req := Request{Messages: []Message{{Role: RoleUser, Content: "q"}}, Reasoning: level}
	var body map[string]any
	if kind == KindAnthropic {
		body = anthropicBody(req, model, Config{Kind: kind, PromptCache: PromptCacheOff})
	} else {
		body = openAIBody(req, model, Config{Kind: kind})
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// OMX-5: a reasoning level is sent the way each model takes it.
func TestReasoningAnthropic(t *testing.T) {
	for _, c := range []struct{ model, level, want, not string }{
		{"claude-opus-5-5", "", "", `"thinking"`},
		{"claude-opus-5-5", "high", `"output_config":{"effort":"high"}`, "budget_tokens"},
		{"claude-opus-5-5", "max", `"thinking":{"type":"adaptive"}`, "budget_tokens"},
		{"claude-sonnet-5-5", "xhigh", `"effort":"xhigh"`, ""},
		{"claude-opus-4-6", "xhigh", `"effort":"high"`, "xhigh"}, // 4.6 has no xhigh
		{"claude-haiku-4-5", "high", `"thinking":{"budget_tokens":16384,"type":"enabled"}`, "output_config"},
		{"claude-sonnet-4-5-20250929", "low", `"budget_tokens":2048`, "effort"},
		{"claude-haiku-4-5", "max", `"budget_tokens":31999`, ""},
		{"claude-opus-5-5", "bogus", "", `"thinking"`},
	} {
		s := reasonBody(t, KindAnthropic, c.model, c.level)
		if c.want != "" && !strings.Contains(s, c.want) {
			t.Errorf("%s %s: want %s in %s", c.model, c.level, c.want, s)
		}
		if c.not != "" && strings.Contains(s, c.not) {
			t.Errorf("%s %s: %s must not be sent: %s", c.model, c.level, c.not, s)
		}
	}
}

// A small max_tokens keeps the thinking budget below it (or sends none).
func TestReasoningAnthropicBudgetBelowMaxTokens(t *testing.T) {
	req := Request{Messages: []Message{{Role: RoleUser, Content: "q"}}, Reasoning: "max", MaxTokens: 4000}
	b, _ := json.Marshal(anthropicBody(req, "claude-haiku-4-5", Config{Kind: KindAnthropic}))
	if !strings.Contains(string(b), `"budget_tokens":3999`) {
		t.Errorf("budget not capped: %s", b)
	}
	req.MaxTokens = 1000
	b, _ = json.Marshal(anthropicBody(req, "claude-haiku-4-5", Config{Kind: KindAnthropic}))
	if strings.Contains(string(b), "thinking") {
		t.Errorf("no room for thinking, none is sent: %s", b)
	}
}

func TestReasoningOpenAI(t *testing.T) {
	for _, c := range []struct{ kind, model, level, want, not string }{
		{KindOpenAI, "gpt-5-mini", "low", `"reasoning_effort":"low"`, ""},
		{KindOpenAI, "o3", "max", `"reasoning_effort":"high"`, ""},
		{KindOpenAI, "gpt-4.1-mini", "high", "", "reasoning"}, // not a reasoning model: a 400
		{KindOpenRouter, "openai/gpt-5", "xhigh", `"reasoning":{"effort":"high"}`, "reasoning_effort"},
		{KindOllama, "qwen", "high", "", "reasoning"},
		{KindCompatible, "x", "high", "", "reasoning"},
		{KindOpenAI, "gpt-5", "", "", "reasoning"},
	} {
		s := reasonBody(t, c.kind, c.model, c.level)
		if c.want != "" && !strings.Contains(s, c.want) {
			t.Errorf("%s %s %s: want %s in %s", c.kind, c.model, c.level, c.want, s)
		}
		if c.not != "" && strings.Contains(s, c.not) {
			t.Errorf("%s %s %s: %s must not be sent: %s", c.kind, c.model, c.level, c.not, s)
		}
	}
}

// The level of the request wins over the slot's; a slot's comes from
// ai_models.<slot>.reasoning, the main slot's from ai_reasoning (not the
// main config: ghost text uses it).
func TestReasoningFromConfig(t *testing.T) {
	req := Request{Messages: []Message{{Role: RoleUser, Content: "q"}}}
	b, _ := json.Marshal(anthropicBody(req, "claude-opus-5-5", Config{Kind: KindAnthropic, Reasoning: "low"}))
	if !strings.Contains(string(b), `"effort":"low"`) {
		t.Errorf("config level not sent: %s", b)
	}
	req.Reasoning = "max"
	b, _ = json.Marshal(anthropicBody(req, "claude-opus-5-5", Config{Kind: KindAnthropic, Reasoning: "low"}))
	if !strings.Contains(string(b), `"effort":"max"`) {
		t.Errorf("request level does not win: %s", b)
	}
	getenv := func(string) string { return "" }
	main := Resolve(Settings{Provider: KindAnthropic, Reasoning: "High"}, Credentials{Keys: map[string]string{KindAnthropic: "k"}}, getenv)
	if main.Reasoning != "" {
		t.Errorf("the main config (ghost text too) must not think: %q", main.Reasoning)
	}
	cfgs, errs := ResolveSlots(Settings{Reasoning: "High", Models: map[string]ModelSlot{"w": {Provider: KindAnthropic, Reasoning: "low"}, "bad": {Provider: KindAnthropic, Reasoning: "huge"}}}, main, Credentials{}, getenv)
	if cfgs["w"].Reasoning != "low" || cfgs[SlotMain].Reasoning != "high" || cfgs[SlotQuick].Reasoning != "" {
		t.Errorf("slot levels: w %q main %q quick %q", cfgs["w"].Reasoning, cfgs[SlotMain].Reasoning, cfgs[SlotQuick].Reasoning)
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "reasoning") {
		t.Errorf("a bad level is reported: %v", errs)
	}
}

func TestNextReasoning(t *testing.T) {
	got := []string{}
	l := ""
	for i := 0; i < 7; i++ {
		l = NextReasoning(l)
		got = append(got, l)
	}
	if strings.Join(got, ",") != "low,medium,high,xhigh,max,,low" {
		t.Errorf("cycle: %v", got)
	}
}
