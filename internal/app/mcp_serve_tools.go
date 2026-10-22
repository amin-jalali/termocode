package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/picker"
)

// termocode as an MCP server (M1): the tools an outside agent (Claude
// Code, Codex, …) may call through the session socket. The list is ONE
// table; tools/list and tools/call both read it, and
// TestMCPServeAllowListGolden pins it. What is left out on purpose:
// file writes, run_command, terminal_send, git writes, settings,
// credentials, LSP rename / code actions, service starts, HTTP sends,
// database writes, containers and every tool of the user's own MCP servers
// (mcp__*: never re-exported, MC-11). The agent has its own shell and
// editor; we add the IDE, not a second agent.

// mcpServeToolRow is one exposed tool. only (optional) narrows a tool
// with an op argument to these ops.
type mcpServeToolRow struct {
	name string
	only []string
}

// mcpServeAllowList is the exposed tool set, in tools/list order.
var mcpServeAllowList = []mcpServeToolRow{
	// Editor / IDE
	{name: "open_file"},
	{name: "get_editor_state"},
	{name: "get_ide_state"},
	{name: "reveal_in_explorer"},
	{name: "show_panel"},
	{name: "notify"},
	{name: "list_actions"},   // only the ids mcpServeActionAllowed lets through
	{name: "execute_action"}, // the same filter, then the policy map
	// Workspace (read)
	{name: "read_file"},
	{name: "list_dir"},
	{name: "search"},
	{name: "workspace_folders", only: []string{"list"}},
	// LSP (read)
	{name: "diagnostics"},
	{name: "lsp_hover"},
	{name: "lsp_symbols"},
	{name: "lsp_nav"},
	// Tasks and tests (runs ask first, class run)
	{name: "tasks_list"},
	{name: "tasks_run"},
	{name: "tasks_status"},
	{name: "tests_run"},
	{name: "tests_results"},
	// Debug (as in the chat: start / control ask)
	{name: "debug_state"},
	{name: "debug_breakpoint"},
	{name: "debug_start"},
	{name: "debug_control"},
	{name: "debug_evaluate"},
	// Terminal (read; no new shells, no closing, no typing)
	{name: "terminal_tabs", only: []string{"list", "focus"}},
	{name: "terminal_read"},
	// Git (read)
	{name: "git_status"},
	{name: "git_diff"},
	{name: "git_log"},
	{name: "git_blame"},
	{name: "git_branch"},
	// Services and HTTP (read)
	{name: "services_list"},
	{name: "services_status"},
	{name: "services_logs"},
	{name: "http_list_requests"},
	{name: "http_environments"},
	{name: "http_history"},
	// Databases (read: connections, schema, DDL, Redis key names)
	{name: "db_connections"},
	{name: "db_schema"},
	{name: "db_ddl"},
	{name: "redis_keys"},
}

// mcpServeRow returns the allow-list row of name. Names of the user's
// own MCP servers (mcp__*) are never found, whatever the table says.
func mcpServeRow(name string) (mcpServeToolRow, bool) {
	if strings.HasPrefix(name, "mcp__") {
		return mcpServeToolRow{}, false
	}
	for _, r := range mcpServeAllowList {
		if r.name == name {
			return r, true
		}
	}
	return mcpServeToolRow{}, false
}

// mcpServeTools picks the exposed tools out of all (the chat's tool
// list, built with an MCP caller), in table order, narrowed per row.
func mcpServeTools(all []ai.Tool, call aiIDECaller) []ai.Tool {
	out := make([]ai.Tool, 0, len(mcpServeAllowList))
	for _, r := range mcpServeAllowList {
		t, ok := ai.FindTool(all, r.name)
		if !ok || strings.HasPrefix(t.Name, "mcp__") {
			continue
		}
		if len(r.only) > 0 {
			t = mcpServeOnlyOps(t, r.only)
		}
		switch t.Name {
		case "execute_action":
			t = mcpServeExecuteAction(t)
		case "list_actions":
			t = mcpServeListActions(t, call)
		}
		out = append(out, t)
	}
	return out
}

// mcpServeCatalog is every exposed tool as the docs see it: built on a
// model with an (unstarted) editor client, so the LSP and editor tools are
// there whatever runs now. tools/list answers from it; tools/call builds
// the live tool on the UI goroutine.
func mcpServeCatalog() []ai.Tool {
	m := &Model{nvim: &nvim.Client{}}
	call := func(context.Context, aiIDEFunc) (string, error) { return "", errors.New("list only") }
	all := m.aiChatTools(".", call)
	if _, ok := ai.FindTool(all, "redis_keys"); !ok {
		all = append(all, aiRedactResults(aiRedisTools(".", call))...) // listed only where a Redis connection exists
	}
	return mcpServeTools(all, call)
}

