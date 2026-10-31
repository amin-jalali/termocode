package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/mcp"
)

// "AI: MCP: Reconnect All Servers" (RT-5): every failed or waiting
// server tries again now; running ones get a health ping.

type mcpReconnectAllDoneMsg struct{ res mcp.ReconnectResult }

func (m *Model) mcpReconnectAll() tea.Cmd {
	mgr := m.aiMCP()
	return tea.Batch(m.aiInfo("MCP: reconnecting…"), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		return mcpReconnectAllDoneMsg{res: mgr.ReconnectAll(ctx)}
	})
}

func (m *Model) onMCPReconnectAllDone(msg mcpReconnectAllDoneMsg) tea.Cmd {
	return m.aiInfo(mcpReconnectAllText(msg.res))
}

// mcpReconnectAllText is the toast after Reconnect All.
func mcpReconnectAllText(r mcp.ReconnectResult) string {
	if r.Tried == 0 {
		return "MCP: every server is fine"
	}
	s := fmt.Sprintf("MCP: %d of %d servers back", r.OK, r.Tried)
	if len(r.Failed) > 0 {
		s += " · still down: " + strings.Join(r.Failed, ", ")
	}
	return s
}
