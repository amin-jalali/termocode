package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// MCPX-17: mcp_tool_search is an MCP Settings row (auto, on, off); a bad
// value is refused and nothing is written.
func TestMCPToolSearchSettingRow(t *testing.T) {
	m := aiTestModel(t)
	if v, ok := mcpSettingValue("setting-mcp-tool-search"); !ok || v != "auto" {
		t.Fatalf("row value %q ok=%v", v, ok)
	}
	m.applySetting("setting-mcp-tool-search", "on")
	if s := mcp.LoadSettings(mcpConfigPath()); s.ToolSearch != mcp.ToolSearchOn {
		t.Fatalf("saved %q", s.ToolSearch)
	}
	before, _ := os.ReadFile(mcpConfigPath())
	m.applySetting("setting-mcp-tool-search", "sometimes")
	if after, _ := os.ReadFile(mcpConfigPath()); string(after) != string(before) {
		t.Fatal("a bad value was saved")
	}
	if p, ok := aiSettingPolicies["mcp_tool_search"]; !ok || p.Gate != aiGateNever {
		t.Fatal("the AI must never change mcp_tool_search")
	}
}

// With mcp_tool_search on, a chat turn's MCP tools are hidden behind
// the search tool.
func TestMCPTurnDefersToolsWhenToolSearchOn(t *testing.T) {
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
	if err := mcp.SaveSetting(mcpConfigPath(), "mcp_tool_search", "on"); err != nil {
		t.Fatal(err)
	}
	tools := m.aiMCPTurn(1, make(chan tea.Msg, 8))(context.Background())
	var shown, hidden int
	for _, tl := range tools {
		if tl.IsHidden() {
			hidden++
		} else {
			shown++
		}
	}
	if _, ok := ai.FindTool(tools, mcp.ToolSearchName); !ok || shown != 1 || hidden == 0 {
		t.Fatalf("shown=%d hidden=%d tools=%d", shown, hidden, len(tools))
	}
}
