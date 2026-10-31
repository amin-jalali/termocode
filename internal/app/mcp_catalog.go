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

// ── MCP catalog (MCPX-16) ──────────────────────────────────────────────
//
// AI: MCP: Browse Catalog… lists servers of the official MCP Registry or
// mcp_catalog_url. The network is used only on "Load the catalog"; the
// list is kept on disk. Picking a server shows a Cancel-first card with
// the exact entry before it is added to the user's mcp.json.

const pickerKindMCPCatalog pickerKindEnum = 2501

const confirmKindMCPCatalogAdd confirmKindEnum = 2501

const mcpCatalogLoadID = "\x00load"

// mcpCatalogLoadedMsg ends a catalog fetch.
type mcpCatalogLoadedMsg struct {
	list []mcp.CatalogEntry
	err  error
}

func mcpCatalog() mcp.Catalog {
	return mcp.Catalog{URL: mcp.LoadSettings(mcpConfigPath()).CatalogURL, CacheDir: mcpCacheDir()}
}

// mcpCatalogTitle is a row's title: ✓ verified, ⚠ deprecated.
func mcpCatalogTitle(e mcp.CatalogEntry) string {
	badge := "  "
	switch {
	case e.Deprecated():
		badge = "⚠ "
	case e.Verified:
		badge = "✓ "
	}
	name := e.ShortName()
	if e.Title != "" && !strings.EqualFold(e.Title, name) {
		name += " (" + e.Title + ")"
	}
	return badge + name
}

func mcpCatalogHint(e mcp.CatalogEntry) string {
	parts := []string{}
	if e.Deprecated() {
		parts = append(parts, e.Status)
	}
	parts = append(parts, e.Publisher)
	if e.Version != "" {
		parts = append(parts, "v"+e.Version)
	}
	if e.Description != "" {
		parts = append(parts, e.Description)
	}
	return strings.Join(parts, " · ")
}

// openMCPCatalog shows the cached list (no network) and the Load row.
func (m *Model) openMCPCatalog() tea.Cmd {
	c := mcpCatalog()
	list, at, ok := c.Cached()
	if ok {
		m.aiS().mcpCatalog = list
	}
	load := picker.Item{ID: mcpCatalogLoadID, Title: "↻ Load the catalog from " + c.Host(), Hint: "uses the network"}
	if ok {
		load.Title = "↻ Load the catalog again"
		load.Hint = "last loaded " + at.Local().Format("2006-01-02 15:04")
	}
	items := []picker.Item{load}
	list = append([]mcp.CatalogEntry(nil), m.aiS().mcpCatalog...)
	sort.SliceStable(list, func(i, j int) bool { return list[i].Deprecated() != list[j].Deprecated() && !list[i].Deprecated() })
	for _, e := range list {
		items = append(items, picker.Item{ID: e.Name, Title: mcpCatalogTitle(e), Hint: mcpCatalogHint(e)})
	}
	m.picker = picker.NewItems(" MCP catalog · "+c.Host()+" (type to search, Enter: add) ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindMCPCatalog
	return nil
}

// loadMCPCatalog fetches the catalog in the background.
func loadMCPCatalog() tea.Cmd {
	c := mcpCatalog()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		list, err := c.Fetch(ctx)
		return mcpCatalogLoadedMsg{list: list, err: err}
	}
}

func (m *Model) onMCPCatalogLoaded(msg mcpCatalogLoadedMsg) tea.Cmd {
	if msg.err != nil {
		return m.aiToastErr("MCP catalog", msg.err)
	}
	m.aiS().mcpCatalog = msg.list
	m.openMCPCatalog()
	return m.aiInfo(fmt.Sprintf("MCP catalog: %d servers", len(msg.list)))
}

func (m *Model) onMCPCatalogPicked(id string) tea.Cmd {
	if id == mcpCatalogLoadID {
		m.pickerOpen = false
		return tea.Batch(m.aiInfo("Loading the MCP catalog…"), loadMCPCatalog())
	}
	for _, e := range m.aiS().mcpCatalog {
		if e.Name == id {
			return m.openMCPCatalogAdd(e)
		}
	}
	return nil
}

// mcpEntryText is an entry as people read it: the command line or the URL.
func mcpEntryText(entry map[string]any) string {
	if u, ok := entry["url"].(string); ok {
		return u
	}
	cmd, _ := entry["command"].(string)
	args, _ := entry["args"].([]string)
	return strings.TrimSpace(cmd + " " + strings.Join(args, " "))
}

// openMCPCatalogAdd is the Cancel-first card before a catalog server is
// added.
func (m *Model) openMCPCatalogAdd(e mcp.CatalogEntry) tea.Cmd {
	name, entry, err := e.ServerEntry()
	if err != nil {
		return m.aiToastErr("MCP catalog", err)
	}
	m.aiS().mcpCatalogSel = e.Name
	var b strings.Builder
	if e.Description != "" {
		b.WriteString(e.Description + "\n\n")
	}
	pub := e.Publisher + " (not verified)"
	if e.Verified {
		pub = e.Publisher + " ✓ verified by the MCP Registry"
	}
	fmt.Fprintf(&b, "Publisher: %s\nVersion: %s (pinned)\n", pub, e.Version)
	if e.Deprecated() {
		fmt.Fprintf(&b, "⚠ The publisher marked it %s.\n", e.Status)
	}
	if s := e.Secrets(); len(s) > 0 {
		fmt.Fprintf(&b, "Secrets it needs: %s (set them in its server actions; they go to the secret store).\n", strings.Join(s, ", "))
	}
	b.WriteString("It is added to your mcp.json as \"" + name + "\" and runs only when the AI first calls it.")
	m.confirm = confirm.Risky("Add MCP server "+name+"?", b.String(),
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "add", Title: "Add server", Style: confirm.StyleDestructive}).
		WithCode(mcpEntryText(entry), "This runs on your computer when the AI uses it.")
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindMCPCatalogAdd
	return nil
}

// answerMCPCatalogAdd runs the card's button (Esc is Cancel).
func (m *Model) answerMCPCatalogAdd(id string) tea.Cmd {
	sel := m.aiS().mcpCatalogSel
	m.aiS().mcpCatalogSel = ""
	if id != "add" || sel == "" {
		return nil
	}
	for _, e := range m.aiS().mcpCatalog {
		if e.Name != sel {
			continue
		}
		name, entry, err := e.ServerEntry()
		if err == nil {
			err = mcp.AddServer(mcpUserPath(), name, entry)
		}
		if err != nil {
			return m.aiToastErr("MCP catalog", err)
		}
		m.aiMCP().Reload()
		if len(e.Secrets()) > 0 {
			m.openMCPServerActions(name) // its secret rows say "not set"
			return m.aiInfo("Added MCP " + name + ". Set its secrets here")
		}
		return m.aiInfo("Added MCP " + name + ". It starts on the AI's first call")
	}
	return nil
}
