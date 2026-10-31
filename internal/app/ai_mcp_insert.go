package app

import (
	"context"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/picker"
)

// "AI: MCP Prompts and Resources…" (MCPX-12/13): one picker lists the
// prompts and resources of the connected servers, so "/server.prompt"
// and "@server:uri" are easy to find. A pick puts the text in the chat
// input; nothing is sent until the user presses Enter.

// pickerKindMCPInsert is that picker (MCPX kinds start at 2440).
const pickerKindMCPInsert pickerKindEnum = 2440

type aiMCPInsertListMsg struct{ items []picker.Item }

// openMCPInsertPicker loads the lists (servers start; untrusted project
// servers are skipped).
func (m *Model) openMCPInsertPicker() tea.Cmd {
	mgr := m.aiMCP()
	return tea.Batch(m.aiInfo("Loading MCP prompts and resources…"), func() tea.Msg {
		mgr.Reload()
		ctx, cancel := context.WithTimeout(context.Background(), aiMCPPromptTimeout)
		defer cancel()
		var items []picker.Item
		for _, st := range mgr.Servers() {
			if st.State == mcp.StateInvalid || st.State == mcp.StateDisabled || st.State == mcp.StateSignIn {
				continue
			}
			ps, _ := mgr.Prompts(ctx, nil, st.Name)
			for _, p := range ps {
				hint := p.Description
				if u := aiMCPPromptUsage(p); u != "" {
					hint = u + " · " + hint
				}
				title := p.Title
				if title == "" {
					title = p.Name
				}
				items = append(items, picker.Item{ID: "/" + st.Name + "." + p.Name + " ", Title: "/" + st.Name + "." + p.Name + " — " + title, Hint: "prompt · " + hint})
			}
			rs, _, _ := mgr.Resources(ctx, nil, st.Name)
			for _, r := range rs {
				items = append(items, picker.Item{ID: "@" + st.Name + ":" + r.URI + " ", Title: "@" + st.Name + ": " + r.Label(), Hint: "resource · " + r.URI})
			}
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		return aiMCPInsertListMsg{items: items}
	})
}

func (m *Model) onAIMCPInsertList(msg aiMCPInsertListMsg) tea.Cmd {
	if len(msg.items) == 0 {
		return m.aiInfo("No MCP prompts or resources: no connected server has any")
	}
	m.picker = picker.NewItems(" MCP prompts and resources (Enter: put in the chat) ", msg.items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindMCPInsert
	return nil
}

// onMCPInsertPicked adds the pick to the chat input (a prompt command
// replaces it: it must start the message).
func (m *Model) onMCPInsertPicked(id string) tea.Cmd {
	cmd := m.openAIPanel(true)
	in := string(m.aiS().input)
	switch {
	case strings.HasPrefix(id, "/") || strings.TrimSpace(in) == "":
		m.aiSetInput(id)
	case strings.HasSuffix(in, " "):
		m.aiSetInput(in + id)
	default:
		m.aiSetInput(in + " " + id)
	}
	return cmd
}
