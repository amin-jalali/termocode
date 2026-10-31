package ai

import (
	"context"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// Real Anthropic SSE shapes: message_start carries the input side,
// message_delta the running output count.
func TestAnthropicUsageFromSSE(t *testing.T) {
	srv := sseServer(t, nil,
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5-5","content":[],"stop_reason":null,"usage":{"input_tokens":25,"cache_creation_input_tokens":100,"cache_read_input_tokens":2000,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":15}}`,
		`{"type":"message_stop"}`)
	defer srv.Close()
	p := NewProvider(Config{Kind: KindAnthropic, BaseURL: srv.URL, APIKey: "k"})
	text, last := Collect(p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}))
	if text != "ok" || last.Err != nil {
		t.Fatalf("text=%q err=%v", text, last.Err)
	}
	want := Usage{Model: "claude-sonnet-5-5", Input: 25, Output: 15, CacheRead: 2000, CacheWrite: 100, Requests: 1, PeakContext: 2125}
	if last.Usage == nil || *last.Usage != want {
		t.Fatalf("usage = %+v, want %+v", last.Usage, want)
	}
}

func TestAnthropicNoUsageIsNil(t *testing.T) {
	srv := sseServer(t, nil,
		`{"type":"message_start"}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"x"}}`,
		`{"type":"message_stop"}`)
	defer srv.Close()
	p := NewProvider(Config{Kind: KindAnthropic, BaseURL: srv.URL, APIKey: "k"})
	_, last := Collect(p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}))
	if last.Usage != nil {
		t.Fatalf("usage = %+v, want nil", last.Usage)
	}
}

// OpenAI with stream_options.include_usage: a last chunk with no choices
// and the usage; cached tokens are part of prompt_tokens.
func TestOpenAIUsageFromFinalChunk(t *testing.T) {
	var opts any
	srv := sseServer(t, func(_ *http.Request, body map[string]any) { opts = body["stream_options"] },
		`{"id":"c1","model":"gpt-4.1-mini-2025-04-14","choices":[{"index":0,"delta":{"content":"Hi"}}]}`,
		`{"id":"c1","model":"gpt-4.1-mini-2025-04-14","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`{"id":"c1","model":"gpt-4.1-mini-2025-04-14","choices":[],"usage":{"prompt_tokens":1200,"completion_tokens":40,"total_tokens":1240,"prompt_tokens_details":{"cached_tokens":1000}}}`,
		`[DONE]`)
	defer srv.Close()
	p := NewProvider(Config{Kind: KindOpenAI, BaseURL: srv.URL, APIKey: "k", Model: "gpt-4.1-mini"})
	_, last := Collect(p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}))
	m, _ := opts.(map[string]any)
	if m["include_usage"] != true {
		t.Errorf("stream_options = %v", opts)
	}
	want := Usage{Model: "gpt-4.1-mini-2025-04-14", Input: 200, Output: 40, CacheRead: 1000, Requests: 1, PeakContext: 1200}
	if last.Usage == nil || *last.Usage != want {
		t.Fatalf("usage = %+v, want %+v", last.Usage, want)
	}
}

