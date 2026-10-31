package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/mcp"
)

// ── MCP log: one server, a level (MCPX-30) ─────────────────────────────
//
// The MCP channel of the Output panel holds every server's lines
// ("15:04:05  server: text"). A server's Show log shows only its lines;
// mcp_log_level hides the lines below it. Every line is still kept, so a
// lower level shows the older lines too.

// mcpLogRank orders the levels: error 0 … debug 3.
func mcpLogRank(level string) int {
	for i, l := range mcp.LogLevels {
		if l == level {
			return i
		}
	}
	return 2 // info
}

// mcpLogLineLevel is the level of one MCP log line: the server's own
// level (notifications/message: "debug: …"), error for a failure, else
// info (lifecycle and stderr lines).
func mcpLogLineLevel(line string) string {
	text := line
	if _, rest, ok := strings.Cut(line, "  "); ok { // after the time
		if _, msg, ok := strings.Cut(rest, ": "); ok { // after the server
			text = msg
		}
	}
	low := strings.ToLower(text)
	for prefix, level := range map[string]string{"debug:": "debug", "info:": "info", "notice:": "info",
		"warning:": "warning", "error:": "error", "critical:": "error", "alert:": "error", "emergency:": "error"} {
		if strings.HasPrefix(low, prefix) {
			return level
		}
	}
	if strings.Contains(low, "failed") || strings.Contains(low, "error") {
		return "error"
	}
	return "info"
}

// showMCPLogFor opens the MCP log for one server ("" = every server) at
// the mcp_log_level level.
func (m *Model) showMCPLogFor(server string) tea.Cmd {
	m.aiMCP() // the channel exists once MCP ran; show it empty otherwise
	m.aiS().mcpLogServer = server
	m.applyMCPLogFilter()
	m.output.SetActive(mcpLogChannel)
	m.showPanelTab(panelKindOutput)
	return nil
}

// applyMCPLogFilter sets the MCP channel's filter from the shown server
// and mcp_log_level.
func (m *Model) applyMCPLogFilter() {
	server := m.aiS().mcpLogServer
	level := mcp.LoadSettings(mcpConfigPath()).LogLevel
	if server == "" && level == "debug" {
		m.output.SetFilter(mcpLogChannel, nil, "")
		return
	}
	max := mcpLogRank(level)
	tag := "  " + server + ": "
	keep := func(l string) bool {
		if server != "" && !strings.Contains(l, tag) {
			return false
		}
		return mcpLogRank(mcpLogLineLevel(l)) <= max
	}
	note := "MCP log · level " + level + " (mcp_log_level)"
	if server != "" {
		note = "MCP log: only " + server + " · level " + level + " (mcp_log_level) · AI: MCP: Show Log shows all"
	}
	m.output.SetFilter(mcpLogChannel, keep, note)
}
