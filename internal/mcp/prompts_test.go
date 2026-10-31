package mcp

import (
	"context"
	"strings"
	"testing"
)

// MCPX-13: prompts are listed, one is got with its arguments, and an
// argument value is completed (completion/complete).
func TestClientPrompts(t *testing.T) {
	p := startFake(t, nil, Options{})
	ctx := context.Background()
	ps, err := p.Client.ListPrompts(ctx)
	if err != nil || len(ps) != 2 || ps[0].Name != "review" || len(ps[0].Arguments) != 2 || !ps[0].Arguments[0].Required {
		t.Fatalf("prompts %+v %v", ps, err)
	}
	got, err := p.Client.GetPrompt(ctx, "review", map[string]string{"file": "main.go"})
	if err != nil || len(got.Messages) != 2 || got.Messages[0].Role != "user" || got.Messages[0].Content.Text != "Review main.go (style: short)." {
		t.Fatalf("get %+v %v", got, err)
	}
	if _, err := p.Client.GetPrompt(ctx, "review", nil); err == nil || !strings.Contains(err.Error(), "file") {
		t.Fatalf("missing argument: %v", err)
	}
	vals, err := p.Client.CompletePrompt(ctx, "review", "file", "ma", nil)
	if err != nil || strings.Join(vals, ",") != "main.go,main_test.go" {
		t.Fatalf("complete %v %v", vals, err)
	}
}

// The manager gets prompts through the server's connection.
func TestManagerPrompts(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}})
	m := e.manager(t, 0, nil)
	ctx := context.Background()
	ps, err := m.Prompts(ctx, nil, "fake")
	if err != nil || len(ps) != 2 {
		t.Fatalf("prompts %+v %v", ps, err)
	}
	if c, ok := m.Caps("fake"); !ok || !c.Prompts {
		t.Fatalf("caps %+v %v", c, ok)
	}
	got, err := m.GetPrompt(ctx, nil, "fake", "hello", nil)
	if err != nil || got.Messages[0].Content.Text != "Hello!" {
		t.Fatalf("get %+v %v", got, err)
	}
	vals, err := m.CompletePrompt(ctx, nil, "fake", "review", "style", "", nil)
	if err != nil || len(vals) != 2 {
		t.Fatalf("complete %v %v", vals, err)
	}
}
