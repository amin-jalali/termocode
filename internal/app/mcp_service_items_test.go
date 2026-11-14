package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// MCPX-39: work with a connected service yourself. AI: MCP: Browse a
// Service… lists its servers, then the tools that only read and need no
// argument (favorites first), runs one and lists the result lines; a line
// goes to the chat as an attachment so you (or the AI) can act on it.

func TestMCPBrowsableToolsAreReadOnlyWithNoRequiredArgs(t *testing.T) {
	yes := true
	ro := &mcp.ToolAnnotations{ReadOnlyHint: &yes}
	req := map[string]any{"required": []any{"text"}}
	tools := []mcp.Tool{
		{Name: "echo", Annotations: ro, InputSchema: req},
		{Name: "write_note"},
		{Name: "roots", Annotations: ro},
		{Name: "mixed", Annotations: ro, InputSchema: map[string]any{"type": "object"}},
	}
	items := mcpServiceToolItems("fake", tools, map[string]bool{"fake.mixed": true})
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	if got := strings.Join(ids, ","); got != "tool-mixed,tool-roots" {
		t.Fatalf("items = %s (favorites first; no write, no required argument)", got)
	}
	if !strings.Contains(items[0].Title, "★") || strings.Contains(items[1].Title, "★") {
		t.Fatalf("favorite mark: %q %q", items[0].Title, items[1].Title)
	}
}

func TestMCPFavoritesAreKeptInTheConfigDir(t *testing.T) {
	covMCPModel(t)
	if len(mcpLoadFavorites()) != 0 {
		t.Fatal("favorites before any was set")
	}
	if on, err := mcpToggleFavorite("fake", "mixed"); err != nil || !on {
		t.Fatalf("toggle on: %v %v", on, err)
	}
	if !mcpLoadFavorites()["fake.mixed"] {
		t.Fatal("favorite not saved")
	}
	if on, err := mcpToggleFavorite("fake", "mixed"); err != nil || on {
		t.Fatalf("toggle off: %v %v", on, err)
	}
	if mcpLoadFavorites()["fake.mixed"] {
		t.Fatal("favorite not removed")
	}
}

func TestMCPBrowseAServiceEndToEnd(t *testing.T) {
	m := covMCPModel(t)
	writeUserMCP(t, mcptest.Config(map[string]map[string]string{"fake": nil}))
	cmd, ok := m.dispatchGitxPalette(mcpServiceBrowseID) // AI: MCP: Browse a Service…
	if !ok || cmd == nil {
		t.Fatal("the palette command gave no command")
	}
	msg := cmd().(gitxDoneMsg)
	msg.apply(m)
	if !strings.Contains(pickerIDs(m), "srv-fake") {
		t.Fatalf("servers: %s", pickerIDs(m))
	}
	msg = m.gitx.onPick(m, "srv-fake")().(gitxDoneMsg)
	msg.apply(m)
	if ids := pickerIDs(m); !strings.Contains(ids, "tool-mixed") || strings.Contains(ids, "tool-echo") || strings.Contains(ids, "tool-write_note") {
		t.Fatalf("tools: %s", ids)
	}
	// A tool opens Run / Favorite; Run lists the result's lines.
	_ = m.gitx.onPick(m, "tool-mixed")
	if ids := pickerIDs(m); !strings.Contains(ids, "run") || !strings.Contains(ids, "favorite") {
		t.Fatalf("tool actions: %s", ids)
	}
	msg = m.gitx.onPick(m, "run")().(gitxDoneMsg)
	msg.apply(m)
	if ids := pickerIDs(m); !strings.Contains(ids, "line-0") {
		t.Fatalf("result lines: %s", ids)
	}
	// A line is attached to the chat.
	m.gitx.onPick(m, "line-0")
	if len(m.aiS().attachments) != 1 || !strings.Contains(m.aiS().attachments[0].Label, "fake.mixed") {
		t.Fatalf("attachments %+v", m.aiS().attachments)
	}
}

func pickerIDs(m *Model) string {
	var ids []string
	for _, it := range m.gitx.items {
		ids = append(ids, it.ID)
	}
	return strings.Join(ids, ",")
}
