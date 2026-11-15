package app

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Tool groups of a chat turn (ai.DeferToolGroups): a request sends the
// core tools below and tool_search; the others wait in these groups.

// aiCoreTools are sent on every request: the tools of everyday coding
// and the ones the doctrine (aiDoctrine) names.
var aiCoreTools = []string{
	"read_file", "list_dir", "search", "glob", "edit_file", "write_file", "create_file",
	"run_command", "git_status", "git_diff", "diagnostics", "tests_run",
	"todo_write", "agent_run_start", "skill_load", "rule_load", // the loaders: only there when the prompt offers skills or rules
}

// aiDeferGroups are the groups of the other tools, in the order
// tool_search lists them. A tool no group names is in "more".
var aiDeferGroups = []ai.ToolGroup{
	{Name: "files", About: "create folders, delete, rename, structural and meaning search, images and PDFs, screenshots, canvas, worktrees",
		Names: []string{"create_dir", "delete", "rename", "ast_grep", "codebase_search", "look_at", "proof_screenshot", "canvas_", "worktree", "worktree_", "worktrees_"}},
	{Name: "git", About: "stage, commit, push, pull, branches, history, blame, stash, pull requests",
		Names: []string{"git_"}},
	{Name: "db", About: "the owner's database connections: tables, schema, queries, data; redis",
		Names: []string{"db_", "redis_"}},
	{Name: "containers", About: "Docker here and on the owner's ssh servers: containers, images, volumes, compose, registries",
		Names: []string{"contexts_", "containers_", "images_", "volumes_", "networks_", "compose_", "registries_", "registry_"}},
	{Name: "kube", About: "Kubernetes: list, describe, logs",
		Names: []string{"kube_"}},
	{Name: "services", About: "the project's dev services: status, logs, start, stop, connect",
		Names: []string{"services_", "service_"}},
	{Name: "terminal", About: "type into and read the owner's visible terminals",
		Names: []string{"terminal_"}},
	{Name: "tests", About: "test results, project tasks",
		Names: []string{"tests_", "tasks_"}},
	{Name: "debug", About: "breakpoints, start, step, evaluate",
		Names: []string{"debug_"}},
	{Name: "code", About: "language servers: hover, symbols, definitions, rename, code actions",
		Names: []string{"lsp_"}},
	{Name: "ide", About: "open files for the owner, panels, IDE commands, settings, layout, the built-in guide, extensions",
		Names: []string{"open_file", "get_editor_state", "get_ide_state", "notify", "show_panel", "reveal_in_explorer", "list_actions",
			"execute_action", "settings_", "layout_", "ide_op", "help_read", "extensions", "workspace_folders", "dap_adapters"}},
	{Name: "http", About: "the owner's .http requests: send, environments, history",
		Names: []string{"http_"}},
	{Name: "web", About: "read web pages, search the web, outside repositories",
		Names: []string{"web_", "repo_"}},
	{Name: "agents", About: "background agent runs, races, splits, outside agents",
		Names: []string{"agent_", "outside_agent_", "outside_agents_"}},
	{Name: "workflows", About: "the owner's workflows: list, read, draft, run",
		Names: []string{"workflow_"}},
	{Name: "views", About: "show data as a view (tables, lists, status) on the owner's screen",
		Names: []string{"view_", "ai_view_"}},
	{Name: "chat", About: "this chat's todo list, schedule and docs, old chats, tracker pins, specs, prompts",
		Names: []string{"todo_", "chat_", "chats_", "tracker_", "specs", "prompt_"}},
	{Name: "termocode", About: "termocode itself: AI profiles, models, MCP services, usage",
		Names: []string{"profile_", "profiles_", "model_", "models_", "mcp_server_", "mcp_services_", "usage_"}},
}

// aiTurnToolGroups is the loaded-groups set of the open chat, made on
// first use.
func (s *aiState) aiTurnToolGroups() *ai.LoadedGroups {
	if s.toolGroups == nil {
		s.toolGroups = map[string]*ai.LoadedGroups{}
	}
	l := s.toolGroups[s.conv.ID]
	if l == nil {
		l = &ai.LoadedGroups{}
		s.toolGroups[s.conv.ID] = l
	}
	return l
}

// aiDeferTurnTools hides the non-core tools of a chat turn behind their
// groups (ai_tool_groups "off", or a provider without native tool
// calling: tools unchanged). preload are groups the turn's context calls
// for (aiAttachGroups); they load first.
func (s *aiState) aiDeferTurnTools(tools []ai.Tool, native bool, preload []string) []ai.Tool {
	if !native || !ai.ToolGroupsEnabled(s.settings.ToolGroups) {
		return tools
	}
	loaded := s.aiTurnToolGroups()
	loaded.Load(preload...)
	ab := aiAbilityStates(s.settings.Abilities, aiWorkspaceRoot(), preload) // ai_abilities.go
	return ai.DeferToolGroupsWith(tools, aiCoreTools, aiDeferGroups, loaded, ai.GroupPolicy{State: ab.state, Enable: ab.enable})
}

// aiAttachGroups are the tool groups the attachments call for: a
// database card the db group, an issue or pull request git, and so on.
func aiAttachGroups(attached []aiAttachment) []string {
	var out []string
	for _, a := range attached {
		switch {
		case strings.HasPrefix(a.Label, "db ") || a.Label == "failed query":
			out = append(out, "db")
		case a.Kind == "issue" || a.Kind == "pr":
			out = append(out, "git")
		case a.Kind == "run" || a.Kind == "race":
			out = append(out, "agents")
		case a.Kind == "workflow":
			out = append(out, "workflows")
		case a.Kind == "view":
			out = append(out, "views")
		case a.Kind == "term":
			out = append(out, "terminal")
		}
	}
	return out
}
