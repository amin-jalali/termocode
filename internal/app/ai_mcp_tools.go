package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/picker"
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

// mcpToolItems are the rows of Show tools: "[x] name  description" with
// the gate (or "off …") as hint. ok=false: no tools known yet.
func mcpToolItems(m *Model, name string) ([]picker.Item, bool) {
	srv, tools, ok := m.aiMCP().KnownTools(name)
	if !ok || len(tools) == 0 {
		return nil, false
	}
	set := mcp.LoadSettings(mcpConfigPath())
	var items []picker.Item
	for _, t := range tools {
		exposed, mutating, class := mcp.Gate(srv, t, set.TrustAnnotations)
		gate := "runs at once"
		switch {
		case !exposed:
			gate = "hidden"
		case mutating && class == "":
			gate = "always asks"
		case mutating:
			gate = "asks"
		}
		mark := "[x] "
		if set.ToolOff(name, t.Name) {
			mark, gate = "[ ] ", "off · not sent to the AI"
		}
		desc := aiCleanText(strings.SplitN(t.Description, "\n", 2)[0])
		if r := []rune(desc); len(r) > 80 {
			desc = string(r[:79]) + "…"
		}
		items = append(items, picker.Item{ID: t.Name, Title: mark + t.Name + "  " + desc, Hint: gate})
	}
	return items, true
}

// mcpToolsHint is the Show tools row hint: "10 tools" or "10 tools · 2 off".
func mcpToolsHint(m *Model, name string) string {
	_, tools, _ := m.aiMCP().KnownTools(name)
	set := mcp.LoadSettings(mcpConfigPath())
	off := 0
	for _, t := range tools {
		if set.ToolOff(name, t.Name) {
			off++
		}
	}
	h := fmt.Sprintf("%d tools", len(tools))
	if off > 0 {
		h += fmt.Sprintf(" · %d off", off)
	}
	return h
}

// onMCPToolToggle turns one tool of the open Show tools list off or on,
// saves it and redraws the list in place.
func (m *Model) onMCPToolToggle(tool string) tea.Cmd {
	name := m.aiS().mcpSel
	set := mcp.LoadSettings(mcpConfigPath())
	off := set.ToolOff(name, tool)
	var next []string
	for _, n := range set.DisabledTools[name] {
		if n != tool {
			next = append(next, n)
		}
	}
	if !off {
		next = append(next, tool)
	}
	if err := mcp.SaveDisabledTools(mcpConfigPath(), name, next); err != nil {
		return m.aiToastErr("MCP "+name, err)
	}
	if items, ok := mcpToolItems(m, name); ok {
		m.picker.SetItems(items)
	}
	if off {
		return m.aiInfo("MCP " + name + ": " + tool + " is on")
	}
	return m.aiInfo("MCP " + name + ": " + tool + " is off (not sent to the AI)")
}
