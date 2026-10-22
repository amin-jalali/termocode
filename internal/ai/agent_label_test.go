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

// Spec (fix 6): the agent calls a tool's Release before the call and after
// it, also when the user declines.
func TestRunToolReleasesArgs(t *testing.T) {
	var log []string
	a := &Agent{
		Tools: []Tool{{
			Name: "x", Mutating: true, Class: "x",
			Release: func(map[string]any) { log = append(log, "release") },
			Run:     func(context.Context, map[string]any) (string, error) { log = append(log, "run"); return "", nil },
		}},
		Confirm: func(context.Context, Tool, string) bool { return false },
	}
	a.runTool(context.Background(), ToolCall{ID: "1", Name: "x"}, func(Event) {})
	if len(log) != 2 || log[0] != "release" || log[1] != "release" {
		t.Fatalf("log = %v", log)
	}
}