// MCPServeToolSpecs are the tools/list entries of the MCP server (the
// relay answers with them when no UI runs, MS-2). Nothing exposed is
// destructive; readOnlyHint is set for tools that never ask.
func MCPServeToolSpecs() []mcp.Tool {
	tools := mcpServeCatalog()
	out := make([]mcp.Tool, 0, len(tools))
	no := false
	for _, t := range tools {
		spec := t.Spec()
		readOnly := !t.Mutating && t.Gate == nil
		out = append(out, mcp.Tool{Name: spec.Name, Description: spec.Description, InputSchema: spec.Schema,
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: &readOnly, DestructiveHint: &no}})
	}
	return out
}

// mcpServeOnlyOps narrows a tool with an "op" argument to ops: the schema
// lists only them, and Validate and Run refuse the others.
func mcpServeOnlyOps(t ai.Tool, ops []string) ai.Tool {
	params := make(map[string]any, len(t.Params))
	for k, v := range t.Params {
		params[k] = v
	}
	op := map[string]any{}
	if old, ok := params["op"].(map[string]any); ok {
		for k, v := range old {
			op[k] = v
		}
	}
	op["enum"] = append([]string(nil), ops...)
	params["op"] = op
	t.Params = params
	check := func(a map[string]any) error {
		if o := ai.ArgString(a, "op"); !contains(ops, o) {
			return fmt.Errorf("op %q is not open to external agents; use one of %s", o, strings.Join(ops, ", "))
		}
		return nil
	}
	validate, run := t.Validate, t.Run
	t.Validate = func(a map[string]any) error {
		if err := check(a); err != nil {
			return err
		}
		if validate != nil {
			return validate(a)
		}
		return nil
	}
	t.Run = func(ctx context.Context, a map[string]any) (string, error) {
		if err := check(a); err != nil { // keep Run safe alone
			return "", err
		}
		return run(ctx, a)
	}
	return t
}

// mcpServeIDEActions are the confirm (class ide) palette ids an external
// agent may run: they stop, clear or close things in the IDE. The other
// ide ids change settings, write config files or the clipboard.
var mcpServeIDEActions = map[string]bool{
	"close-buffer":    true,
	"bookmark-clear":  true,
	"output-clear":    true,
	"debug-clear-bps": true,
	"debug-pause":     true,
	"debug-stop":      true,
	"http-cancel":     true,
	"tasks-terminate": true,
	"testing-stop":    true,
}

// mcpServeActionPrefixes are palette id groups closed to external agents
// even when the chat may run them: the chat's own UI, databases,
// containers, extension and user commands (they run code).
var mcpServeActionPrefixes = []string{"ai-", "db-", "containers-", extPaletteIDPfx, "user-"}

var errMCPServeAction = errors.New("is not open to external agents (MCP): it changes files, git, settings, services or runs extension code")

// mcpServeActionAllowed refuses the execute_action ids an external agent
// must not run: never ids, then everything but auto ids, run ids and the
// few ide ids above.
func mcpServeActionAllowed(id string) error {
	p, err := aiCheckAction(id)
	if err != nil {
		return err
	}
	for _, pfx := range mcpServeActionPrefixes {
		if strings.HasPrefix(id, pfx) {
			return fmt.Errorf("command %q %w", id, errMCPServeAction)
		}
	}
	switch {
	case p.Gate == aiGateAuto,
		p.Gate == aiGateConfirm && p.Class == ai.ClassRun,
		p.Gate == aiGateConfirm && p.Class == ai.ClassIDE && mcpServeIDEActions[id]:
		return nil
	}
	return fmt.Errorf("command %q %w", id, errMCPServeAction)
}

// mcpServeExecuteAction is execute_action behind mcpServeActionAllowed.
func mcpServeExecuteAction(t ai.Tool) ai.Tool {
	validate, run := t.Validate, t.Run
	t.Validate = func(a map[string]any) error {
		if err := mcpServeActionAllowed(ai.ArgString(a, "id")); err != nil {
			return err
		}
		if validate != nil {
			return validate(a)
		}
		return nil
	}
	t.Run = func(ctx context.Context, a map[string]any) (string, error) {
		if err := mcpServeActionAllowed(ai.ArgString(a, "id")); err != nil {
			return "", err
		}
		return run(ctx, a)
	}
	return t
}

// mcpServeListActions is list_actions that shows only the ids
// execute_action takes from an external agent.
func mcpServeListActions(t ai.Tool, call aiIDECaller) ai.Tool {
	t.Run = func(ctx context.Context, a map[string]any) (string, error) {
		q := ai.ArgString(a, "query")
		return call(ctx, func(m *Model) (string, tea.Cmd, error) {
			return aiListActions(mcpServeActionItems(m.aiLiveActions()), q, aiListActionsMax), nil, nil
		})
	}
	return t
}

// mcpServeActionItems keeps the palette rows an external agent may run.
func mcpServeActionItems(items []picker.Item) []picker.Item {
	out := make([]picker.Item, 0, len(items))
	for _, it := range items {
		if mcpServeActionAllowed(it.ID) == nil {
			out = append(out, it)
		}
	}
	return out
}
