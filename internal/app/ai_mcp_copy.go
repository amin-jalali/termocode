package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// ── Copy for a teammate (MCPX-28) ──────────────────────────────────────

// mcpCopyForTeammate copies one entry as an mcpServers block: secrets as
// ${secret:NAME}, never their values (mcp.TeammateJSON; a value the
// secret store knows counts as a secret too).
func (m *Model) mcpCopyForTeammate(name string) tea.Cmd {
	entries := mcpServerEntries(name)
	if len(entries) == 0 {
		return nil
	}
	out, names, err := mcp.TeammateJSON(entries[0], func(v string) bool { return ai.Redact(v) != v })
	if err != nil {
		return m.aiToastErr("MCP "+name, err)
	}
	done := "MCP " + name + " copied for a teammate (no secret values)"
	if len(names) > 0 {
		done += "; they set " + strings.Join(names, ", ")
	}
	return m.copyWithToast(out, done)
}
