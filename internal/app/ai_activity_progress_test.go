package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// MCPX-19: the activity line of a running tool shows its last progress
// text; a new tool starts without it.
func TestAIActivityToolProgress(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	var a aiActivity
	a.begin(now)
	a.onEvent(ai.Event{Kind: ai.EventToolStart, Label: "mcp.fake.long"}, now)
	a.onEvent(ai.Event{Kind: ai.EventToolProgress, Label: "mcp.fake.long", Detail: "page 1 · 1 of 4 (25%)"}, now.Add(time.Second))
	line := func() string {
		var out []string
		for _, l := range a.view(now.Add(3*time.Second)).lines("", 120) {
			out = append(out, l.text)
		}
		return strings.Join(out, "\n")
	}
	if got := line(); !strings.Contains(got, "mcp.fake.long · 3 s · page 1 · 1 of 4 (25%)") {
		t.Fatalf("line %q", got)
	}
	a.onEvent(ai.Event{Kind: ai.EventToolStart, Label: "mcp.fake.next"}, now.Add(2*time.Second))
	if got := line(); strings.Contains(got, "page 1") {
		t.Fatalf("old progress kept: %q", got)
	}
}
