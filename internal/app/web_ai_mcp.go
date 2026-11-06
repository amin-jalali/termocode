package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The chat's MCP profile from the web (web parity: AI: MCP: Switch
// Profile…): the list the laptop's picker shows, and a switch that runs
// only after a laptop card (it changes which tools the AI may call).

func init() {
	webOps[webproto.OpAIMCPProfiles] = (*Model).webAIMCPProfiles
	webAskOps[webproto.OpAIUseMCPProfile] = (*Model).webAIUseMCPProfile
}

func (m *Model) webAIMCPProfiles(call *webCall) tea.Cmd {
	ps, _ := mcpLoadProfiles()
	out := webproto.MCPProfiles{Current: webName(m.aiS().mcpProfile), Profiles: []webproto.MCPProfileItem{}}
	for _, p := range ps {
		if len(out.Profiles) == webproto.MaxMCPProfiles {
			break
		}
		hint := p.Source + " · "
		if p.Description != "" {
			hint += p.Description + " · "
		}
		out.Profiles = append(out.Profiles, webproto.MCPProfileItem{Name: webName(p.Name), Hint: webName(hint + p.Summary())})
	}
	call.reply(out)
	return nil
}

func (m *Model) webAIUseMCPProfile(call *webCall) *webAsk {
	a := call.args.(*webproto.AIMCPProfileArgs)
	cur := m.aiS().mcpProfile
	if a.All {
		if cur == "" {
			call.fail(webproto.CodeConflict, "This chat uses all servers already.")
			return nil
		}
		return &webAsk{
			summary: "all",
			title:   "Use all MCP servers?",
			body:    "The AI chat may use every MCP server and tool again.",
			action:  "Use All",
			run:     func(m *Model) tea.Cmd { return m.onMCPProfilePicked(mcpProfileAllID) },
		}
	}
	ps, _ := mcpLoadProfiles()
	p, ok := mcp.FindProfile(ps, a.Name)
	switch {
	case !ok:
		call.fail(webproto.CodeNotFound, "There is no such MCP profile.")
		return nil
	case cur == p.Name:
		call.fail(webproto.CodeConflict, "That profile is in use already.")
		return nil
	}
	name := p.Name
	return &webAsk{
		summary: name,
		title:   "Use MCP profile " + name + "?",
		body:    "The AI chat uses only the servers and tools of profile " + name + " (" + p.Summary() + ").",
		action:  "Use",
		run:     func(m *Model) tea.Cmd { return m.onMCPProfilePicked(name) },
	}
}
