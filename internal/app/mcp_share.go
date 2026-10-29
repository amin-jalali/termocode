package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// Shared MCP servers (G-MCP, internal/mcp/share.go): one local server
// process for every termocode session of the user. cmd/termocode tells
// the app which binary runs the share broker; without it (tests, an
// unknown binary) every session runs its own servers.

var (
	mcpShareExe  string
	mcpShareArgs []string
	mcpShareDir  string // "" = the default socket folder (tests set one)
)

// SetMCPShareExe is the termocode binary that runs the share broker
// (main sets it; it calls mcp.RunShareIfHelper first).
func SetMCPShareExe(exe string) { mcpShareExe = exe }

// mcpShareOptions are the Manager's share options: nil when mcp_share is
// off or there is no broker binary.
func mcpShareOptions(set mcp.Settings) *mcp.ShareOptions {
	if !set.Share || mcpShareExe == "" {
		return nil
	}
	return &mcp.ShareOptions{Exe: mcpShareExe, Args: mcpShareArgs, Dir: mcpShareDir}
}

// mcpSessionsText is "1 session" / "3 sessions".
func mcpSessionsText(n int) string {
	if n == 1 {
		return "1 session"
	}
	return fmt.Sprintf("%d sessions", n)
}

// confirmKindMCPShareStop is the "Stop it for all sessions?" card.
const confirmKindMCPShareStop confirmKindEnum = 2212

// openMCPShareStop asks before a shared server stops: it stops for every
// session that uses it. Cancel is first and focused.
func (m *Model) openMCPShareStop(name string, sessions int) tea.Cmd {
	m.aiS().mcpSel = name
	msg := fmt.Sprintf("%s runs once for %s. This stops it for %s.\nEach one starts it again on its next tool call.",
		name, mcpSessionsText(sessions), mcpSessionsText(sessions))
	m.confirm = confirm.Risky("Stop MCP "+name+" for all sessions?", msg,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "stop", Title: "Stop for all", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindMCPShareStop
	return nil
}

// answerMCPShareStop runs the card's button (Esc is Cancel).
func (m *Model) answerMCPShareStop(id string) tea.Cmd {
	name := m.aiS().mcpSel
	if id != "stop" || name == "" {
		return nil
	}
	m.aiMCP().Stop(name)
	return m.aiInfo("MCP " + name + ": stopped for all sessions")
}
