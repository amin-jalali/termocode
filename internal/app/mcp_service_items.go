package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/picker"
)

// Work with a connected MCP service yourself (MCPX-39): AI: MCP: Browse a
// Service… lists the servers, then the tools that only read and need no
// argument (your favorites first), runs one and lists the lines of its
// result. A line goes to the chat as an attachment, so you or the AI can
// act on it (a change still goes through /tool or the AI, and asks first).
// Favorites are tool names in mcp-favorites.json next to config.json.

const mcpServiceBrowseID = "mcp-service-browse"

func init() {
	registerGitx(gitxFeature{
		palette:  []picker.Item{{ID: mcpServiceBrowseID, Title: "AI: MCP: Browse a Service…", Hint: "its lists, favorites"}},
		policies: map[string]aiActionPolicy{mcpServiceBrowseID: aiNever(aiNoPicker)},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			if id == mcpServiceBrowseID {
				return m.openMCPService(), true
			}
			return nil, false
		},
	})
}

func mcpFavoritesPath() string {
	p, err := ai.ConfigPath()
	if err != nil || p == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(p), "mcp-favorites.json")
}

// mcpLoadFavorites reads "server.tool" names; a missing or bad file is none.
func mcpLoadFavorites() map[string]bool {
	out := map[string]bool{}
	b, err := os.ReadFile(mcpFavoritesPath())
	if err != nil {
		return out
	}
	var names []string
	if json.Unmarshal(b, &names) != nil {
		return out
	}
	for _, n := range names {
		out[n] = true
	}
	return out
}

// mcpToggleFavorite flips a tool's favorite mark and says what it is now.
func mcpToggleFavorite(server, tool string) (bool, error) {
	favs := mcpLoadFavorites()
	key := server + "." + tool
	on := !favs[key]
	if on {
		favs[key] = true
	} else {
		delete(favs, key)
	}
	names := make([]string, 0, len(favs))
	for n := range favs {
		names = append(names, n)
	}
	sort.Strings(names)
	b, _ := json.MarshalIndent(names, "", "  ")
	p := mcpFavoritesPath()
	if p == "" {
		return false, fmt.Errorf("no config folder")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return false, err
	}
	return on, os.WriteFile(p, append(b, '\n'), 0o600)
}

// mcpToolBrowsable: it only reads (the server says so) and needs no argument.
func mcpToolBrowsable(t mcp.Tool) bool {
	if !t.ReadOnly() {
		return false
	}
	req, _ := t.InputSchema["required"].([]any)
	return len(req) == 0
}

// mcpServiceToolItems are the picker rows of a server's browsable tools,
// favorites first (pure: tests).
func mcpServiceToolItems(server string, tools []mcp.Tool, favs map[string]bool) []picker.Item {
	var fav, rest []picker.Item
	for _, t := range tools {
		if !mcpToolBrowsable(t) {
			continue
		}
		title, hint := t.Name, aiCleanText(t.Description)
		if f := favs[server+"."+t.Name]; f {
			fav = append(fav, picker.Item{ID: "tool-" + t.Name, Title: "★ " + title, Hint: hint})
		} else {
			rest = append(rest, picker.Item{ID: "tool-" + t.Name, Title: "  " + title, Hint: hint})
		}
	}
	return append(fav, rest...)
}

// openMCPService lists the servers of this chat's MCP profile.
func (m *Model) openMCPService() tea.Cmd {
	if !mcp.LoadSettings(mcpConfigPath()).Enabled {
		return m.aiInfo("MCP is off (mcp_enabled)")
	}
	mgr := m.aiMCP()
	prof := m.mcpActiveProfile()
	var items []picker.Item
	for _, st := range mgr.Servers() {
		if st.State == mcp.StateDisabled || (prof != nil && !prof.AllowsServer(st.Name)) {
			continue
		}
		items = append(items, picker.Item{ID: "srv-" + st.Name, Title: st.Name, Hint: st.Type})
	}
	return func() tea.Msg {
		return gitxDoneMsg{apply: func(m *Model) tea.Cmd {
			if len(items) == 0 {
				return m.aiInfo("No MCP service is connected: AI: MCP: Connect a Service…")
			}
			m.gitxPick(" Browse which service? ", items, func(m *Model, id string) tea.Cmd {
				return m.mcpServiceTools(strings.TrimPrefix(id, "srv-"))
			})
			return nil
		}}
	}
}

