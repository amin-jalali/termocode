package ai

import (
	"context"
	"sync"
	"testing"
)

// Spec (MCPX-38): a running tool can show a note to the user that the
// model does not get (an MCP item for the user only); the agent emits it
// as EventToolNote, redacted. Notes after the call ended are dropped.
func TestAgentToolNote(t *testing.T) {
	var late func()
	tool := Tool{Name: "n", Display: "my.n", Run: func(ctx context.Context, _ map[string]any) (string, error) {
		ShowUser(ctx, "look at the dashboard")
		late = func() { ShowUser(ctx, "too late") }
		return "done", nil
	}}
	p := &scripted{tools: true, replies: []Chunk{{ToolCalls: []ToolCall{{ID: "c1", Name: "n"}}}, {Delta: "ok"}}}
	a := &Agent{Provider: p, Native: true, Tools: []Tool{tool}}
	var mu sync.Mutex
	var got []string
	a.Run(context.Background(), []Message{{Role: RoleUser, Content: "go"}}, func(e Event) {
		if e.Kind == EventToolNote {
			mu.Lock()
			got = append(got, e.Label+": "+e.Detail)
			mu.Unlock()
		}
	})
	late()
	if len(got) != 1 || got[0] != "my.n: look at the dashboard" {
		t.Fatalf("notes %v", got)
	}
	ShowUser(context.Background(), "nobody listens") // no panic
}
