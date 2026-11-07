package ai

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (CMP-59): PreTool runs before the gate; a block never asks and
// never runs, and the model is told why; changed arguments are what
// runs; Allow skips the approval card.
func TestAgentPreTool(t *testing.T) {
	dir := t.TempDir()
	call := ToolCall{ID: "c1", Name: "write_file", Args: map[string]any{"path": "x.txt", "content": "hi"}}
	mk := func() *scripted {
		return &scripted{tools: true, replies: []Chunk{{ToolCalls: []ToolCall{call}}, {Delta: "ok"}}}
	}
	never := func(context.Context, Tool, string) bool { t.Fatal("the card must not open"); return false }
	a := &Agent{Provider: mk(), Native: true, Tools: WorkspaceTools(Workspace{Root: dir}), Confirm: never,
		PreTool: func(_ context.Context, tl Tool, args map[string]any) PreToolVerdict {
			if tl.Name != "write_file" || args["path"] != "x.txt" {
				t.Errorf("PreTool saw %s %v", tl.Name, args)
			}
			return PreToolVerdict{Block: "no writes today."}
		}}
	evs, added := collectEvents(a, []Message{{Role: RoleUser, Content: "write"}})
	if _, err := os.Stat(filepath.Join(dir, "x.txt")); err == nil {
		t.Fatal("a blocked call ran")
	}
	if !strings.Contains(added[1].Content, "blocked by a hook: no writes today.") {
		t.Fatalf("model not told: %+v", added[1])
	}
	found := false
	for _, e := range evs {
		if e.Kind == EventToolResult && e.Gate == GateHook && e.Decision == "hook denied" {
			found = true
		}
	}
	if !found {
		t.Errorf("no hook-denied event: %+v", evs)
	}

	a = &Agent{Provider: mk(), Native: true, Tools: WorkspaceTools(Workspace{Root: dir}), Confirm: never,
		PreTool: func(context.Context, Tool, map[string]any) PreToolVerdict {
			return PreToolVerdict{Allow: true, Args: map[string]any{"path": "y.txt", "content": "changed"}}
		}}
	evs, _ = collectEvents(a, []Message{{Role: RoleUser, Content: "write"}})
	if b, _ := os.ReadFile(filepath.Join(dir, "y.txt")); string(b) != "changed" {
		t.Fatalf("changed args did not run: %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "x.txt")); err == nil {
		t.Fatal("the old args ran")
	}
	allowed := false
	for _, e := range evs {
		if e.Kind == EventToolResult && e.Decision == "allowed by hook" {
			allowed = true
		}
	}
	if !allowed {
		t.Errorf("no allowed-by-hook decision: %+v", evs)
	}

	asked := 0
	a = &Agent{Provider: mk(), Native: true, Tools: WorkspaceTools(Workspace{Root: dir}),
		Confirm: func(context.Context, Tool, string) bool { asked++; return false },
		PreTool: func(context.Context, Tool, map[string]any) PreToolVerdict { return PreToolVerdict{} }}
	collectEvents(a, []Message{{Role: RoleUser, Content: "write"}})
	if asked != 1 {
		t.Errorf("no verdict must keep the card: asked %d", asked)
	}
}