// mcpServiceTools lists a server's browsable tools (the server may start).
func (m *Model) mcpServiceTools(server string) tea.Cmd {
	mgr := m.aiMCP()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), aiMCPToolTimeout)
		defer cancel()
		var tools []mcp.Tool
		for _, st := range mgr.ToolsFor(ctx, nil, func(n string) bool { return n == server }) {
			tools = append(tools, st.Tools...)
		}
		return gitxDoneMsg{apply: func(m *Model) tea.Cmd {
			set := mcp.LoadSettings(mcpConfigPath())
			var shown []mcp.Tool
			for _, t := range tools {
				if !set.ToolOff(server, t.Name) {
					shown = append(shown, t)
				}
			}
			items := mcpServiceToolItems(server, shown, mcpLoadFavorites())
			if len(items) == 0 {
				return m.aiInfo(server + " has no list to browse: its tools need an argument or change things. Ask the AI, or use /tool " + server + ".<tool>")
			}
			m.gitxPick(" "+server+" (Enter: run or favorite) ", items, func(m *Model, id string) tea.Cmd {
				return m.mcpServiceToolActions(server, strings.TrimPrefix(id, "tool-"))
			})
			return nil
		}}
	}
}

// mcpServiceToolActions: run the tool, or flip its favorite mark.
func (m *Model) mcpServiceToolActions(server, tool string) tea.Cmd {
	favTitle := "Add to Favorites"
	if mcpLoadFavorites()[server+"."+tool] {
		favTitle = "Remove from Favorites"
	}
	m.gitxPick(" "+server+"."+tool+" ", []picker.Item{
		{ID: "run", Title: "Run", Hint: "reads only"},
		{ID: "favorite", Title: favTitle, Hint: "shown first next time"},
	}, func(m *Model, id string) tea.Cmd {
		if id == "favorite" {
			on, err := mcpToggleFavorite(server, tool)
			if err != nil {
				return m.gitxFail("Could not save the favorite", err)
			}
			if on {
				return m.aiInfo(server + "." + tool + " is a favorite")
			}
			return m.aiInfo(server + "." + tool + " is no longer a favorite")
		}
		return m.mcpServiceRun(server, tool)
	})
	return nil
}

// mcpServiceRun calls the tool and lists the lines of its result.
func (m *Model) mcpServiceRun(server, tool string) tea.Cmd {
	mgr := m.aiMCP()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), aiMCPToolTimeout)
		defer cancel()
		res := aiMCPToolRun(ctx, mgr, aiMCPToolCmd{server: server, tool: tool, args: map[string]any{}})
		return gitxDoneMsg{apply: func(m *Model) tea.Cmd {
			if res.err != nil {
				return m.aiMCPWarn("MCP tool not run", ai.Redact(res.err.Error()))
			}
			var lines []string
			for _, l := range strings.Split(res.text, "\n") {
				if l = strings.TrimSpace(l); l != "" {
					lines = append(lines, l)
				}
			}
			if len(lines) > 200 {
				lines = lines[:200]
			}
			items := make([]picker.Item, len(lines))
			for i, l := range lines {
				items[i] = picker.Item{ID: fmt.Sprintf("line-%d", i), Title: l}
			}
			if len(items) == 0 {
				return m.aiInfo(server + "." + tool + " returned nothing")
			}
			m.gitxPick(" "+server+"."+tool+" (Enter: send a line to the chat) ", items, func(m *Model, id string) tea.Cmd {
				var i int
				if _, err := fmt.Sscanf(id, "line-%d", &i); err != nil || i < 0 || i >= len(lines) {
					return nil
				}
				s := m.aiS()
				s.attachments = append(s.attachments, aiAttachment{Label: server + "." + tool + " item", Text: lines[i]})
				m.openAIPanel(true)
				return m.aiInfo("Attached the item to the AI chat: say what to do with it")
			})
			return nil
		}}
	}
}
