package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tea "github.com/charmbracelet/bubbletea"
)

// Spec (plan §5): with MCP servers configured, the chat panel header
// shows a chip "⌁ N MCP" (N = connected servers); a click on it opens
// AI: MCP Servers…. Without servers there is no chip.
func TestMCPHeaderChip(t *testing.T) {
	m := aiTestModel(t)
	m.actionsOpen, m.actionsPinned = true, true
	if strings.Contains(ansi.Strip(m.renderActionsHeader(60)), "MCP") {
		t.Fatal("chip without any MCP server")
	}
	if err := os.MkdirAll(filepath.Dir(mcpUserPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mcpUserPath(), []byte(`{"mcpServers":{"a":{"command":"termocode-no-such-cmd"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m.aiMCP()
	head := ansi.Strip(m.renderActionsHeader(60))
	i := strings.Index(head, mcpGlyph()+" 0 MCP") // ⌁ ("~" in ASCII icon mode)
	if i < 0 {
		t.Fatalf("no chip in header %q", head)
	}
	x := m.w - m.actionsColumnWidth() + ansi.StringWidth(head[:i]) + 2
	if id := m.hitActionsButton(x, 0); id != "mcp" {
		t.Fatalf("hit at chip = %q", id)
	}
	if id := m.aiPanelTargetAt(x, 0); id != "ai:mcp" {
		t.Fatalf("target at chip = %q", id)
	}
	m.handleAIPanelMouse(tea.MouseMsg{X: x, Y: 0, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !m.pickerOpen || m.pickerKind != pickerKindMCPServers {
		t.Errorf("click on the chip did not open the MCP servers picker")
	}
}
