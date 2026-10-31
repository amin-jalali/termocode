package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// ── Server details (MCPX-4) ────────────────────────────────────────────
//
// A server's screen (AI: MCP Servers… → a server) shows its details
// above its actions: the file, transport, command or URL, env and header
// NAMES (never values), what initialize said, tools, state and the last
// error.

// mcpDetailLines are the detail lines of a server's screen.
func mcpDetailLines(m *Model, name string) []string {
	var st mcp.Status
	for _, x := range m.aiMCP().Servers() {
		if x.Name == name {
			st = x
		}
	}
	var lines []string
	entries := mcpServerEntries(name)
	if len(entries) > 0 {
		e := entries[0]
		from := "yours"
		if e.Source == mcp.SourceProject {
			from = "this project"
		}
		lines = append(lines, "File: "+e.SourcePath+" ("+from+")")
		if e.Type == mcp.TypeStdio {
			lines = append(lines, "Transport: local (stdio)",
				"Command: "+strings.TrimSpace(e.Command+" "+strings.Join(e.Args, " ")))
		} else {
			lines = append(lines, "Transport: remote ("+e.Type+")", "URL: "+e.URL)
		}
		if k := mcpSortedKeys(e.Env); k != "" {
			lines = append(lines, "Env: "+k)
		}
		if k := mcpSortedKeys(e.Headers); k != "" {
			lines = append(lines, "Headers: "+k)
		}
	}
	if info, proto, ok := m.aiMCP().Info(name); ok {
		l := "Server: " + strings.TrimSpace(mcp.CleanAccount(info.Name)+" "+mcp.CleanAccount(info.Version))
		if proto != "" {
			l += " · MCP " + proto
		}
		lines = append(lines, l)
	} else {
		lines = append(lines, "Server: not connected yet")
	}
	tools := "Tools: " + mcpToolsHint(m, name)
	if n := mcpServerTokens(m, name, mcp.LoadSettings(mcpConfigPath())); n > 0 {
		tools += " · " + mcpTokensText(n) + " per message"
	}
	lines = append(lines, tools)
	state := st.State
	if st.Pid > 0 {
		state += fmt.Sprintf(" · pid %d", st.Pid)
	}
	if st.Trusted {
		state += " · trusted"
	} else {
		state += " · not trusted"
	}
	lines = append(lines, "State: "+state)
	if st.Problem != "" {
		lines = append(lines, "Last error: "+st.Problem)
	}
	for i, l := range lines {
		lines[i] = ai.Redact(aiCleanText(l))
	}
	return lines
}

// mcpSortedKeys is "A, B" of a map's keys (never its values).
func mcpSortedKeys(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
