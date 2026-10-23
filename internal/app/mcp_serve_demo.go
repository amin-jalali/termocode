package app

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
)

// mcpServeDemo is the dev action "mcpservedemo:<variant>" (shots and
// goldens): "status" shows the MCP server status screen, "card" an
// outside agent's confirm card, "chip" the status bar while the server
// listens with no agent and "opencode" the card that asks to add termocode
// to a project's opencode.json, and "tip-gemini" / "tip-aider" the one-time
// hint for agents termocode cannot hand its tools to, over made-up state (a fake socket path, one
// agent, a few audit lines). It starts no listener and writes nothing.
func (m *Model) mcpServeDemo(variant string) tea.Cmd {
	hub := newMCPServeHub()
	switch variant {
	case "chip": // listening, nobody connected: the quiet "ready" chip
		m.mcpServe = &mcpServeState{hub: hub, path: "/run/user/1000/termocode/3f9a1c2b.mcp.sock"}
		return nil
	case "tip-gemini", "tip-aider": // the one-time hand tip toast
		if a, ok := agents.ByID(strings.TrimPrefix(variant, "tip-")); ok {
			return m.agentMCPHandTip(a)
		}
		return nil
	case "opencode": // the project-file card of OpenCode (nothing is written)
		if a, ok := agents.ByID("opencode"); ok {
			m.openAgentMCPCard(&agentMCPCard{agent: a, path: "/home/me/project/opencode.json"})
		}
		return nil
	}
	m.mcpServe = &mcpServeState{hub: hub, path: "/run/user/1000/termocode/3f9a1c2b.mcp.sock",
		clients: []*mcpServeAgent{{id: 1, name: "claude-code", since: time.Date(2026, 10, 14, 9, 41, 0, 0, time.UTC), calls: 14, asked: 2}}}
	switch variant {
	case "status":
		if m.output != nil {
			for _, l := range []string{
				`09:41:12  get_ide_state  gate=auto  by=mcp:claude-code  → 2 tabs, branch main`,
				`09:41:30  diagnostics  gate=auto  by=mcp:claude-code  → 3 problems`,
				`09:42:05  tasks_run  label="say hi"  gate=confirm:allowed  by=mcp:claude-code  → started`,
				`09:42:40  debug_start  config=Launch  gate=confirm  by=mcp:claude-code  → declined: declined by the user`,
			} {
				m.output.Append(aiAuditChannel, l)
			}
		}
		return m.openMCPServeStatus()
	case "card":
		tool, ok := ai.FindTool(mcpServeCatalog(), "tasks_run")
		if !ok {
			return nil
		}
		args := map[string]any{"label": "say hi"}
		key := mcpServeAlwaysKey(tool, args)
		t, _ := mcpServeGate(tool, args)
		m.mcpServe.queue = append(m.mcpServe.queue, &mcpServeAsk{ctx: context.Background(), client: "claude-code", tool: t,
			human: t.Human(args), args: aiAuditArgs(args), always: key, reply: make(chan bool, 1)})
		m.mcpServe.ticking = true // no timer for a shot
		m.mcpServePump()
	}
	return nil
}
