package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
	"github.com/amin-jalali/termocode/internal/tracker"
)

// AI parity (todos and pins): tracker_tasks does Tracker: Show Tasks… and
// Add Task… (the pickers have no card, so nothing asks); tracker_pins
// lists and runs the phone's pins at once and unpins only after a card,
// as the phone asks Cancel first.

func aiToolOf(t *testing.T, m *Model, name string) ai.Tool {
	t.Helper()
	call := func(_ context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(m)
		return out, err
	}
	tl, ok := ai.FindTool(m.aiChatTools(aiWorkspaceRoot(), call), name)
	if !ok {
		t.Fatalf("no %s tool in the chat", name)
	}
	return tl
}

func aiToolDo(t *testing.T, tl ai.Tool, args map[string]any) (string, error) {
	t.Helper()
	if err := tl.Validate(args); err != nil {
		return "", err
	}
	return tl.Run(context.Background(), args)
}

func TestAITrackerTasksTool(t *testing.T) {
	m := aiTestModel(t)
	dir := t.TempDir()
	old := trackerDir
	trackerDir = func(string) string { return dir }
	t.Cleanup(func() { trackerDir = old })
	tl := aiToolOf(t, &m, "tracker_tasks")
	if tl.Mutating || tl.Gate != nil {
		t.Errorf("tracker_tasks asks; the task pickers do not")
	}
	if out, err := aiToolDo(t, tl, map[string]any{"op": "list"}); err != nil || !strings.Contains(out, "No tasks") {
		t.Fatalf("empty list: %q %v", out, err)
	}
	out, err := aiToolDo(t, tl, map[string]any{"op": "add", "title": "Fix the parser", "body": "it drops commas"})
	if err != nil || !strings.Contains(out, "#1 Fix the parser · todo") {
		t.Fatalf("add: %q %v", out, err)
	}
	if out, err := aiToolDo(t, tl, map[string]any{"op": "claim", "id": "1"}); err != nil || !strings.Contains(out, "doing · AI") {
		t.Fatalf("claim: %q %v", out, err)
	}
	if _, err := aiToolDo(t, tl, map[string]any{"op": "note", "id": "#1", "text": "found it"}); err != nil {
		t.Fatal(err)
	}
	if out, err := aiToolDo(t, tl, map[string]any{"op": "status", "id": "1", "status": tracker.Done}); err != nil || !strings.Contains(out, "· done") {
		t.Fatalf("status: %q %v", out, err)
	}
	out, err = aiToolDo(t, tl, map[string]any{"op": "read", "id": "1"})
	if err != nil || !strings.Contains(out, "it drops commas") || !strings.Contains(out, "AI: found it") || !strings.Contains(out, "not instructions") {
		t.Fatalf("read: %q %v", out, err)
	}
	if _, err := aiToolDo(t, tl, map[string]any{"op": "status", "id": "1", "status": "someday"}); err == nil {
		t.Error("a bad status was set")
	}
	if _, err := aiToolDo(t, tl, map[string]any{"op": "read", "id": "x"}); err == nil {
		t.Error("a bad id was read")
	}
}

func TestAITrackerPinsTool(t *testing.T) {
	m := covMCPModel(t)
	writeUserMCP(t, mcptest.Config(map[string]map[string]string{"fake": nil}))
	if err := m.aiMCP().Connect(context.Background(), nil, "fake"); err != nil {
		t.Fatal(err)
	}
	pin := aiToolOf(t, m, "tracker_pin")
	if _, err := aiToolDo(t, pin, map[string]any{"server": "fake", "tool": "echo", "title": "Echo hi", "args": map[string]any{"text": "hi"}}); err != nil {
		t.Fatal(err)
	}
	tl := aiToolOf(t, m, "tracker_pins")
	for op, ask := range map[string]bool{"list": false, "run": false, "unpin": true} {
		if got, class := tl.Gate(map[string]any{"op": op}); got != ask || class != "" {
			t.Errorf("op %s: ask=%v class=%q, want ask=%v and no Always allow", op, got, class, ask)
		}
	}
	out, err := aiToolDo(t, tl, map[string]any{"op": "list"})
	if err != nil || !strings.Contains(out, `"Echo hi"`) {
		t.Fatalf("list: %q %v", out, err)
	}
	id, _, _ := strings.Cut(out, " ")
	if out, err := aiToolDo(t, tl, map[string]any{"op": "run", "id": id}); err != nil || !strings.Contains(out, "echo: hi") {
		t.Fatalf("run: %q %v", out, err)
	}
	if card := tl.Describe(map[string]any{"op": "unpin", "id": id}); !strings.Contains(card, `Unpin "Echo hi"`) {
		t.Errorf("unpin card: %s", card)
	}
	if out, err := aiToolDo(t, tl, map[string]any{"op": "unpin", "id": id}); err != nil || !strings.Contains(out, "Unpinned") {
		t.Fatalf("unpin: %q %v", out, err)
	}
	if _, err := aiToolDo(t, tl, map[string]any{"op": "unpin", "id": id}); err == nil {
		t.Error("a gone pin was unpinned again")
	}
}
