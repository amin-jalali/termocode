package ai

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Spec (OMX-24): the read-only calls of one reply run at once; results
// and events keep the call order; a mutating call runs alone, in order.
func TestAgentRunsReadOnlyCallsInParallel(t *testing.T) {
	var started sync.WaitGroup
	started.Add(2)
	both := make(chan struct{})
	go func() { started.Wait(); close(both) }()
	var mu sync.Mutex
	var order []string
	reader := func(name string) Tool {
		return Tool{Name: name, ReadOnly: true, Run: func(ctx context.Context, _ map[string]any) (string, error) {
			started.Done()
			select {
			case <-both:
				mu.Lock()
				order = append(order, name)
				mu.Unlock()
				return name + " ok", nil
			case <-time.After(5 * time.Second):
				return name + " ran alone", nil
			}
		}}
	}
	writer := Tool{Name: "w", Mutating: true, Class: ClassFileWrite, Run: func(context.Context, map[string]any) (string, error) {
		mu.Lock()
		order = append(order, "w")
		mu.Unlock()
		return "w ok", nil
	}}
	p := &scripted{tools: true, replies: []Chunk{{ToolCalls: []ToolCall{
		{ID: "1", Name: "r1"}, {ID: "2", Name: "r2"}, {ID: "3", Name: "w"},
	}}}}
	a := &Agent{Provider: p, Native: true, Tools: []Tool{reader("r1"), reader("r2"), writer},
		AutoApprove: map[string]bool{ClassFileWrite: true}}
	evs, added := collectEvents(a, []Message{{Role: RoleUser, Content: "go"}})
	var results []string
	for _, m := range added {
		if m.Role == RoleTool {
			results = append(results, m.ToolCallID+"="+m.Content)
		}
	}
	if len(results) != 3 || results[0] != "1=r1 ok" || results[1] != "2=r2 ok" || results[2] != "3=w ok" {
		t.Fatalf("results %v", results)
	}
	if order[2] != "w" {
		t.Errorf("the write did not wait for the reads: %v", order)
	}
	var seq []string
	for _, e := range evs {
		switch e.Kind {
		case EventToolStart:
			seq = append(seq, "start "+e.Tool)
		case EventToolResult:
			seq = append(seq, "end "+e.Tool)
		}
	}
	want := []string{"start r1", "end r1", "start r2", "end r2", "start w", "end w"}
	if len(seq) != len(want) {
		t.Fatalf("events %v", seq)
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Fatalf("events %v, want %v", seq, want)
		}
	}
}

func TestWorkspaceReadToolsAreReadOnly(t *testing.T) {
	ro := map[string]bool{}
	for _, tool := range WorkspaceTools(Workspace{Root: t.TempDir()}) {
		if tool.ReadOnly {
			ro[tool.Name] = true
			if tool.Mutating || tool.Gate != nil {
				t.Errorf("%s is read-only and gated", tool.Name)
			}
		}
	}
	for _, n := range []string{"read_file", "list_dir", "search", "glob", "git_status", "git_diff", "git_branch"} {
		if !ro[n] {
			t.Errorf("%s is not marked read-only", n)
		}
	}
}
