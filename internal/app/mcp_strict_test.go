package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/mcp"
)

// Spec (MCPX-33, CF-27): with mcp_strict on, a server whose entry has
// keys termocode does not read gets a warning row under it in the
// servers list; Enter on the row opens that server (Edit in mcp.json is
// there). Off (the default): no row. The setting is in Settings → MCP.
func TestMCPStrictWarningRow(t *testing.T) {
	t.Chdir(t.TempDir())
	m := covMCPModel(t)
	writeUserMCP(t, `{"mcpServers": {"typo": {"command": "x", "comand": "y"}, "fine": {"command": "z"}}}`)
	m.aiMCP().Reload()
	box := func() string {
		m.openMCPServers()
		m.picker.SetSize(200, 60)
		return ansi.Strip(m.picker.Box())
	}
	if got := box(); strings.Contains(got, "unknown key") {
		t.Fatalf("a warning row while mcp_strict is off:\n%s", got)
	}
	if _, ok := m.applyMCPSetting("setting-mcp-strict", "true"); !ok || !mcp.LoadSettings(mcpConfigPath()).Strict {
		t.Fatal("mcp_strict was not saved")
	}
	got := box()
	if strings.Count(got, "unknown key") != 1 || !strings.Contains(got, "unknown key: comand") {
		t.Fatalf("warning rows in\n%s", got)
	}
	m.picker.SetCursorID(mcpWarnPrefix + "typo")
	if it, ok := m.picker.Current(); !ok || it.ID != mcpWarnPrefix+"typo" {
		t.Fatalf("no warning row for typo: %+v", it)
	}
	m.handleMCPPickerSelect(mcpWarnPrefix + "typo")
	if m.pickerKind != pickerKindMCPServer || m.aiS().mcpSel != "typo" {
		t.Fatalf("Enter on the warning opened kind %v sel %q", m.pickerKind, m.aiS().mcpSel)
	}
}
