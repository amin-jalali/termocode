package app

import (
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/picker"
)

// ── MCP profiles (MCPX-27) ─────────────────────────────────────────────
//
// AI: MCP: Switch Profile… picks the named set of servers and tools this
// chat uses (mcp.Profile). "" is every server and tool.

const pickerKindMCPProfile pickerKindEnum = 2500

const (
	mcpProfileAllID  = "\x00all"
	mcpProfileEditID = "\x00edit"
)

func mcpProfilesUserPath() string {
	d, err := ai.ConfigDir()
	if err != nil {
		return ""
	}
	return mcp.ProfilesPath(d)
}

// mcpLoadProfiles reads the user's and the workspace's profiles.
func mcpLoadProfiles() ([]mcp.Profile, []string) {
	return mcp.LoadProfiles(mcpProfilesUserPath(), mcp.ProjectProfilesPath(aiWorkspaceRoot()))
}

// mcpActiveProfile is the chat's profile; nil: all (none set, or it was
// removed from the files).
func (m *Model) mcpActiveProfile() *mcp.Profile {
	name := m.aiS().mcpProfile
	if name == "" {
		return nil
	}
	ps, _ := mcpLoadProfiles()
	if p, ok := mcp.FindProfile(ps, name); ok {
		return &p
	}
	return nil
}

// openMCPProfiles opens the profile picker; the current one is marked.
func (m *Model) openMCPProfiles() tea.Cmd {
	ps, warns := mcpLoadProfiles()
	cur := m.aiS().mcpProfile
	mark := func(on bool) string {
		if on {
			return "● "
		}
		return "  "
	}
	items := []picker.Item{{ID: mcpProfileAllID, Title: mark(cur == "") + "All servers and tools", Hint: "no profile"}}
	for _, p := range ps {
		hint := p.Source + " · "
		if p.Description != "" {
			hint += p.Description + " · "
		}
		hint += p.Summary()
		items = append(items, picker.Item{ID: p.Name, Title: mark(cur == p.Name) + p.Name, Hint: hint})
	}
	items = append(items, picker.Item{ID: mcpProfileEditID, Title: "  Edit profiles file…", Hint: "named sets of servers and tools"})
	m.picker = picker.NewItems(" MCP profile for this chat (Enter: use) ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindMCPProfile
	if len(warns) > 0 {
		return m.aiInfo("MCP profiles: " + warns[0])
	}
	return nil
}

// onMCPProfilePicked applies a picker row.
func (m *Model) onMCPProfilePicked(id string) tea.Cmd {
	switch id {
	case mcpProfileAllID:
		m.aiS().mcpProfile = ""
		return m.aiInfo("MCP: this chat uses all servers and tools")
	case mcpProfileEditID:
		return m.editMCPProfiles()
	}
	m.aiS().mcpProfile = id
	return m.aiInfo("MCP profile " + id + ": this chat uses only its servers and tools")
}

// editMCPProfiles opens the user's profiles file, made with an example
// when missing.
func (m *Model) editMCPProfiles() tea.Cmd {
	p := mcpProfilesUserPath()
	if p == "" {
		return nil
	}
	if _, err := os.Stat(p); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return m.aiToastErr("MCP profiles", err)
		}
		example := "{\n  \"profiles\": {\n    \"example\": {\n      \"description\": \"read-only GitHub\",\n" +
			"      \"servers\": [\"github\"],\n      \"tools\": [\"github.get_*\", \"github.list_*\", \"github.search_*\"]\n    }\n  }\n}\n"
		if err := os.WriteFile(p, []byte(example), 0o644); err != nil {
			return m.aiToastErr("MCP profiles", err)
		}
	}
	m.openFileAtLine(p, 1)
	return m.aiInfo("A profile keeps only its servers and tools (globs, ! removes). Share one in .termocode/mcp-profiles.json")
}
