package ai

import (
	"context"
	"strings"
	"testing"
)

// OMX-25: tool results are cut to what still fits the context window.
func TestFitToolResults(t *testing.T) {
	big := strings.Repeat("a", 50000) + "TAIL"
	small := "short result"
	// Plenty of room: nothing changes.
	got := fitToolResults([]string{big, small}, 1_000_000, 10_000)
	if got[0] != big || got[1] != small {
		t.Fatal("cut although the window has room")
	}
	// Unknown window: nothing changes.
	if got := fitToolResults([]string{big}, 0, 10_000); got[0] != big {
		t.Fatal("cut without a window")
	}
	// A nearly full window: the big result is cut, head and tail kept,
	// with a note; the small one stays.
	got = fitToolResults([]string{big, small}, 40_000, 20_000)
	if len(got[0]) >= len(big) || !strings.HasPrefix(got[0], "aaaa") || !strings.HasSuffix(got[0], "TAIL") || !strings.Contains(got[0], "cut to fit the context window") {
		t.Fatalf("not cut well: %d chars, %q…%q", len(got[0]), got[0][:20], got[0][len(got[0])-60:])
	}
	if got[1] != small {
		t.Error("a small result was cut")
	}
	// A full window still keeps a useful part.
	got = fitToolResults([]string{big}, 40_000, 40_000)
	if len(got[0]) < minFitChars {
		t.Errorf("kept only %d chars", len(got[0]))
	}
}

// The agent cuts a big tool result when the last request was near the
// model's window.
func TestAgentFitsToolResults(t *testing.T) {
	big := strings.Repeat("x", 200000)
	tool := Tool{Name: "dump", Run: func(context.Context, map[string]any) (string, error) { return big, nil }}
	p := &recorder{replies: []Chunk{
		{ToolCalls: []ToolCall{{ID: "1", Name: "dump"}}, Usage: &Usage{Input: 90_000, PeakContext: 90_000, Requests: 1}},
		{Delta: "ok"},
	}}
	a := &Agent{Provider: p, Native: true, Tools: []Tool{tool}, ContextWindow: 100_000}
	collectEvents(a, []Message{{Role: RoleUser, Content: "q"}})
	rs := p.requests()
	last := rs[len(rs)-1].Messages
	res := last[len(last)-1]
	if res.Role != RoleTool || len(res.Content) >= len(big) {
		t.Fatalf("result not cut: %d chars", len(res.Content))
	}
}
