package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Outgoing secret scan (AU-28): an MCP call's arguments are checked for
// the secrets termocode knows (stored API keys, ${secret:} values, a
// .env password it read) and for token-like values (ghp_…, sk-…,
// Bearer …) before the call is sent. A hit makes the call ask, with no
// "Always allow"; the card names the argument, never the value.

// ScanSecrets lists the argument paths ("opts.list[1]") that hold a
// secret, sorted; nil when none does.
func ScanSecrets(args map[string]any) []string {
	var hits []string
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case string:
			if ai.RedactSecrets(x) != x || ai.Redact(x) != x {
				hits = append(hits, path)
			}
		case map[string]any:
			for k, e := range x {
				p := k
				if path != "" {
					p = path + "." + k
				}
				walk(p, e)
			}
		case []any:
			for i, e := range x {
				walk(fmt.Sprintf("%s[%d]", path, i), e)
			}
		}
	}
	walk("", args)
	sort.Strings(hits)
	return hits
}

// secretNote is the card's warning for hits.
func secretNote(hits []string) string {
	return "⚠ secret in " + strings.Join(hits, ", ") + ": Deny unless you mean to send it to the server"
}
