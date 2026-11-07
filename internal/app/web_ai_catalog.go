package app

import (
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// "What can I do?" on the phone (MOB-13): ai.catalog is the AI's tools
// grouped into features people know (Git, Databases, Containers, …),
// each with prompts to try and the view it opens, and every MCP server
// with the tools it last listed. Only cached lists are read: no server
// starts, so a viewer may ask. The servers' names and descriptions are
// data from outside: cleaned and redacted.

func init() {
	webOps[webproto.OpAICatalog] = (*Model).webAICatalog
}

// catalogFeature is one row of the built-in catalog. Tools are the tool
// names it covers: an entry ending in "_" is a prefix.
type catalogFeature struct {
	id, title, hint, view string
	tools                 []string
	try                   []string
}

// catalogFeatures is the order the phone lists them. A tool no row
// covers counts under "more", so a new tool group is never lost.
var catalogFeatures = []catalogFeature{
	{id: "files", title: "Files and search", hint: "Read, find and change files in this project",
		tools: []string{"read_file", "list_dir", "search", "glob", "write_file", "edit_file", "create_file", "create_dir", "delete", "rename", "ast_grep", "look_at"},
		try:   []string{"Where is the login handled?", "Find every TODO in the project"}},
	{id: "git", title: "Git", hint: "Status, diffs, history, commits and branches", view: "git.status",
		tools: []string{"git_"},
		try:   []string{"Show git status", "What changed since yesterday?", "Write a commit message for my changes"}},
	{id: "code", title: "Code and editor", hint: "Errors, symbols, rename, format, open files on the laptop",
		tools: []string{"lsp_", "diagnostics", "format", "open_file", "get_editor_state", "reveal_in_explorer"},
		try:   []string{"What errors does the project have?", "Rename this function everywhere"}},
	{id: "terminal", title: "Terminal", hint: "Run commands and read the laptop's terminals",
		tools: []string{"run_command", "terminal_"},
		try:   []string{"Run the build and tell me what failed"}},
	{id: "tasks", title: "Tasks and tests", hint: "Run tasks and tests, read the results", view: "tasks",
		tools: []string{"tasks_", "tests_"},
		try:   []string{"Run the tests", "Which tests failed last time?"}},
	{id: "lanes", title: "Lanes and agents", hint: "Worktrees, agent runs, races, outside agents", view: "lanes",
		tools: []string{"worktrees_list", "worktree_note", "agent_", "outside_agent", "outside_agents_status"},
		try:   []string{"Which lanes are running?", "Start a race on this task with two agents"}},
	{id: "workflows", title: "Workflows", hint: "List, check, draft and run workflows", view: "workflows",
		tools: []string{"workflow_"},
		try:   []string{"List my workflows", "Draft a workflow that runs lint and tests"}},
	{id: "debug", title: "Debugger", hint: "Breakpoints, start, step, evaluate",
		tools: []string{"debug_", "dap_adapters"},
		try:   []string{"Set a breakpoint where the request is parsed"}},
	{id: "services", title: "Services", hint: "Status, logs, start and stop",
		tools: []string{"services_"},
		try:   []string{"Which services are down?"}},
	{id: "containers", title: "Containers", hint: "Containers, images, volumes, compose",
		tools: []string{"containers_", "images_", "volumes_", "networks_", "contexts_", "registries_", "compose_"},
		try:   []string{"Which containers are running?", "Show the logs of the web container"}},
	{id: "kube", title: "Kubernetes", hint: "Pods, describe and logs (read only)",
		tools: []string{"kube_"},
		try:   []string{"Pods in staging that restart"}},
	{id: "http", title: "HTTP client", hint: "Saved requests, environments, send and history",
		tools: []string{"http_"},
		try:   []string{"Send the login request in dev"}},
	{id: "db", title: "Databases", hint: "Connections, schema, queries (writes ask first)",
		tools: []string{"db_", "redis_"},
		try:   []string{"Show the tables of the main database", "How many orders today?"}},
	{id: "web", title: "Web", hint: "Search the web and read pages (when turned on)",
		tools: []string{"web_fetch", "web_search"},
		try:   []string{"Research the best way to cache this"}},
	{id: "chats", title: "Chats and todos", hint: "Old chats and the chat's todo list", view: "chats",
		tools: []string{"chats_search", "chat_read", "todo_"},
		try:   []string{"What did we decide about the cache last week?"}},
	{id: "more", title: "Settings and more", hint: "Settings, layout, profiles, models, help and the rest",
		try: []string{"Which model is in use?", "How do I pair another phone?"}},
}

// catalogCount holds the built-in tool counts per feature id: the tool
// list does not change while termocode runs, so it is built once.
var catalogCount = sync.OnceValue(func() map[string][2]int {
	out := map[string][2]int{}
	for _, d := range AIToolCatalog() {
		id := catalogFeatureOf(d.Name)
		c := out[id]
		c[0]++
		if !strings.HasPrefix(d.Gate, "auto") {
			c[1]++
		}
		out[id] = c
	}
	return out
})

// catalogFeatureOf is the feature id a tool counts under.
func catalogFeatureOf(tool string) string {
	for _, f := range catalogFeatures {
		for _, n := range f.tools {
			if n == tool || strings.HasSuffix(n, "_") && strings.HasPrefix(tool, n) {
				return f.id
			}
		}
	}
	return "more"
}

// catalogMatch reports whether q (lower case) is in any of the texts.
func catalogMatch(q string, texts ...string) bool {
	if q == "" {
		return true
	}
	for _, t := range texts {
		if strings.Contains(strings.ToLower(t), q) {
			return true
		}
	}
	return false
}

func (m *Model) webAICatalog(call *webCall) tea.Cmd {
	a := call.args.(*webproto.AICatalogArgs)
	q := strings.ToLower(strings.TrimSpace(a.Query))
	out := webproto.AICatalog{Features: []webproto.CatalogFeature{}, Servers: []webproto.CatalogServer{}}
	if a.Feature != webproto.CatalogFeatureMCP {
		counts := catalogCount()
		for _, f := range catalogFeatures {
			if a.Feature != "" && a.Feature != f.id || !catalogMatch(q, append([]string{f.title, f.hint}, f.tools...)...) {
				continue
			}
			c := counts[f.id]
			try := f.try
			if len(try) > webproto.MaxCatalogTry {
				try = try[:webproto.MaxCatalogTry]
			}
			out.Features = append(out.Features, webproto.CatalogFeature{ID: f.id, Title: f.title, Hint: f.hint,
				Tools: c[0], Asks: c[1], Try: append([]string{}, try...), View: f.view})
		}
	}
	if a.Feature == "" || a.Feature == webproto.CatalogFeatureMCP {
		m.catalogServers(&out, q)
	}
	call.reply(out)
	return nil
}

// catalogServers adds the MCP servers and their known tools: cached
// lists only (KnownTools, Caps never start a server).
func (m *Model) catalogServers(out *webproto.AICatalog, q string) {
	set := mcp.LoadSettings(mcpConfigPath())
	if !set.Enabled {
		out.Note = "MCP is off on the laptop."
		return
	}
	mgr := m.aiMCP()
	prof := m.mcpActiveProfile()
	tools := 0
	for _, st := range mgr.Servers() {
		if len(out.Servers) == webproto.MaxCatalogServers {
			out.Truncated = true
			break
		}
		srv, list, known := mgr.KnownTools(st.Name)
		caps, _ := mgr.Caps(st.Name)
		cs := webproto.CatalogServer{Name: webName(st.Name), State: webName(st.State), Trusted: st.Trusted, Cached: st.Cached,
			Known: known, Prompts: caps.Prompts, Resources: caps.Resources, Tools: []webproto.CatalogTool{}}
		nameHit := catalogMatch(q, st.Name)
		for _, t := range list {
			if !nameHit && !catalogMatch(q, t.Name, t.Title, t.Description) {
				continue
			}
			if tools == webproto.MaxCatalogTools {
				out.Truncated = true
				break
			}
			tools++
			cs.Tools = append(cs.Tools, webproto.CatalogTool{Name: webName(t.Name), Title: webText(t.Title),
				Hint: webText(catalogLine(t.Description)), Gate: catalogGate(srv, t, set, prof, st.ToolsChanged),
				Text: webClean("Use " + mcp.DisplayName(st.Name, t.Name) + " to ")})
		}
		if q != "" && !nameHit && len(cs.Tools) == 0 {
			continue
		}
		out.Servers = append(out.Servers, cs)
	}
	if len(out.Servers) == 0 && q == "" {
		out.Note = "No MCP server is set up on the laptop. Add one there (AI: MCP Servers…)."
	}
}

// catalogGate is a tool's gate as the chat would offer it now.
func catalogGate(srv mcp.Server, t mcp.Tool, set mcp.Settings, prof *mcp.Profile, changed bool) string {
	if set.ToolOff(srv.Name, t.Name) || prof != nil && !prof.Allows(srv.Name, t.Name) {
		return "off"
	}
	exposed, mutating, _ := mcp.Gate(srv, t, set.TrustAnnotations)
	switch {
	case !exposed:
		return "off"
	case mutating || changed: // a changed tool list: every tool asks
		return "asks"
	}
	return "runs"
}

// catalogLine is the first line of a description, cut to 160 bytes.
func catalogLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len(s) > 160 {
		s = strings.ToValidUTF8(s[:157], "") + "…"
	}
	return s
}
