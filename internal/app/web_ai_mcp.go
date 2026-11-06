package app

import (
	"strings"

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

// The agent's auto-approve rows from the web (web parity: AI: Agent
// Auto-Approve…): the laptop picker's rows, and a switch of one of them
// after a laptop card.

func init() {
	webOps[webproto.OpAIAutoApprove] = (*Model).webAIAutoApprove
	webAskOps[webproto.OpAISetAutoApprove] = (*Model).webAISetAutoApprove
}

func (m *Model) webAIAutoApprove(call *webCall) tea.Cmd {
	out := webproto.AutoApprove{Rows: []webproto.AutoApproveRow{}}
	for _, it := range m.aiAutoApproveItems() {
		row := webproto.AutoApproveRow{Title: webName(strings.TrimSpace(it.Title)), Header: it.Header}
		if !it.Header {
			row.ID, row.On = webName(it.ID), it.Hint == "AUTO"
		}
		out.Rows = append(out.Rows, row)
	}
	call.reply(out)
	return nil
}

func (m *Model) webAISetAutoApprove(call *webCall) *webAsk {
	id := call.args.(*webproto.AutoApproveArgs).ID
	for _, it := range m.aiAutoApproveItems() {
		if it.Header || it.ID != id {
			continue
		}
		title, body, action := "Ask before "+id+"?", "The AI asks before it does this again: "+it.Title+".", "Ask First"
		if it.Hint != "AUTO" {
			title, body, action = "Run "+id+" without asking?", "The AI does this without asking you: "+it.Title+".", "Turn On AUTO"
		}
		return &webAsk{summary: id, title: title, body: body, action: action,
			run: func(m *Model) tea.Cmd { return m.onAIAutoApproveToggled(id) }}
	}
	call.fail(webproto.CodeNotFound, "There is no such row.")
	return nil
}
