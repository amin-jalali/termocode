package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

func cacheReq() Request {
	return Request{
		Messages: []Message{{Role: RoleSystem, Content: "rules"}, {Role: RoleUser, Content: "hi"}},
		Tools: []ToolSpec{
			{Name: "a", Description: "A", Schema: map[string]any{"type": "object"}},
			{Name: "b", Description: "B", Schema: map[string]any{"type": "object"}},
		},
	}
}

func bodyJSON(t *testing.T, body map[string]any) string {
	t.Helper()
	b, err := marshalBody(body)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("body is not JSON: %v\n%s", err, b)
	}
	return string(b)
}

// OMX-1: the system prompt, the last tool and the growing conversation
// (top-level automatic caching) are marked for the prompt cache.
func TestAnthropicBodyPromptCacheDefault(t *testing.T) {
	body := anthropicBody(cacheReq(), "m", Config{Kind: KindAnthropic})
	s := bodyJSON(t, body)
	sys := body["system"].([]map[string]any)
	if sys[len(sys)-1]["cache_control"] == nil {
		t.Errorf("last system block has no cache_control: %s", s)
	}
	if !strings.Contains(s, `"name":"b","cache_control":{"type":"ephemeral"}}`) {
		t.Errorf("last tool has no cache_control: %s", s)
	}
	if strings.Count(s, `"cache_control"`) != 3 {
		t.Errorf("want 3 breakpoints (tools, system, top level): %s", s)
	}
	if strings.Contains(s, `"ttl"`) {
		t.Errorf("default is the 5 minute cache (no ttl): %s", s)
	}
}

func TestAnthropicBodyPromptCacheOneHour(t *testing.T) {
	s := bodyJSON(t, anthropicBody(cacheReq(), "m", Config{Kind: KindAnthropic, PromptCache: PromptCache1h}))
	if strings.Count(s, `{"ttl":"1h","type":"ephemeral"}`)+strings.Count(s, `{"type":"ephemeral","ttl":"1h"}`) != 3 {
		t.Errorf("every breakpoint needs the 1 hour ttl: %s", s)
	}
}

func TestAnthropicBodyPromptCacheOff(t *testing.T) {
	s := bodyJSON(t, anthropicBody(cacheReq(), "m", Config{Kind: KindAnthropic, PromptCache: PromptCacheOff}))
	if strings.Contains(s, "cache_control") {
		t.Errorf("off sends no cache_control: %s", s)
	}
}

// The spec's shared JSON (made once per turn) is not changed by the
// marker on the last tool.
func TestAnthropicPromptCacheKeepsToolWire(t *testing.T) {
	tool := Tool{Name: "x", Description: "X"}
	specs := []ToolSpec{tool.Spec()}
	req := Request{Messages: []Message{{Role: RoleUser, Content: "q"}}, Tools: specs}
	_ = bodyJSON(t, anthropicBody(req, "m", Config{Kind: KindAnthropic}))
	raw, err := specs[0].anthropicJSON()
	if err != nil || strings.Contains(string(raw), "cache_control") {
		t.Errorf("shared tool JSON changed: %s %v", raw, err)
	}
	s := bodyJSON(t, anthropicBody(req, "m", Config{Kind: KindAnthropic}))
	if strings.Count(s, `"cache_control"`) != 2 { // tool + top level (no system)
		t.Errorf("second body: %s", s)
	}
}

func TestPromptCacheSetting(t *testing.T) {
	for in, want := range map[string]string{"": "", "5m": "", "1h": PromptCache1h, "off": PromptCacheOff, "OFF ": PromptCacheOff, "bad": ""} {
		cfg := Resolve(Settings{Provider: KindAnthropic, PromptCache: in}, Credentials{Keys: map[string]string{KindAnthropic: "k"}}, func(string) string { return "" })
		if cfg.PromptCache != want {
			t.Errorf("ai_prompt_cache %q → %q, want %q", in, cfg.PromptCache, want)
		}
	}
	main := Config{Kind: KindAnthropic, PromptCache: PromptCache1h}
	cfgs, _ := ResolveSlots(Settings{Models: map[string]ModelSlot{"w": {Provider: KindAnthropic}}}, main, Credentials{}, func(string) string { return "" })
	if cfgs["w"].PromptCache != PromptCache1h {
		t.Errorf("slot does not follow ai_prompt_cache: %+v", cfgs["w"])
	}
}
