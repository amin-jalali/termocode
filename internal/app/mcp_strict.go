package app

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/picker"
)

// Strict mode (MCPX-33, CF-27): with mcp_strict on, the servers list
// has a warning row under each server whose entry has keys termocode
// does not read (a typo, another app's key). Enter on it opens the
// server, where Edit in mcp.json is.

// mcpWarnPrefix starts a warning row's id; the rest is the server name.
const mcpWarnPrefix = "warn:"

// mcpStrictRow is the warning row of st (ok false: none).
func mcpStrictRow(set mcp.Settings, st mcp.Status) (picker.Item, bool) {
	if !set.Strict || len(st.UnknownKeys) == 0 {
		return picker.Item{}, false
	}
	word := "unknown key: "
	if len(st.UnknownKeys) > 1 {
		word = "unknown keys: "
	}
	return picker.Item{ID: mcpWarnPrefix + st.Name, Title: "  ⚠ " + word + strings.Join(st.UnknownKeys, ", "),
		Hint: "termocode does not read them: check the spelling in " + st.Source + " mcp.json"}, true
}

// mcpServerOfRow is the server a servers list row is about.
func mcpServerOfRow(id string) string { return strings.TrimPrefix(id, mcpWarnPrefix) }
