package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
)

// Spec (U20 AP-1): a chat turn writes one usage line to the AI audit
// channel: model, tokens, cost and context use.
func TestAIAuditUsageLine(t *testing.T) {
	t.Cleanup(clock.Set(func() time.Time { return time.Date(2026, 10, 18, 9, 5, 0, 0, time.Local) }))
	p := &scriptedAIProvider{replies: []ai.Chunk{
		{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "list_dir", Args: map[string]any{"path": "."}}},
			Usage: &ai.Usage{Model: "claude-sonnet-5-5", Input: 3000, Output: 300, CacheRead: 1000, Requests: 1, PeakContext: 4000}},
		{Delta: "done", Usage: &ai.Usage{Model: "claude-sonnet-5-5", Input: 1103, Output: 512, Requests: 1, PeakContext: 5103}},
	}}
	m, _ := aiIDETestModel(t, p)
	m.ai.cfg = ai.Config{Kind: ai.KindAnthropic, Model: "claude-sonnet-5-5"}
	m.output = newOutputStore()
	driveAIChat(t, m, m.sendAIChat("hi"), nil)
	var got string
	for _, l := range m.output.Lines(aiAuditChannel) {
		if strings.Contains(l, "  usage  ") {
			got = l
		}
	}
	// 4103 in × $2 + 812 out × $10 + 1000 cached × $0.20 per Mtok =
	// $0.016526; the biggest prompt (5103) is 0.5 % of the 1M window.
	want := "09:05:00  usage  claude-sonnet-5-5 · 2 requests · 4 103 in · 812 out · 1 000 cached · $0.017 · context 1%"
	if got != want {
		t.Fatalf("usage line\n got %q\nwant %q\nall: %q", got, want, m.output.Lines(aiAuditChannel))
	}
}

func TestAIUsageText(t *testing.T) {
	prices := ai.NewPricing(map[string]ai.Price{"house-model": {In: 1, Out: 1}})
	windows := ai.NewContextWindows(nil)
	cases := []struct {
		name string
		kind string
		u    ai.Usage
		want string
	}{
		{"unknown model: no cost, no window", ai.KindCompatible,
			ai.Usage{Model: "mystery", Input: 12, Output: 3, Requests: 1, PeakContext: 12},
			"mystery · 12 in · 3 out"},
		{"local is free", ai.KindOllama,
			ai.Usage{Model: "qwen2.5-coder:7b", Input: 1234567, Output: 1, Requests: 1},
			"qwen2.5-coder:7b · 1 234 567 in · 1 out · $0"},
		{"override price, cache write", ai.KindCompatible,
			ai.Usage{Model: "house-model", Input: 2_000_000, Output: 3_000_000, CacheWrite: 10, Requests: 1},
			"house-model · 2 000 000 in · 3 000 000 out · 10 cache write · $5.00"},
		{"no model name", ai.KindOpenAI,
			ai.Usage{Input: 1, Output: 1, Requests: 1},
			"? · 1 in · 1 out"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := aiUsageText(c.kind, c.u, prices, windows); got != c.want {
				t.Errorf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}
