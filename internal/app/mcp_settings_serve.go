package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/toast"
)

// AH-9: mcp_server (termocode as an MCP server for outside agents) as a
// Settings row in the MCP group. The AI never lists or sets it
// (aiSettingPolicies["mcp_server"] is never); only the user picks.

const mcpServeRowID = "setting-mcp-server"

var mcpServeChoices = []string{"auto", "on", "off"}

func init() {
	settingsRows = append(settingsRows, settingsRow{ID: mcpServeRowID, Label: "mcp_server", Kind: "enum", Default: "auto",
		Desc: "termocode as an MCP server: outside agents (Claude Code, Codex, …) reach a short list of IDE tools (open files, diagnostics, tests, debugger, terminal, git reads and the stash) over a private socket in the session folder, open only to your own user. Tools that change something ask on a card first; files, settings, other git writes and your own MCP servers are never offered. auto: serve once you start an agent tab (F1 → Agent: Start …). on: serve from start. off: never. Applied at once."})
}

// mcpServeSettingValue is the row's value. ok=false: not ours.
func mcpServeSettingValue(id string) (string, bool) {
	if id != mcpServeRowID {
		return "", false
	}
	return mcpServeMode(), true
}

// applyMCPServeSetting saves mcp_server and starts or stops the listener
// to match. ok=false: not ours.
func (m *Model) applyMCPServeSetting(id, value string) (tea.Cmd, bool) {
	if id != mcpServeRowID {
		return nil, false
	}
	ok := false
	for _, c := range mcpServeChoices {
		ok = ok || c == value
	}
	if !ok {
		return m.pushToastDetail(toast.Errr, "mcp_server: expected auto, on or off", ""), true
	}
	path, err := settingsConfigPath()
	if err == nil {
		err = mcp.SaveSetting(path, "mcp_server", value)
	}
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Settings error", err.Error()), true
	}
	return tea.Batch(m.syncMCPServe(), m.pushToastDetail(toast.Info, "mcp_server = "+value, imServeMode(value))), true
}
