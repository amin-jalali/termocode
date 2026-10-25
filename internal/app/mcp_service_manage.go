package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/mcp"
)

// mcpSetDisabled turns a service off or on: "disabled": true in the file
// the entry lives in (other keys stay). An off service stays listed, is
// not started and gives the AI no tools; turning it off stops it now.
func (m *Model) mcpSetDisabled(name string, off bool) tea.Cmd {
	entries := mcpServerEntries(name)
	if len(entries) == 0 {
		return nil
	}
	if err := mcp.SetDisabled(entries[0].SourcePath, name, off); err != nil {
		return m.aiToastErr("MCP "+name, err)
	}
	mgr := m.aiMCP()
	mgr.Reload() // a changed entry is stopped there
	if off {
		mgr.Stop(name)
		return m.aiInfo("MCP " + name + ": off (Enable brings it back)")
	}
	return m.aiInfo("MCP " + name + ": on")
}
