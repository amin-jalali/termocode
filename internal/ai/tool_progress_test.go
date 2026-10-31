package ai

import (
	"context"
	"sync"
	"testing"
)

// A running tool reports progress text; the agent passes it on as
// EventToolProgress for that tool. Reports after the call ended are
// dropped.
func TestAgentToolProgress(t *testing.T) {
	var late func()
	tool := Tool{Name: "long", Display: "my.long", Run: func(ctx context.Context, _ map[string]any) (string, error) {
		ReportProgress(ctx, "step 1 of 2")
		ReportProgress(ctx, "step 2 of 2")
		late = func() { ReportProgress(ctx, "too late") }
		return "done", nil
	}}
	p := &scripted{tools: true, replies: []Chunk{{ToolCalls: []ToolCall{{ID: "c1", Name: "long"}}}, {Delta: "ok"}}}
	a := &Agent{Provider: p, Native: true, Tools: []Tool{tool}}
	var mu sync.Mutex
	var got []string
	a.Run(context.Background(), []Message{{Role: RoleUser, Content: "go"}}, func(e Event) {
		if e.Kind == EventToolProgress {
			mu.Lock()
			got = append(got, e.Label+": "+e.Detail)
			mu.Unlock()
		}
	})
	late()
	if len(got) != 2 || got[0] != "my.long: step 1 of 2" || got[1] != "my.long: step 2 of 2" {
		t.Fatalf("progress %v", got)
	}
	ReportProgress(context.Background(), "nobody listens") // no panic
}
