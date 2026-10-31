package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/picker"
)

// ── Reset trust (MCPX-9) ───────────────────────────────────────────────
//
// Undo a wrong "Always allow": for a server, its saved start trust
// (mcp_trusted, project servers) and "mcp:<server>" in ai_auto_approve;
// for one tool, its name in the entry's autoApprove (your own mcp.json).
// The card puts Cancel first.

// confirmKindMCPUntrust is the "Reset trust?" card.
const confirmKindMCPUntrust confirmKindEnum = 2290

// mcpUntrustParts are what Reset trust would undo for a server, as card
// lines ("" none).
func (m *Model) mcpUntrustParts(name string) []string {
	var parts []string
	if _, ok := mcp.LoadSettings(mcpConfigPath()).Trusted[name]; ok {
		parts = append(parts, "It asks again before it starts.")
	} else {
		for _, e := range mcpServerEntries(name) {
			if e.Source == mcp.SourceProject && mcpServerStatusOf(m, name).Trusted {
				parts = append(parts, "It asks again before it starts.")
			}
		}
	}
	if contains(m.aiS().settings.AutoApprove, mcp.ClassPrefix+name) {
		parts = append(parts, "Its tools ask again before they change something.")
	}
	return parts
}

// mcpUntrustItems are the Reset trust rows of a server's actions.
func mcpUntrustItems(m *Model, name string) []picker.Item {
	var items []picker.Item
	if len(m.mcpUntrustParts(name)) > 0 {
		items = append(items, picker.Item{ID: "untrust", Title: "Reset trust…", Hint: "undo Always allow; it asks again"})
	}
	for _, e := range mcpServerEntries(name) {
		if e.Source != mcp.SourceUser {
			continue
		}
		for _, t := range e.AutoApprove {
			items = append(items, picker.Item{ID: "untrust-tool:" + t, Title: "Untrust tool " + t + "…", Hint: "it asks again before it runs"})
		}
	}
	return items
}

// openMCPUntrust shows the Reset trust card for a server (tool "") or
// one tool of it.
func (m *Model) openMCPUntrust(name, tool string) tea.Cmd {
	s := m.aiS()
	s.mcpSel, s.mcpUntrustTool = name, tool
	title := "Reset trust for MCP " + name + "?"
	var msg string
	if tool != "" {
		title = "Untrust tool " + tool + " of " + name + "?"
		msg = tool + " asks again before it runs. It leaves autoApprove in your mcp.json."
	} else {
		parts := m.mcpUntrustParts(name)
		if len(parts) == 0 {
			return m.aiInfo("MCP " + name + ": nothing to reset")
		}
		msg = strings.Join(parts, "\n")
	}
	m.confirm = confirm.Risky(title, msg, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "reset", Title: "Reset trust"})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindMCPUntrust
	return nil
}

// answerMCPUntrust handles the card (Esc = cancel).
func (m *Model) answerMCPUntrust(id string) tea.Cmd {
	s := m.aiS()
	name, tool := s.mcpSel, s.mcpUntrustTool
	s.mcpUntrustTool = ""
	if id != "reset" || name == "" {
		return nil
	}
	if tool != "" {
		for _, e := range mcpServerEntries(name) {
			var next []string
			for _, t := range e.AutoApprove {
				if t != tool {
					next = append(next, t)
				}
			}
			var v any = next
			if len(next) == 0 {
				v = nil
			}
			if err := mcp.SetServerKey(e.SourcePath, name, "autoApprove", v); err != nil {
				return m.aiToastErr("MCP "+name, err)
			}
		}
		m.aiMCP().Reload()
		return m.aiInfo("MCP " + name + ": " + tool + " asks again before it runs")
	}
	if err := mcp.ForgetTrusted(mcpConfigPath(), name); err != nil {
		return m.aiToastErr("MCP "+name, err)
	}
	m.aiMCP().Untrust(name)
	class := mcp.ClassPrefix + name
	if contains(s.settings.AutoApprove, class) {
		err := m.saveAISettings(func(st *ai.Settings) {
			var next []string
			for _, c := range st.AutoApprove {
				if c != class {
					next = append(next, c)
				}
			}
			st.AutoApprove = next
		})
		if err != nil {
			return m.aiToastErr("Settings error", err)
		}
	}
	return m.aiInfo("MCP " + name + ": trust reset; it asks again")
}

// mcpServerStatusOf is one server's row of the servers list (zero when
// it is gone).
func mcpServerStatusOf(m *Model, name string) mcp.Status {
	for _, st := range m.aiMCP().Servers() {
		if st.Name == name {
			return st
		}
	}
	return mcp.Status{}
}
