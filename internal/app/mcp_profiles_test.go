package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// MCPX-27: AI: MCP: Switch Profile… lists "All servers" and the
// profiles with what they keep; picking one makes this chat's turns use
// only its servers and tools.
func TestMCPProfilePickerAndTurnFilter(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	m := covMCPModel(t)
	pp := mcp.ProjectPath(aiWorkspaceRoot())
	_ = os.MkdirAll(filepath.Dir(pp), 0o755)
	if err := os.WriteFile(pp, []byte(mcptest.Config(map[string]map[string]string{"fake": nil})), 0o644); err != nil {
		t.Fatal(err)
	}
	m.aiS().mcpSel = "fake"
	m.onMCPServerAction("trust")
	if err := os.WriteFile(mcp.ProjectProfilesPath(aiWorkspaceRoot()),
		[]byte(`{"profiles":{"small":{"description":"one tool","tools":["fake.echo"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if c, ok := m.dispatchAIPalette("ai-mcp-profile"); !ok || c != nil || !m.pickerOpen || m.pickerKind != pickerKindMCPProfile {
		t.Fatal("the palette entry did not open the profile picker")
	}
	view := m.picker.View()
	for _, want := range []string{"All servers and tools", "small", "one tool", "project"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker lacks %q:\n%s", want, view)
		}
	}
	m.handleMCPPickerSelect("small")
	if m.aiS().mcpProfile != "small" {
		t.Fatalf("profile = %q", m.aiS().mcpProfile)
	}
	tools := mcpWireTools(m.aiMCPTurn(1, make(chan tea.Msg, 8))(context.Background()))
	if len(tools) != 1 || tools[0].Display != "mcp.fake.echo" {
		var n []string
		for _, tl := range tools {
			n = append(n, tl.Display)
		}
		t.Fatalf("tools = %v", n)
	}
	m.openMCPProfiles()
	m.handleMCPPickerSelect(mcpProfileAllID)
	if m.aiS().mcpProfile != "" {
		t.Fatal("All did not clear the profile")
	}
	if len(mcpWireTools(m.aiMCPTurn(2, make(chan tea.Msg, 8))(context.Background()))) < 2 {
		t.Fatal("All did not give every tool back")
	}
}

// mcpWireTools are a turn's server tools (mcp__…), without the resource,
// result and search tools termocode adds beside them.
func mcpWireTools(tools []ai.Tool) []ai.Tool {
	var out []ai.Tool
	for _, t := range tools {
		if strings.HasPrefix(t.Name, mcp.WirePrefix) {
			out = append(out, t)
		}
	}
	return out
}
