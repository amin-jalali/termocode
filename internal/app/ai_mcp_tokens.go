package app

import (
	"encoding/json"
	"fmt"

	"github.com/amin-jalali/termocode/internal/mcp"
)

// ── Token cost (MCPX-10) ───────────────────────────────────────────────
//
// Every chat message carries the schemas of all MCP tools. The servers
// list and a server's screen show an estimate of what they cost; past
// mcpTokenWarn the list warns and says how to turn tools off.

// mcpTokenWarn is the estimated tokens of all MCP tool schemas above
// which the servers list warns (a var for tests).
var mcpTokenWarn = 10000

// mcpToolTokens estimates the tokens a server's tools add to each
// message: about 4 bytes of JSON per token, for the tools the model gets
// (not hidden, not off).
func mcpToolTokens(srv mcp.Server, tools []mcp.Tool, set mcp.Settings) int {
	n := 0
	for _, t := range tools {
		if exposed, _, _ := mcp.Gate(srv, t, set.TrustAnnotations); !exposed || set.ToolOff(srv.Name, t.Name) {
			continue
		}
		desc := t.Description
		if len(desc) > 1024 {
			desc = desc[:1024]
		}
		b, _ := json.Marshal(map[string]any{"name": mcp.WireName(srv.Name, t.Name), "description": "[MCP " + srv.Name + "] " + desc, "input_schema": t.InputSchema})
		n += (len(b) + 3) / 4
	}
	return n
}

// mcpServerTokens is mcpToolTokens of a server's known tools (0 when
// none are known or it is off).
func mcpServerTokens(m *Model, name string, set mcp.Settings) int {
	srv, tools, ok := m.aiMCP().KnownTools(name)
	if !ok || srv.Disabled {
		return 0
	}
	return mcpToolTokens(srv, tools, set)
}

// mcpTokensText is "~340 tokens", "~1.2k tokens", "~23k tokens" ("" for 0).
func mcpTokensText(n int) string {
	switch {
	case n <= 0:
		return ""
	case n < 1000:
		return fmt.Sprintf("~%d tokens", n)
	case n < 10000:
		return fmt.Sprintf("~%.1fk tokens", float64(n)/1000)
	}
	return fmt.Sprintf("~%dk tokens", (n+500)/1000)
}

// mcpTokenNote is the servers list warning when all MCP tools together
// cost more than mcpTokenWarn (nil otherwise).
func mcpTokenNote(total int) []string {
	if total <= mcpTokenWarn {
		return nil
	}
	return []string{
		"MCP tools use " + mcpTokensText(total) + " of each message: slower and more costly.",
		"Turn off tools you do not need: a server → Show tools → Space.",
	}
}
