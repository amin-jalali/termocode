package ai

import (
	"context"
	"testing"
)

// Spec: tool events carry the tool's Label (its Display name, e.g.
// mcp.fake.echo) next to the wire name, for the audit log.
func TestAgentToolEventsCarryLabel(t *testing.T) {
	tool := Tool{Name: "mcp__fake__echo", Display: "mcp.fake.echo",
		Run: func(context.Context, map[string]any) (string, error) { return "ok", nil }}
	a := &Agent{Tools: []Tool{tool}}
	var got []Event
	a.runTool(context.Background(), ToolCall{ID: "1", Name: "mcp__fake__echo"}, func(e Event) { got = append(got, e) })
	if len(got) != 2 {
		t.Fatalf("events %+v", got)
	}
	for _, e := range got {
		if e.Label != "mcp.fake.echo" || e.Tool != "mcp__fake__echo" {
			t.Errorf("event %+v", e)
		}
	}
}
