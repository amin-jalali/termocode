package app

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// MCPX-12: a chat turn gets the resource tools when a server has
// resources; mcp_read_resource reads through the manager.
func TestAIMCPTurnAddsResourceTools(t *testing.T) {
	m := covMCPModel(t)
	writeUserMCP(t, mcptest.Config(map[string]map[string]string{"fake": nil}))
	ch := make(chan tea.Msg, 8)
	tools := m.aiMCPTurn(1, ch)(context.Background())
	read, ok := ai.FindTool(tools, "mcp_read_resource")
	if _, ok2 := ai.FindTool(tools, "mcp_list_resources"); !ok || !ok2 {
		t.Fatalf("no resource tools in %d tools", len(tools))
	}
	out, err := read.Run(context.Background(), map[string]any{"server": "fake", "uri": "file:///notes/a.txt"})
	if err != nil || !strings.Contains(out, "note a body") {
		t.Fatalf("read: %q %v", out, err)
	}
}

// "@server:uri" in a message attaches that resource for the model; a
// word that names no server with resources is left alone.
func TestAIMCPResourceMentions(t *testing.T) {
	m := covMCPModel(t)
	writeUserMCP(t, mcptest.Config(map[string]map[string]string{"fake": nil}))
	got := aiMCPMentionWords("see @fake:file:///notes/a.txt, and @fake:notes://7 @nobody:x://y mail@fake:x://z")
	if len(got) != 3 || got[0].server != "fake" || got[0].uri != "file:///notes/a.txt" || got[1].uri != "notes://7" || got[2].server != "nobody" {
		t.Fatalf("mentions %+v", got)
	}
	ch := make(chan tea.Msg, 8)
	resolve := m.aiMCPMentions(1, ch, "see @fake:file:///notes/a.txt and @nobody:x://y", false)
	if resolve == nil {
		t.Fatal("no resolver for a message with a resource mention")
	}
	var evs []ai.Event
	text, imgs := resolve(context.Background(), func(e ai.Event) { evs = append(evs, e) })
	if !strings.Contains(text, "Attached MCP resource fake:file:///notes/a.txt:\n```\nnote a body\n```") || len(imgs) != 0 {
		t.Fatalf("context %q", text)
	}
	// The chat shows the read as a tool line (start + result).
	if len(evs) != 2 || evs[0].Kind != ai.EventToolStart || evs[0].Label != "mcp.resources.read" || evs[1].Kind != ai.EventToolResult || strings.Contains(text, "nobody") {
		t.Fatalf("events %+v / %q", evs, text)
	}
	if m.aiMCPMentions(1, ch, "no mentions here, mail@x.y", false) != nil {
		t.Error("a resolver for a message without mentions")
	}
}

// The mentioned resources go into the turn's user message.
func TestAIAddMentioned(t *testing.T) {
	h := []ai.Message{{Role: ai.RoleSystem, Content: "sys"}, {Role: ai.RoleUser, Content: "see @fake:x://y"}}
	aiAddMentioned(context.Background(), h, func(context.Context, func(ai.Event)) (string, []ai.Image) {
		return "Attached MCP resource fake:x://y", []ai.Image{{MediaType: "image/png"}}
	}, func(ai.Event) {})
	if h[1].Content != "see @fake:x://y\n\nAttached MCP resource fake:x://y" || len(h[1].Images) != 1 || h[0].Content != "sys" {
		t.Fatalf("history %+v", h)
	}
}
