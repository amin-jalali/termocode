package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fakeTools is the fake MCP caller. It keeps the contract: a tool that
// is not read-only runs only when WriteAllowed(ctx).
type fakeTools struct {
	tools map[string]fakeTool
	ran   []string
	// cheat breaks the contract: it runs every tool.
	cheat bool
}

type fakeTool struct {
	readOnly bool
	answer   string
}

func (f *fakeTools) CallTool(ctx context.Context, server, tool string, args map[string]any) (json.RawMessage, bool, error) {
	if server == "down" {
		return nil, false, fmt.Errorf("%s: %w", server, ErrNotConnected)
	}
	t, ok := f.tools[tool]
	if !ok {
		return nil, false, errors.New("no such tool")
	}
	if !t.readOnly && !WriteAllowed(ctx) && !f.cheat {
		return nil, false, nil
	}
	f.ran = append(f.ran, tool)
	return json.RawMessage(t.answer), t.readOnly, nil
}

func newFakeTools() *fakeTools {
	return &fakeTools{tools: map[string]fakeTool{
		"list_issues": {readOnly: true, answer: `{"content":[{"type":"text","text":"{\"issues\":[{\"id\":\"LIN-1\"},{\"id\":\"LIN-2\"}]}"}]}`},
		"structured":  {readOnly: true, answer: `{"content":[{"type":"text","text":"ignored"}],"structuredContent":{"items":[{"id":7}]}}`},
		"plain":       {readOnly: true, answer: `[{"id":"a"}]`},
		"broken":      {readOnly: true, answer: `{"content":[{"type":"text","text":"rate limited"}],"isError":true}`},
		"text":        {readOnly: true, answer: `{"content":[{"type":"text","text":"hello there"}]}`},
		"close_issue": {readOnly: false, answer: `{"content":[{"type":"text","text":"{\"done\":true}"}]}`},
	}}
}

func TestMCPReadOnlyTool(t *testing.T) {
	f := newFakeTools()
	s := &Sources{Tools: f}
	ctx := context.Background()
	r, err := s.MCP(ctx, MCPInput{Server: "linear", Tool: "list_issues", Args: map[string]any{"state": "Todo"}, Shape: Shape{Items: "$.issues", Key: "$.id"}})
	if err != nil || keys(r) != "LIN-1 LIN-2" {
		t.Fatalf("text JSON: %q %v", keys(r), err)
	}
	r, err = s.MCP(ctx, MCPInput{Server: "linear", Tool: "structured", Shape: Shape{Items: "$.items", Key: "$.id"}})
	if err != nil || keys(r) != "7" {
		t.Fatalf("structured: %q %v", keys(r), err)
	}
	r, err = s.MCP(ctx, MCPInput{Server: "linear", Tool: "plain", Shape: Shape{Key: "$.id"}})
	if err != nil || keys(r) != "a" {
		t.Fatalf("plain: %q %v", keys(r), err)
	}
	if _, err := s.MCP(ctx, MCPInput{Server: "linear", Tool: "broken"}); !errors.Is(err, ErrToolFailed) || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("isError: %v", err)
	}
	if _, err := s.MCP(ctx, MCPInput{Server: "linear", Tool: "text"}); err == nil {
		t.Error("text answer: want an error")
	}
}

func TestMCPWriteToolNeedsConfirm(t *testing.T) {
	f := newFakeTools()
	s := &Sources{Tools: f}
	ctx := context.Background()
	_, err := s.MCP(ctx, MCPInput{Server: "linear", Tool: "close_issue"})
	if !errors.Is(err, ErrNeedsConfirm) {
		t.Fatalf("want ErrNeedsConfirm, got %v", err)
	}
	if len(f.ran) != 0 {
		t.Fatalf("the tool ran: %v", f.ran)
	}
	r, err := s.MCP(ctx, MCPInput{Server: "linear", Tool: "close_issue", Confirmed: true})
	if err != nil || len(r.Items) != 1 || len(f.ran) != 1 {
		t.Fatalf("confirmed: %+v %v %v", r, err, f.ran)
	}
	// A caller that runs it anyway: the answer is still not used.
	f.cheat = true
	r, err = s.MCP(ctx, MCPInput{Server: "linear", Tool: "close_issue"})
	if !errors.Is(err, ErrNeedsConfirm) || len(r.Items) != 0 {
		t.Fatalf("cheating caller: %+v %v", r, err)
	}
}

func TestMCPErrors(t *testing.T) {
	s := &Sources{Tools: newFakeTools()}
	ctx := context.Background()
	if _, err := s.MCP(ctx, MCPInput{Server: "down", Tool: "list_issues"}); !errors.Is(err, ErrNotConnected) {
		t.Errorf("down: %v", err)
	}
	if _, err := (&Sources{}).MCP(ctx, MCPInput{Server: "linear", Tool: "list_issues"}); !errors.Is(err, ErrNotConnected) {
		t.Errorf("no caller: %v", err)
	}
	for name, in := range map[string]MCPInput{
		"bad server":     {Server: "a b", Tool: "x"},
		"bad tool":       {Server: "linear", Tool: ""},
		"secret in args": {Server: "linear", Tool: "list_issues", Args: map[string]any{"q": map[string]any{"k": []any{"${secret:X}"}}}},
		"cmd in args":    {Server: "linear", Tool: "list_issues", Args: map[string]any{"q": "${cmd:id}"}},
		"bad cap":        {Server: "linear", Tool: "list_issues", Shape: Shape{MaxItems: -1}},
		"unknown":        {Server: "linear", Tool: "nope"},
	} {
		if _, err := s.MCP(ctx, in); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
