package ai

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
)

// Tool groups (lean request). Every tool schema is paid on every request
// of a turn, and a chat has well over a hundred tools. With groups on, a
// request sends a small core set; the rest are hidden in named groups.
// One stand-in tool, tool_search, loads a group by name or by keywords,
// and a loaded group stays loaded for the chat. A hidden tool the model
// calls by name still runs and loads its group.
//
// config.json "ai_tool_groups": "on" (default) or "off" (every tool on
// every request, as before).

// Values of ai_tool_groups.
const (
	ToolGroupsOn  = "on"
	ToolGroupsOff = "off"
)

// ToolGroupsEnabled reads ai_tool_groups: only "off" turns it off.
func ToolGroupsEnabled(v string) bool {
	return strings.ToLower(strings.TrimSpace(v)) != ToolGroupsOff
}

// ToolSearchTool is the stand-in tool's name.
const ToolSearchTool = "tool_search"

// ToolGroupOther is the group of a tool no group names.
const ToolGroupOther = "more"

// toolGroupHits is the most groups one keyword search loads.
const toolGroupHits = 3

// ToolGroup is one named group of deferred tools.
type ToolGroup struct {
	Name  string
	About string   // what the group is for, one short phrase
	Names []string // tool names; a name ending in "_" is a prefix
}

// matches reports whether the group names tool name.
func (g ToolGroup) matches(name string) bool {
	for _, p := range g.Names {
		if strings.HasSuffix(p, "_") && strings.HasPrefix(name, p) || name == p {
			return true
		}
	}
	return false
}

// LoadedGroups is the set of tool groups a chat loaded. It is safe for
// concurrent use (the agent's goroutine loads, the next turn reads).
type LoadedGroups struct {
	mu sync.Mutex
	m  map[string]bool
}

// Load marks groups loaded.
func (l *LoadedGroups) Load(groups ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.m == nil {
		l.m = map[string]bool{}
	}
	for _, g := range groups {
		l.m[g] = true
	}
}

// Has reports whether group g is loaded.
func (l *LoadedGroups) Has(g string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.m[g]
}

