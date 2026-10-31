package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Tool search / deferred tool loading (MCPX-17). With many servers the
// full tool schemas fill the model's context before the user says a
// word. Deferred, every MCP tool is hidden from the specs and one search
// tool stands in for them: it finds tools by keyword, returns their full
// schema and loads them, so the model can call them from the next step.

// Tool search modes (mcp_tool_search).
const (
	ToolSearchAuto = "auto" // defer when the MCP schemas pass ToolSearchAutoTokens
	ToolSearchOn   = "on"
	ToolSearchOff  = "off"
)

// ToolSearchName is the stand-in tool's wire name.
const ToolSearchName = "mcp_tool_search"

// ToolSearchAutoTokens is the auto threshold: the estimated tokens of
// all MCP tool specs (about 4 bytes of JSON per token).
const ToolSearchAutoTokens = 10000

// toolSearchMax is the most tools one search returns.
const toolSearchMax = 8

// ParseToolSearchMode reads mcp_tool_search; unknown values are auto.
func ParseToolSearchMode(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case ToolSearchOn:
		return ToolSearchOn
	case ToolSearchOff:
		return ToolSearchOff
	}
	return ToolSearchAuto
}

// SpecTokens estimates the tokens of tools' specs.
func SpecTokens(tools []ai.Tool) int {
	n := 0
	for _, t := range tools {
		b, _ := json.Marshal(t.Spec())
		n += len(b)
	}
	return n / 4
}

// DeferTools applies mode to a turn's tools: MCP tools (WirePrefix) are
// hidden until the search tool loads them. Off, or auto under the
// threshold, it returns tools unchanged.
func DeferTools(tools []ai.Tool, mode string) []ai.Tool {
	var mcpTools []ai.Tool
	for _, t := range tools {
		if strings.HasPrefix(t.Name, WirePrefix) {
			mcpTools = append(mcpTools, t)
		}
	}
	switch {
	case len(mcpTools) == 0, mode == ToolSearchOff:
		return tools
	case mode != ToolSearchOn && SpecTokens(mcpTools) <= ToolSearchAutoTokens:
		return tools
	}
	var mu sync.Mutex
	loaded := map[string]bool{}
	out := make([]ai.Tool, 0, len(tools)+1)
	perServer := map[string]int{}
	for _, t := range tools {
		if strings.HasPrefix(t.Name, WirePrefix) {
			name := t.Name
			t.Hidden = func() bool { mu.Lock(); defer mu.Unlock(); return !loaded[name] }
			perServer[serverOfDisplay(t.Display)]++
		}
		out = append(out, t)
	}
	var servers []string
	for s, n := range perServer {
		servers = append(servers, fmt.Sprintf("%s (%d)", s, n))
	}
	sort.Strings(servers)
	search := ai.Tool{
		Name: ToolSearchName,
		Description: fmt.Sprintf("%d MCP tools are not listed to save space. Servers: %s. "+
			"Search them by keywords (or \"select:<name>,<name>\"); the matches come back with their full input schema "+
			"and you can call them by name from your next step.", len(mcpTools), strings.Join(servers, ", ")),
		Params: map[string]any{
			"query": map[string]any{"type": "string", "description": "keywords, or select:<tool name>,<tool name>"},
		},
		Required: []string{"query"},
		Describe: func(args map[string]any) string {
			q, _ := args["query"].(string)
			return "search MCP tools " + fmt.Sprintf("%q", q)
		},
		Run: func(_ context.Context, args map[string]any) (string, error) {
			q, _ := args["query"].(string)
			found := searchTools(mcpTools, q)
			if len(found) == 0 {
				return "No MCP tool matches " + fmt.Sprintf("%q", q) + ". Try other words.", nil
			}
			var b strings.Builder
			mu.Lock()
			for _, t := range found {
				loaded[t.Name] = true
			}
			mu.Unlock()
			for _, t := range found {
				schema, _ := json.Marshal(t.Spec().Schema)
				fmt.Fprintf(&b, "%s\n  %s\n  input schema: %s\n", t.Name, t.Description, schema)
			}
			b.WriteString("These tools are loaded: call them by name.")
			return b.String(), nil
		},
	}
	return append(out, search)
}

// serverOfDisplay reads the server of "mcp.<server>.<tool>".
func serverOfDisplay(d string) string {
	d = strings.TrimPrefix(d, "mcp.")
	if i := strings.LastIndex(d, "."); i > 0 {
		return d[:i]
	}
	return d
}

// searchTools ranks tools by query words found in the name and
// description; "select:a,b" picks exact wire or display names.
func searchTools(tools []ai.Tool, q string) []ai.Tool {
	q = strings.TrimSpace(q)
	if rest, ok := strings.CutPrefix(q, "select:"); ok {
		want := map[string]bool{}
		for _, n := range strings.Split(rest, ",") {
			want[strings.TrimSpace(n)] = true
		}
		var out []ai.Tool
		for _, t := range tools {
			if want[t.Name] || want[t.Display] {
				out = append(out, t)
			}
		}
		return out
	}
	words := strings.Fields(strings.ToLower(q))
	type hit struct {
		t     ai.Tool
		score int
	}
	var hits []hit
	for _, t := range tools {
		name := strings.ToLower(t.Name + " " + t.Display)
		desc := strings.ToLower(t.Description)
		score := 0
		for _, w := range words {
			if strings.Contains(name, w) {
				score += 3
			} else if strings.Contains(desc, w) {
				score++
			}
		}
		if score > 0 {
			hits = append(hits, hit{t, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	var out []ai.Tool
	for i, h := range hits {
		if i == toolSearchMax {
			break
		}
		out = append(out, h.t)
	}
	return out
}
