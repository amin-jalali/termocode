package app

import (
	"context"
	"errors"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/source"
)

var (
	_ source.ToolCaller = (*wfMCP)(nil)
	_ run.ToolCheck     = (*wfMCP)(nil)
	_ source.DBOpener   = wfDB{}
)

func wfTestMCP(calls *int) *wfMCP {
	yes := true
	tools := []mcp.Tool{
		{Name: "list_issues", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: &yes}},
		{Name: "create_comment"},
	}
	return &wfMCP{
		trust: true,
		known: func(name string) (mcp.Server, []mcp.Tool, bool) {
			if name != "linear" {
				return mcp.Server{}, nil, false
			}
			return mcp.Server{Name: "linear"}, tools, true
		},
		call: func(_ context.Context, server, tool string, _ map[string]any) (mcp.CallResult, error) {
			*calls++
			return mcp.CallResult{StructuredContent: []byte(`{"issues":[]}`)}, nil
		},
	}
}

// Spec (WFX wiring, plan WFX-4): the app's tool caller looks at the
// tool's annotation BEFORE it calls: a read-only tool runs; a tool that
// is not read-only never runs unless the step was confirmed; a server
// that is not known is "not connected".
func TestWorkflowMCPChecksReadOnlyBeforeTheCall(t *testing.T) {
	calls := 0
	c := wfTestMCP(&calls)
	ctx := context.Background()
	if ro, err := c.ReadOnly(ctx, "linear", "list_issues"); err != nil || !ro {
		t.Fatalf("list_issues: %v %v", ro, err)
	}
	if ro, _ := c.ReadOnly(ctx, "linear", "create_comment"); ro {
		t.Fatal("create_comment is not read-only")
	}
	if _, ro, err := c.CallTool(ctx, "linear", "list_issues", nil); err != nil || !ro || calls != 1 {
		t.Fatalf("read call: ro %v err %v calls %d", ro, err, calls)
	}
	raw, ro, err := c.CallTool(ctx, "linear", "create_comment", nil)
	if err != nil || ro || raw != nil || calls != 1 {
		t.Fatalf("a not read-only tool without a yes must not run: raw %s ro %v err %v calls %d", raw, ro, err, calls)
	}
	if _, _, err := c.CallTool(source.WithWriteAllowed(ctx), "linear", "create_comment", nil); err != nil || calls != 2 {
		t.Fatalf("confirmed: err %v calls %d", err, calls)
	}
	if _, err := c.ReadOnly(ctx, "nope", "x"); !errors.Is(err, source.ErrNotConnected) {
		t.Fatalf("unknown server: %v", err)
	}
	if _, _, err := c.CallTool(ctx, "linear", "missing", nil); err == nil || calls != 2 {
		t.Fatalf("unknown tool: %v", err)
	}
}

// Spec: the db opener refuses a connection name the workspace does not have.
func TestWorkflowDBUnknownConnection(t *testing.T) {
	if _, _, err := (wfDB{root: t.TempDir()}).OpenReadOnly(context.Background(), "nope"); err == nil {
		t.Fatal("want an error")
	}
}
