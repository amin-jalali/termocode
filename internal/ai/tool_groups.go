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

// Ability states of a tool group (ai_abilities): on (its tools are in
// every request), on demand (deferred: the AI loads them with
// tool_search, the default) and off (the AI cannot load them; it asks the
// user with ability_request).
const (
	AbilityOn     = "on"
	AbilityDemand = "demand"
	AbilityOff    = "off"
)

// AbilityStates are the states ai_abilities may hold.
var AbilityStates = []string{AbilityOn, AbilityDemand, AbilityOff}

// AbilityRequestTool is the tool the AI asks with to turn an off group on.
const AbilityRequestTool = "ability_request"

// GroupPolicy says what each group's ability state is and how an off
// group is turned on. A nil State is "on demand" for every group.
type GroupPolicy struct {
	State func(group string) string
	// Enable turns group on (on demand) after the user allowed it on the
	// ability_request card. Nil: no ability_request tool.
	Enable func(ctx context.Context, group string) error
}

func (p GroupPolicy) state(g string) string {
	if p.State == nil {
		return AbilityDemand
	}
	return p.State(g)
}

// DeferToolGroups hides every tool of tools that is not in core (and not
// an MCP tool: those have their own search) until its group is loaded,
// and adds tool_search. With nothing to hide it returns tools unchanged.
func DeferToolGroups(tools []Tool, core []string, groups []ToolGroup, loaded *LoadedGroups) []Tool {
	return DeferToolGroupsWith(tools, core, groups, loaded, GroupPolicy{})
}

// DeferToolGroupsWith is DeferToolGroups with ability states: an "on"
// group is loaded at once, an "off" group's tools are never sent and do
// not run (tool_search and a call by name say to ask the user), and
// ability_request asks the user to turn an off group on.
func DeferToolGroupsWith(tools []Tool, core []string, groups []ToolGroup, loaded *LoadedGroups, pol GroupPolicy) []Tool {
	byName := map[string]*deferredGroup{}
	var order []*deferredGroup
	out := make([]Tool, 0, len(tools)+2)
	off := func(g string) bool { return pol.state(g) == AbilityOff }
	for _, t := range tools {
		if slices.Contains(core, t.Name) || strings.HasPrefix(t.Name, "mcp__") || t.Name == "mcp_tool_search" || t.Name == ToolSearchTool || t.Name == AbilityRequestTool || t.Hidden != nil {
			out = append(out, t)
			continue
		}
		g := ToolGroupOf(groups, t.Name)
		if pol.state(g) == AbilityOn {
			loaded.Load(g)
		}
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
		t.Hidden = func() bool { return off(g) || !loaded.Has(g) }
		if run := t.Run; run != nil {
			t.Run = func(ctx context.Context, args map[string]any) (string, error) {
				if off(g) {
					return "", fmt.Errorf("the %s tools are off (the user's ai_abilities); call %s with group %q and why, and the user decides", g, AbilityRequestTool, g)
				}
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
	var offNames []string
	for _, mb := range order {
		if off(mb.name) {
			offNames = append(offNames, mb.name)
			continue
		}
		fmt.Fprintf(&list, "\n- %s (%d): %s", mb.name, len(mb.tools), mb.about)
	}
	if len(offNames) > 0 && pol.Enable != nil {
		fmt.Fprintf(&list, "\nOff (the user turned these off; to use one, call %s and the user decides): %s", AbilityRequestTool, strings.Join(offNames, ", "))
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
				if off(mb.name) {
					fmt.Fprintf(&b, "The %s group is off. To use it, call %s with group %q and why; the user decides.\n", mb.name, AbilityRequestTool, mb.name)
					continue
				}
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
	out = append(out, search)
	if len(offNames) > 0 && pol.Enable != nil {
		out = append(out, abilityRequestTool(byName, loaded, pol))
	}
	return out
}

// abilityRequestTool asks the user to turn an off group on. It is a
// mutating tool with no class: every call shows the Deny-first card and
// there is no "Always allow".
func abilityRequestTool(byName map[string]*deferredGroup, loaded *LoadedGroups, pol GroupPolicy) Tool {
	return Tool{
		Name: AbilityRequestTool,
		Description: "Ask the user to turn on a tool group that is off (listed as off in tool_search). " +
			"The user sees a card with your reason and decides. Use it only when the task needs that group.",
		Params: map[string]any{
			"group": map[string]any{"type": "string", "description": "the off group's name"},
			"why":   map[string]any{"type": "string", "description": "one short sentence: why the task needs it"},
		},
		Required: []string{"group", "why"},
		Mutating: true,
		Describe: func(args map[string]any) string {
			g, _ := args["group"].(string)
			why, _ := args["why"].(string)
			d := "turn on the AI's " + g + " tools"
			if mb := byName[g]; mb != nil && mb.about != "" {
				d += " (" + mb.about + ")"
			}
			if why != "" {
				d += ": " + why
			}
			return d
		},
		Validate: func(args map[string]any) error {
			g, _ := args["group"].(string)
			if byName[g] == nil {
				return fmt.Errorf("no tool group %q", g)
			}
			if pol.state(g) != AbilityOff {
				return fmt.Errorf("the %s group is not off: load it with tool_search", g)
			}
			return nil
		},
		Run: func(ctx context.Context, args map[string]any) (string, error) {
			g, _ := args["group"].(string)
			if err := pol.Enable(ctx, g); err != nil {
				return "", err
			}
			loaded.Load(g)
			var b strings.Builder
			fmt.Fprintf(&b, "The user turned on the %s group. Its tools, from your next step:\n", g)
			for _, t := range byName[g].tools {
				fmt.Fprintf(&b, "- %s: %s\n", t.Name, firstSentence(t.Description))
			}
			return b.String(), nil
		},
	}
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
