package ai

import (
	"context"
	"strings"
	"testing"
)

// toolNamesProvider replays replies and records the tool names of each
// request.
type toolNamesProvider struct {
	scripted
	names [][]string
}

func (p *toolNamesProvider) Stream(ctx context.Context, req Request) <-chan Chunk {
	var n []string
	for _, s := range req.Tools {
		n = append(n, s.Name)
	}
	p.names = append(p.names, n)
	return p.scripted.Stream(ctx, req)
}

// A Hidden tool is left out of the specs until Hidden says false; it can
// still be called by name. The specs are built again for each step.
func TestAgentHiddenToolJoinsSpecsAfterLoad(t *testing.T) {
	loaded := false
	hidden := Tool{Name: "mcp__x__big", Hidden: func() bool { return !loaded },
		Run: func(context.Context, map[string]any) (string, error) { return "big ran", nil }}
	search := Tool{Name: "mcp_tool_search",
		Run: func(context.Context, map[string]any) (string, error) { loaded = true; return "found", nil }}
	p := &toolNamesProvider{scripted: scripted{tools: true, replies: []Chunk{
		{ToolCalls: []ToolCall{{ID: "1", Name: "mcp_tool_search"}}},
		{ToolCalls: []ToolCall{{ID: "2", Name: "mcp__x__big"}}},
		{Delta: "ok"},
	}}}
	a := &Agent{Provider: p, Native: true, Tools: []Tool{search, hidden}}
	_, added := collectEvents(a, []Message{{Role: "user", Content: "go"}})
	if len(p.names) < 2 {
		t.Fatalf("requests = %d", len(p.names))
	}
	if strings.Join(p.names[0], ",") != "mcp_tool_search" {
		t.Fatalf("first specs = %v, want only the search tool", p.names[0])
	}
	if strings.Join(p.names[1], ",") != "mcp_tool_search,mcp__x__big" {
		t.Fatalf("second specs = %v, want the loaded tool too", p.names[1])
	}
	var ran bool
	for _, m := range added {
		if strings.Contains(m.Content, "big ran") {
			ran = true
		}
	}
	if !ran {
		t.Fatal("the hidden tool did not run")
	}
}

// The text protocol's catalog leaves Hidden tools out.
func TestSystemPromptSkipsHiddenTools(t *testing.T) {
	tools := []Tool{{Name: "shown_tool"}, {Name: "hidden_tool", Hidden: func() bool { return true }}}
	p := SystemPrompt("base", tools, false)
	if !strings.Contains(p, "shown_tool") || strings.Contains(p, "hidden_tool") {
		t.Fatalf("prompt:\n%s", p)
	}
}