// Servers that are not known to accept stream_options (a custom URL) do
// not get it; usage they send anyway is still read, also on a chunk
// that has choices (llama.cpp) and in Ollama's native field names.
func TestOpenAICompatibleUsageTolerant(t *testing.T) {
	cases := []struct {
		name  string
		kind  string
		send  bool
		event string
		want  Usage
	}{
		{"custom url, usage on last choice chunk", KindCompatible, false,
			`{"model":"local","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3}}`,
			Usage{Model: "local", Input: 10, Output: 3, Requests: 1, PeakContext: 10}},
		{"ollama native counts", KindOllama, true,
			`{"model":"qwen2.5-coder:7b","choices":[],"prompt_eval_count":30,"eval_count":7}`,
			Usage{Model: "qwen2.5-coder:7b", Input: 30, Output: 7, Requests: 1, PeakContext: 30}},
		{"lm studio usage", KindLMStudio, true,
			`{"model":"m","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
			Usage{Model: "m", Input: 5, Output: 2, Requests: 1, PeakContext: 5}},
		{"openrouter usage", KindOpenRouter, true,
			`{"model":"anthropic/claude-sonnet-5-5","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":2,"cost":0.0001}}`,
			Usage{Model: "anthropic/claude-sonnet-5-5", Input: 5, Output: 2, Requests: 1, PeakContext: 5}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var sent bool
			srv := sseServer(t, func(_ *http.Request, body map[string]any) { _, sent = body["stream_options"] },
				`{"choices":[{"index":0,"delta":{"content":"x"}}]}`, c.event, `[DONE]`)
			defer srv.Close()
			p := NewProvider(Config{Kind: c.kind, BaseURL: srv.URL, APIKey: "k", Model: "m"})
			_, last := Collect(p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}))
			if sent != c.send {
				t.Errorf("stream_options sent = %v, want %v", sent, c.send)
			}
			if last.Usage == nil || *last.Usage != c.want {
				t.Fatalf("usage = %+v, want %+v", last.Usage, c.want)
			}
		})
	}
}

func TestOpenAINoUsageIsNil(t *testing.T) {
	srv := sseServer(t, nil, `{"choices":[{"delta":{"content":"x"}}]}`, `[DONE]`)
	defer srv.Close()
	p := NewProvider(Config{Kind: KindOpenAI, BaseURL: srv.URL, APIKey: "k"})
	_, last := Collect(p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}))
	if last.Usage != nil {
		t.Fatalf("usage = %+v", last.Usage)
	}
}

// Agent.Run sums the usage of every request of the turn into EventDone.
func TestAgentSumsUsageIntoDone(t *testing.T) {
	tool := Tool{Name: "read_file", Run: func(context.Context, map[string]any) (string, error) { return "x", nil }}
	p := &scripted{tools: true, replies: []Chunk{
		{ToolCalls: []ToolCall{{ID: "1", Name: "read_file"}}, Usage: &Usage{Model: "m", Input: 100, Output: 10, CacheRead: 50, Requests: 1, PeakContext: 150}},
		{Delta: "done", Usage: &Usage{Model: "m", Input: 20, Output: 5, CacheRead: 200, CacheWrite: 7, Requests: 1, PeakContext: 227}},
	}}
	evs, _ := collectEvents(&Agent{Provider: p, Tools: []Tool{tool}, Native: true}, []Message{{Role: RoleUser, Content: "go"}})
	last := evs[len(evs)-1]
	want := Usage{Model: "m", Input: 120, Output: 15, CacheRead: 250, CacheWrite: 7, Requests: 2, PeakContext: 227}
	if last.Kind != EventDone || last.Usage == nil || *last.Usage != want {
		t.Fatalf("done = %+v usage %+v, want %+v", last, last.Usage, want)
	}
}

func TestAgentNoUsageLeavesDoneNil(t *testing.T) {
	evs, _ := collectEvents(&Agent{Provider: &scripted{}}, []Message{{Role: RoleUser, Content: "go"}})
	if last := evs[len(evs)-1]; last.Kind != EventDone || last.Usage != nil {
		t.Fatalf("done = %+v", last)
	}
}

// A failed turn still reports what it spent before the error.
func TestAgentErrorCarriesUsage(t *testing.T) {
	tool := Tool{Name: "read_file", Run: func(context.Context, map[string]any) (string, error) { return "x", nil }}
	p := &scripted{tools: true, replies: []Chunk{
		{ToolCalls: []ToolCall{{ID: "1", Name: "read_file"}}, Usage: &Usage{Model: "m", Input: 9, Output: 1, Requests: 1, PeakContext: 9}},
		{Err: errString("boom")},
	}}
	evs, _ := collectEvents(&Agent{Provider: p, Tools: []Tool{tool}, Native: true}, []Message{{Role: RoleUser, Content: "go"}})
	last := evs[len(evs)-1]
	if last.Kind != EventError || last.Usage == nil || last.Usage.Input != 9 {
		t.Fatalf("error = %+v usage %+v", last, last.Usage)
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestPricingLookup(t *testing.T) {
	p := NewPricing(nil)
	cases := []struct {
		model string
		in    float64
		ok    bool
	}{
		{"claude-opus-5-5", 4, true},
		{"claude-sonnet-5-5", 2, true},
		{"claude-haiku-4-5-20251001", 1, true},   // dated id: prefix match
		{"anthropic/claude-sonnet-5-5", 2, true}, // OpenRouter vendor prefix
		{"claude-opus-4-1", 15, true},
		{"claude-opus-4-5", 5, true}, // longest prefix wins over claude-opus-4
		{"gpt-4.1-mini-2025-04-14", 0.4, true},
		{"gpt-4.1", 2, true},
		{"openai/gpt-4o-mini", 0.15, true},
		{"qwen2.5-coder:7b", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		pr, ok := p.Lookup(c.model)
		if ok != c.ok || !near(pr.In, c.in) {
			t.Errorf("Lookup(%q) = %+v %v, want in=%v ok=%v", c.model, pr, ok, c.in, c.ok)
		}
	}
}

func TestPricingCost(t *testing.T) {
	p := NewPricing(map[string]Price{
		"my-model":        {In: 1, Out: 2},                 // added; cache defaults to In
		"claude-sonnet-5": {In: 10, Out: 10, CacheRead: 1}, // overrides the table
	})
	u := Usage{Model: "claude-opus-5-5", Input: 1_000_000, Output: 100_000, CacheRead: 1_000_000, CacheWrite: 1_000_000}
	// 4 + 2 + 0.20 + 5 (cache write 1.25 × in)
	if usd, ok := p.Cost(KindAnthropic, u); !ok || !near(usd, 11.2) {
		t.Errorf("opus cost = %v %v", usd, ok)
	}
	if usd, ok := p.Cost(KindCompatible, Usage{Model: "my-model", Input: 1_000_000, Output: 1_000_000, CacheRead: 1_000_000}); !ok || !near(usd, 4) {
		t.Errorf("override cost = %v %v", usd, ok)
	}
	if usd, ok := p.Cost(KindAnthropic, Usage{Model: "claude-sonnet-5-5", Input: 1_000_000}); !ok || !near(usd, 10) {
		t.Errorf("override prefix cost = %v %v", usd, ok)
	}
	if _, ok := p.Cost(KindCompatible, Usage{Model: "mystery", Input: 5}); ok {
		t.Error("unknown model must have no cost")
	}
	for _, k := range []string{KindOllama, KindLMStudio} {
		if usd, ok := p.Cost(k, Usage{Model: "qwen2.5-coder:7b", Input: 5}); !ok || usd != 0 {
			t.Errorf("%s cost = %v %v, want 0 true", k, usd, ok)
		}
	}
	if _, ok := p.Cost(KindClaudeOAuth, u); ok {
		t.Error("a subscription sign-in is not billed per token")
	}
}

func TestContextWindow(t *testing.T) {
	w := NewContextWindows(map[string]int{"qwen2.5-coder": 32768, "claude-haiku-4-5": 100})
	cases := map[string]int{
		"claude-opus-5-5":             1_000_000,
		"claude-haiku-4-5-20251001":   100, // override
		"gpt-4o-mini":                 128_000,
		"qwen2.5-coder:7b":            32768,
		"anthropic/claude-sonnet-5-5": 1_000_000,
		"mystery":                     0,
	}
	for m, want := range cases {
		if got, _ := w.Lookup(m); got != want {
			t.Errorf("window(%q) = %d, want %d", m, got, want)
		}
	}
	if pct, ok := ContextPercent(Usage{PeakContext: 50_000}, 200_000); !ok || pct != 25 {
		t.Errorf("percent = %v %v", pct, ok)
	}
	if _, ok := ContextPercent(Usage{PeakContext: 5}, 0); ok {
		t.Error("no window, no percent")
	}
}

func TestUsageAdd(t *testing.T) {
	var sum *Usage
	sum = sum.Add(nil)
	if sum != nil {
		t.Fatal("nil + nil")
	}
	sum = sum.Add(&Usage{Model: "a", Input: 1, Requests: 1, PeakContext: 5})
	sum = sum.Add(&Usage{Model: "b", Output: 2, Requests: 1, PeakContext: 3})
	if *sum != (Usage{Model: "b", Input: 1, Output: 2, Requests: 2, PeakContext: 5}) {
		t.Errorf("sum = %+v", sum)
	}
}

func TestSettingsPricesAndWindows(t *testing.T) {
	path := t.TempDir() + "/config.json"
	if err := os.WriteFile(path, []byte(`{"ai_prices":{"my-model":{"in":3,"out":15,"cache_read":0.3}},"ai_context_windows":{"my-model":64000},"ai_model":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := LoadSettings(path)
	if s.Prices["my-model"] != (Price{In: 3, Out: 15, CacheRead: 0.3}) || s.ContextWindows["my-model"] != 64000 {
		t.Fatalf("settings = %+v", s)
	}
	// SaveSettings keeps the hand-written keys.
	s.Model = "y"
	if err := SaveSettings(path, s); err != nil {
		t.Fatal(err)
	}
	s2 := LoadSettings(path)
	if s2.Model != "y" || s2.Prices["my-model"].In != 3 || s2.ContextWindows["my-model"] != 64000 {
		t.Fatalf("after save = %+v", s2)
	}
}

// A turn's total counts its side requests too (look_at's request, a
// summary): their tokens add up, but the turn's model and its context
// window use stay the main model's.
func TestAgentTurnCountsSideUsage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "shot.png"), "\x89PNG fake")
	eye := &scripted{replies: []Chunk{{Delta: "A Save button.", Usage: &Usage{Model: "eye-1", Input: 900, Output: 20, Requests: 1, PeakContext: 900}}}}
	main := &scripted{tools: true, replies: []Chunk{
		{ToolCalls: []ToolCall{{ID: "1", Name: "look_at", Args: map[string]any{"path": "shot.png"}}}, Usage: &Usage{Model: "main-1", Input: 100, Output: 5, Requests: 1, PeakContext: 100}},
		{Delta: "It is a Save button.", Usage: &Usage{Model: "main-1", Input: 120, Output: 7, Requests: 1, PeakContext: 120}},
	}}
	a := &Agent{Provider: main, Native: true, Tools: []Tool{LookAtTool(Workspace{Root: dir}, func() (Provider, string, bool) { return eye, "eye-1", true })}}
	evs, _ := collectEvents(a, []Message{{Role: RoleSystem, Content: "s"}, {Role: RoleUser, Content: "what is in shot.png?"}})
	last := evs[len(evs)-1]
	if last.Kind != EventDone || last.Usage == nil {
		t.Fatalf("last event %+v", last)
	}
	u := *last.Usage
	if u.Input != 1120 || u.Output != 32 || u.Requests != 3 || u.Model != "main-1" || u.PeakContext != 120 {
		t.Fatalf("turn usage %+v", u)
	}
}

// A side request outside a turn's sink is not counted anywhere (no
// panic); AddSideUsage before Agent.Run lands in that turn's total.
func TestSideUsageBeforeTheTurn(t *testing.T) {
	AddSideUsage(context.Background(), &Usage{Input: 5})
	ctx := WithSideUsage(context.Background())
	AddSideUsage(ctx, &Usage{Model: "quick-1", Input: 50, Output: 5, Requests: 1})
	main := &scripted{replies: []Chunk{{Delta: "hi", Usage: &Usage{Model: "main-1", Input: 10, Output: 1, Requests: 1}}}}
	var last Event
	(&Agent{Provider: main}).Run(ctx, []Message{{Role: RoleSystem, Content: "s"}, {Role: RoleUser, Content: "hi"}}, func(e Event) { last = e })
	if last.Usage == nil || last.Usage.Input != 60 || last.Usage.Requests != 2 || last.Usage.Model != "main-1" {
		t.Fatalf("turn usage %+v", last.Usage)
	}
}
