package mcp

import (
	"context"
	"strings"
	"testing"
	"time"
)

// MCPX-19 (RT-21): a running MCP call can move to the background: the
// tool answers the model at once, the call goes on, and its result
// arrives on the channel Detach gave.
func TestAIToolsBackground(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	call := func(ctx context.Context, _, _ string, _ map[string]any) (CallResult, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return CallResult{}, ctx.Err()
		}
		return CallResult{Content: []Content{{Type: "text", Text: "built it"}}}, nil
	}
	tool := AITools(Server{Name: "ci"}, []Tool{{Name: "build", Annotations: ann(bp(true), nil)}}, call, AdapterOptions{TrustAnnotations: true})[0]
	bg := NewBackground()
	turn, stop := context.WithCancel(WithBackground(context.Background(), bg))
	out := make(chan string, 1)
	go func() { s, _ := tool.Run(turn, nil); out <- s }()
	<-started
	if got := bg.Running(); len(got) != 1 || got[0] != "mcp.ci.build" {
		t.Fatalf("running %v", got)
	}
	results := bg.Detach()
	s := <-out
	if len(results) != 1 || !strings.Contains(s, "still running in the background") || len(bg.Running()) != 0 {
		t.Fatalf("detach: %q %d", s, len(results))
	}
	stop() // the turn ends: the background call goes on
	close(release)
	select {
	case r := <-results[0]:
		if r.Label != "mcp.ci.build" || !strings.Contains(r.Text, "built it") || r.Err != nil {
			t.Fatalf("result %+v", r)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no background result")
	}
}

// Without Detach a stopped turn still cancels the call.
func TestAIToolsBackgroundCancel(t *testing.T) {
	call := func(ctx context.Context, _, _ string, _ map[string]any) (CallResult, error) {
		<-ctx.Done()
		return CallResult{}, ctx.Err()
	}
	tool := AITools(Server{Name: "ci"}, []Tool{{Name: "build", Annotations: ann(bp(true), nil)}}, call, AdapterOptions{TrustAnnotations: true})[0]
	turn, stop := context.WithCancel(WithBackground(context.Background(), NewBackground()))
	stop()
	if _, err := tool.Run(turn, nil); err == nil {
		t.Fatal("a stopped turn's call did not end")
	}
}
