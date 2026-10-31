package app

import (
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// ── Tools on/off (MCPX-3) ──────────────────────────────────────────────
//
// Show tools lists a server's tools; Space turns one off or on. Off
// tools (config.json mcp_disabled_tools) are never sent to the model.

// mcpTurnTools turns the servers' tools of a chat turn into agent tools,
// without the tools the user turned off.
func mcpTurnTools(list []mcp.ServerTools, set mcp.Settings, call mcp.CallFunc, opts mcp.AdapterOptions) []ai.Tool {
	var out []ai.Tool
	for _, st := range list {
		tools := st.Tools
		if len(set.DisabledTools[st.Server.Name]) > 0 {
			tools = nil
			for _, t := range st.Tools {
				if !set.ToolOff(st.Server.Name, t.Name) {
					tools = append(tools, t)
				}
			}
		}
		o := opts
		o.ToolsChanged = st.ToolsChanged
		out = append(out, mcp.AITools(st.Server, tools, call, o)...)
	}
	return out
}
