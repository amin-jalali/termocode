package app

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

func mcpAddTool(t *testing.T) ai.Tool {
	t.Helper()
	tl, ok := ai.FindTool(aiMCPAddTools(), "mcp_server_add")
	if !ok {
		t.Fatal("no mcp_server_add tool")
	}
	return tl
}

// Spec (MCPX-37): the AI proposes a server from a pasted block; the tool
// always asks (mutating, no class: no "Always allow"), the card shows the
// entry and what runs, and only Allow writes it to the user's mcp.json.
func TestAIMCPAddFromBlock(t *testing.T) {
	withConfigDir(t)
	tl := mcpAddTool(t)
	if !tl.Mutating || tl.Class != "" {
		t.Fatalf("gate: mutating %v class %q", tl.Mutating, tl.Class)
	}
	args := map[string]any{"config": `{"mcpServers":{"files":{"command":"npx","args":["-y","@x/fs","."]}}}`}
	if err := tl.Validate(args); err != nil {
		t.Fatal(err)
	}
	card := tl.Human(args)
	for _, w := range []string{"files", `"command": "npx"`, "It runs on this computer: npx -y @x/fs .", "Nothing starts now"} {
		if !strings.Contains(card, w) {
			t.Errorf("card misses %q:\n%s", w, card)
		}
	}
	if b, _ := os.ReadFile(mcpUserPath()); len(b) != 0 {
		t.Fatal("written before Allow")
	}
	out, err := tl.Run(context.Background(), args)
	if err != nil || !strings.Contains(out, "added files") {
		t.Fatalf("%q %v", out, err)
	}
	b, _ := os.ReadFile(mcpUserPath())
	if !strings.Contains(string(b), `"npx"`) {
		t.Fatalf("file %s", b)
	}
	if err := tl.Validate(args); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("a second add: %v", err)
	}
}

// Spec (MCPX-37): a remote URL becomes an http entry; the card says where
// it connects.
func TestAIMCPAddFromURL(t *testing.T) {
	withConfigDir(t)
	tl := mcpAddTool(t)
	args := map[string]any{"url": "https://mcp.example.com/mcp", "name": "example"}
	if err := tl.Validate(args); err != nil {
		t.Fatal(err)
	}
	if card := tl.Human(args); !strings.Contains(card, "It connects to https://mcp.example.com/mcp") {
		t.Fatalf("card %s", card)
	}
}

// Spec (MCPX-37): refused before any card: no source, two sources, a bad
// URL, a ${cmd:} reference, a literal secret, more than one server.
func TestAIMCPAddRefuses(t *testing.T) {
	withConfigDir(t)
	tl := mcpAddTool(t)
	for _, a := range []map[string]any{
		{},
		{"url": "https://a.example/mcp", "config": `{"a":{"command":"x"}}`},
		{"url": "http://evil.example/mcp"},
		{"config": `{"a":{"command":"sh","env":{"T":"${cmd:cat ~/.ssh/id_rsa}"}}}`},
		{"config": `{"a":{"type":"http","url":"https://a.example/mcp","headers":{"Authorization":"Bearer ` + "sk-" + `test12345678901234567890"}}}`},
		{"config": `{"a":{"command":"x"},"b":{"command":"y"}}`},
		{"config": `not json`},
	} {
		if err := tl.Validate(a); err == nil {
			t.Errorf("%v: no error", a)
		}
	}
}

// Spec (MCPX-37): the card puts Deny first, names the action ("Add
// server"), has no Always allow; a read-only step and outside agents
// never get the tool.
func TestAIMCPAddCardAndReach(t *testing.T) {
	m := aiTestModel(t)
	tools := m.aiChatTools(".", nil)
	tl, ok := ai.FindTool(tools, "mcp_server_add")
	if !ok {
		t.Fatal("not in the chat tools")
	}
	var titles []string
	for _, b := range aiToolCardButtons(tl) {
		titles = append(titles, b.Title)
	}
	if got := strings.Join(titles, " · "); got != "Deny · Add server" {
		t.Fatalf("buttons %s", got)
	}
	if _, ok := ai.FindTool(aiFilterTools(tools, agentdag.ToolRule{Preset: agentdag.PresetReadOnly}), "mcp_server_add"); ok {
		t.Error("a read-only step gets it")
	}
	if _, ok := mcpServeRow("mcp_server_add"); ok {
		t.Error("offered to outside agents")
	}
}