// List is the loaded groups, sorted.
func (l *LoadedGroups) List() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.m))
	for g := range l.m {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

// ToolGroupOf is the group of tool name: the first of groups that names
// it, else ToolGroupOther.
func ToolGroupOf(groups []ToolGroup, name string) string {
	for _, g := range groups {
		if g.matches(name) {
			return g.Name
		}
	}
	return ToolGroupOther
}

// DeferToolGroups hides every tool of tools that is not in core (and not
// an MCP tool: those have their own search) until its group is loaded,
// and adds tool_search. With nothing to hide it returns tools unchanged.
func DeferToolGroups(tools []Tool, core []string, groups []ToolGroup, loaded *LoadedGroups) []Tool {
	byName := map[string]*deferredGroup{}
	var order []*deferredGroup
	out := make([]Tool, 0, len(tools)+1)
	for _, t := range tools {
		if slices.Contains(core, t.Name) || strings.HasPrefix(t.Name, "mcp__") || t.Name == "mcp_tool_search" || t.Name == ToolSearchTool || t.Hidden != nil {
			out = append(out, t)
			continue
		}
		g := ToolGroupOf(groups, t.Name)
		mb := byName[g]
		if mb == nil {
			mb = &deferredGroup{name: g}
			for _, gr := range groups {
				if gr.Name == g {
					mb.about = gr.About
				}
			}
			if g == ToolGroupOther {
				mb.about = "everything else"
			}
			byName[g] = mb
			order = append(order, mb)
		}
		mb.tools = append(mb.tools, t)
		t.Hidden = func() bool { return !loaded.Has(g) }
		if run := t.Run; run != nil {
			t.Run = func(ctx context.Context, args map[string]any) (string, error) {
				loaded.Load(g) // called by name: its group comes along from the next step
				return run(ctx, args)
			}
		}
		out = append(out, t)
	}
	if len(order) == 0 {
		return tools
	}
	// The groups in a fixed order (the order of groups, "more" last), so
	// the description is the same on every request of the chat.
	rank := func(name string) int {
		for i, g := range groups {
			if g.Name == name {
				return i
			}
		}
		return len(groups)
	}
	sort.SliceStable(order, func(i, j int) bool { return rank(order[i].name) < rank(order[j].name) })
	var list strings.Builder
	for _, mb := range order {
		fmt.Fprintf(&list, "\n- %s (%d): %s", mb.name, len(mb.tools), mb.about)
	}
	search := Tool{
		Name: ToolSearchTool,
		Description: "Your tool list holds only the core tools, to keep each request small. More tools wait in these groups:" +
			list.String() + "\nCall tool_search with group names (\"db\" or \"db,containers\") or with keywords, before you need a tool " +
			"that is not in your list. The matching groups load and stay for this chat: call their tools from your next step. " +
			"A tool the prompt names can also be called by its name at once.",
		Params: map[string]any{
			"query": map[string]any{"type": "string", "description": "group names (comma separated) or keywords"},
		},
		Required: []string{"query"},
		ReadOnly: true,
		Describe: func(args map[string]any) string {
			q, _ := args["query"].(string)
			return "load tools " + fmt.Sprintf("%q", q)
		},
		Run: func(_ context.Context, args map[string]any) (string, error) {
			q, _ := args["query"].(string)
			var hit []*deferredGroup
			for _, w := range strings.FieldsFunc(strings.ToLower(q), func(r rune) bool { return r == ',' || r == ' ' }) {
				if mb := byName[w]; mb != nil && !slices.Contains(hit, mb) {
					hit = append(hit, mb)
				}
			}
			if len(hit) == 0 {
				hit = searchGroups(order, q)
			}
			if len(hit) == 0 {
				return "No tool group matches " + fmt.Sprintf("%q", q) + ". Groups: " + groupNames(order) + ".", nil
			}
			var b strings.Builder
			for _, mb := range hit {
				loaded.Load(mb.name)
				fmt.Fprintf(&b, "Loaded %s:\n", mb.name)
				for _, t := range mb.tools {
					fmt.Fprintf(&b, "- %s: %s\n", t.Name, firstSentence(t.Description))
				}
			}
			b.WriteString("Call these tools by name from your next step.")
			return b.String(), nil
		},
	}
	return append(out, search)
}

// searchGroups ranks the groups by query words found in their tools'
// names (3 points) and descriptions (1) and returns the best few.
func searchGroups(groups []*deferredGroup, q string) []*deferredGroup {
	words := strings.Fields(strings.ToLower(q))
	type hit struct {
		g     *deferredGroup
		score int
	}
	var hits []hit
	for _, g := range groups {
		score := 0
		for _, t := range g.tools {
			name, desc := strings.ToLower(t.Name), strings.ToLower(t.Description)
			for _, w := range words {
				if strings.Contains(name, w) {
					score += 3
				} else if strings.Contains(desc, w) {
					score++
				}
			}
		}
		if score > 0 {
			hits = append(hits, hit{g, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	var out []*deferredGroup
	for i, h := range hits {
		if i == toolGroupHits {
			break
		}
		out = append(out, h.g)
	}
	return out
}

// deferredGroup is one group's hidden tools.
type deferredGroup struct {
	name, about string
	tools       []Tool
}

// groupNames is "db, git, …" of groups.
func groupNames(groups []*deferredGroup) string {
	names := make([]string, len(groups))
	for i, g := range groups {
		names[i] = g.name
	}
	return strings.Join(names, ", ")
}

// firstSentence is s up to its first ". " (all of s when it has none),
// on one line.
func firstSentence(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if i := strings.Index(s, ". "); i > 0 {
		return s[:i+1]
	}
	return s
}
