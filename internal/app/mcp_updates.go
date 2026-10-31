package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/picker"
)

// ── MCP server versions (MCPX-32) ──────────────────────────────────────
//
// Servers added from the catalog are pinned. AI: MCP: Check for Updates
// reads what changed in the catalog (network, on request); with
// mcp_auto_update notify / on the servers list does it once a day. The
// list and the server actions show a newer version or a deprecated
// flag; Update asks on a Cancel-first card. "on" updates the user's
// pinned servers by itself and says so.

const confirmKindMCPUpdate confirmKindEnum = 2503

// mcpAutoCheckEvery is how often notify / on check.
const mcpAutoCheckEvery = 24 * time.Hour

// mcpUpdatesMsg ends a check.
type mcpUpdatesMsg struct {
	auto bool
	err  error
}

// mcpKnownUpdates compares the configured servers with the cached
// catalog (never the network).
func mcpKnownUpdates() map[string]mcp.Update {
	list, _, ok := mcpCatalog().Cached()
	if !ok {
		return nil
	}
	servers, _ := mcp.Load(mcpUserPath(), mcp.ProjectPath(aiWorkspaceRoot()))
	return mcp.FindUpdates(servers, list)
}

// mcpUpdateHint starts the servers list's hint for a server ("" none),
// so the note is seen before the row is cut.
func mcpUpdateHint(ups map[string]mcp.Update, name string) string {
	u, ok := ups[name]
	if !ok {
		return ""
	}
	var parts []string
	if u.Newer() {
		parts = append(parts, "update "+u.Latest)
	}
	if u.Deprecated {
		parts = append(parts, "⚠ "+u.Status)
	}
	return strings.Join(parts, " · ") + " · "
}

// mcpUpdateItems is the server action for an update.
func mcpUpdateItems(name string) []picker.Item {
	u, ok := mcpKnownUpdates()[name]
	if !ok {
		return nil
	}
	hint := "pinned " + u.Current
	if u.Deprecated {
		hint += " · ⚠ the publisher marked it " + u.Status
	}
	if !u.Newer() {
		return []picker.Item{{ID: "update-none", Title: "No newer version", Hint: hint}}
	}
	return []picker.Item{{ID: "update", Title: "Update to " + u.Latest + "…", Hint: hint}}
}

// checkMCPUpdates reads the catalog's changes in the background.
func checkMCPUpdates(auto bool) tea.Cmd {
	c := mcpCatalog()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var err error
		if _, at, ok := c.Cached(); ok {
			_, err = c.FetchSince(ctx, at)
		} else {
			_, err = c.Fetch(ctx)
		}
		return mcpUpdatesMsg{auto: auto, err: err}
	}
}

// mcpAutoCheck starts the daily check when mcp_auto_update asks for it.
func mcpAutoCheck() tea.Cmd {
	if mcp.LoadSettings(mcpConfigPath()).AutoUpdate == mcp.AutoUpdateOff {
		return nil
	}
	if _, at, ok := mcpCatalog().Cached(); ok && time.Since(at) < mcpAutoCheckEvery {
		return nil
	}
	return checkMCPUpdates(true)
}

func (m *Model) onMCPUpdates(msg mcpUpdatesMsg) tea.Cmd {
	if msg.err != nil {
		if msg.auto {
			return nil // a daily check that fails stays quiet
		}
		return m.aiToastErr("MCP updates", msg.err)
	}
	ups := mcpKnownUpdates()
	names := make([]string, 0, len(ups))
	for n := range ups {
		names = append(names, n)
	}
	sort.Strings(names)
	var newer, flagged, applied []string
	auto := mcp.LoadSettings(mcpConfigPath()).AutoUpdate == mcp.AutoUpdateOn
	servers := mcpServersByName()
	for _, n := range names {
		u := ups[n]
		if u.Deprecated {
			flagged = append(flagged, n+" ("+u.Status+")")
		}
		if !u.Newer() {
			continue
		}
		if auto && servers[n].Source == mcp.SourceUser && mcp.ApplyUpdate(mcpUserPath(), n, u.Entry) == nil {
			applied = append(applied, n+" "+u.Latest)
			continue
		}
		newer = append(newer, n+" "+u.Latest)
	}
	if len(applied) > 0 {
		m.aiMCP().Reload()
	}
	var parts []string
	if len(applied) > 0 {
		parts = append(parts, "updated "+strings.Join(applied, ", "))
	}
	if len(newer) > 0 {
		parts = append(parts, fmt.Sprintf("%d update(s): %s (server actions → Update)", len(newer), strings.Join(newer, ", ")))
	}
	if len(flagged) > 0 {
		parts = append(parts, "deprecated: "+strings.Join(flagged, ", "))
	}
	if len(parts) == 0 {
		if msg.auto {
			return nil
		}
		return m.aiInfo("MCP: every catalog server is up to date")
	}
	return m.aiInfo("MCP " + strings.Join(parts, "; "))
}

func mcpServersByName() map[string]mcp.Server {
	servers, _ := mcp.Load(mcpUserPath(), mcp.ProjectPath(aiWorkspaceRoot()))
	out := map[string]mcp.Server{}
	for _, s := range servers {
		out[s.Name] = s
	}
	return out
}

// onMCPUpdateAction runs the update server actions. ok=false: not ours.
func (m *Model) onMCPUpdateAction(id, name string) (tea.Cmd, bool) {
	switch id {
	case "update-none":
		return nil, true
	case "update":
	default:
		return nil, false
	}
	u, ok := mcpKnownUpdates()[name]
	if !ok || !u.Newer() {
		return nil, true
	}
	if mcpServersByName()[name].Source != mcp.SourceUser {
		return m.aiInfo("MCP " + name + " is in the project file: change its version there"), true
	}
	_, entry, err := u.Entry.ServerEntry()
	if err != nil {
		return m.aiToastErr("MCP update", err), true
	}
	msg := fmt.Sprintf("From %s to %s. Your env values and other settings stay.", u.Current, u.Latest)
	if u.Deprecated {
		msg += "\n⚠ The publisher marked it " + u.Status + "."
	}
	m.confirm = confirm.Risky("Update MCP "+name+" to "+u.Latest+"?", msg,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "update", Title: "Update", Style: confirm.StyleDestructive}).
		WithCode(mcpEntryText(entry), "The new version runs on your computer on the next tool call.")
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindMCPUpdate
	return nil, true
}

// answerMCPUpdate runs the card's button (Esc is Cancel).
func (m *Model) answerMCPUpdate(id string) tea.Cmd {
	name := m.aiS().mcpSel
	if id != "update" || name == "" {
		return nil
	}
	u, ok := mcpKnownUpdates()[name]
	if !ok {
		return nil
	}
	if err := mcp.ApplyUpdate(mcpUserPath(), name, u.Entry); err != nil {
		return m.aiToastErr("MCP update", err)
	}
	m.aiMCP().Stop(name) // the next call starts the new version
	m.aiMCP().Reload()
	return m.aiInfo("MCP " + name + ": updated to " + u.Latest)
}
