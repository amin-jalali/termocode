package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/picker"
)

// mcpServeNoCall is a caller for tests that never reach the UI.
func mcpServeNoCall(context.Context, aiIDEFunc) (string, error) {
	return "", errAIChatStopped
}

// Spec (MS-1): the exposed tool set is exactly this list, in this order.
// A change here is a security decision: update the golden on purpose.
func TestMCPServeAllowListGolden(t *testing.T) {
	want := []string{
		"open_file", "get_editor_state", "get_ide_state", "reveal_in_explorer", "show_panel", "notify", "list_actions", "execute_action",
		"read_file", "list_dir", "search", "workspace_folders",
		"diagnostics", "lsp_hover", "lsp_symbols", "lsp_nav",
		"tasks_list", "tasks_run", "tasks_status", "tests_run", "tests_results",
		"debug_state", "debug_breakpoint", "debug_start", "debug_control", "debug_evaluate",
		"terminal_tabs", "terminal_read",
		"git_status", "git_diff", "git_log", "git_blame", "git_branch",
		"services_list", "services_status", "services_logs", "http_list_requests", "http_environments", "http_history", "http_send",
		"db_connections", "db_schema", "db_ddl", "db_query", "redis_keys", "redis_get", "redis_command",
	}
	aiTestModel(t)
	got := mcpServeCatalog()
	var names []string
	for _, tool := range got {
		names = append(names, tool.Name)
	}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Fatalf("exposed tools changed\n got: %v\nwant: %v", names, want)
	}
	specs := MCPServeToolSpecs()
	if len(specs) != len(want) {
		t.Fatalf("%d specs, want %d", len(specs), len(want))
	}
	for i, s := range specs {
		if s.Name != want[i] || s.Description == "" || s.InputSchema["type"] != "object" || s.Destructive() {
			t.Errorf("spec %d: %+v", i, s)
		}
	}
	// Every table row names a real chat tool (a typo would drop it).
	for _, r := range mcpServeAllowList {
		if _, ok := ai.FindTool(got, r.name); !ok {
			t.Errorf("allow-list row %q is not a chat tool", r.name)
		}
	}
}

// Spec (MS-1): no write, settings, git-write, credential or review tool
// is exposed, and nothing of the user's own MCP servers (MC-11).
func TestMCPServeDenySet(t *testing.T) {
	aiTestModel(t)
	m := &Model{nvim: &nvim.Client{}}
	all := m.aiChatTools(t.TempDir(), mcpServeNoCall)
	all = append(all,
		ai.Tool{Name: "mcp__github__read_file", Run: func(context.Context, map[string]any) (string, error) { return "x", nil }},
		ai.Tool{Name: "mcp__fs__write_file", Mutating: true, Class: "mcp:fs"})
	got := mcpServeTools(all, ".", mcpServeNoCall)
	deny := []string{"write_file", "create_file", "create_dir", "delete", "rename", "format", "run_command",
		"terminal_send", "settings_set", "settings_list", "layout_set", "git_stage", "git_unstage", "git_commit",
		"git_push", "git_pull", "git_fetch", "git_checkout", "git_stash", "git_hunk", "lsp_rename", "lsp_code_actions",
		"services_start", "services_stop", "services_restart", "http_set_environment", "http_save_request",
		"extensions", "help_read", "db_explain", "db_history", "containers_control", "compose_up", "images_pull"}
	for _, tool := range got {
		if contains(deny, tool.Name) || strings.HasPrefix(tool.Name, "mcp__") {
			t.Errorf("exposed %s", tool.Name)
		}
		if tool.Mutating && tool.Gate == nil && !contains([]string{ai.ClassRun, ai.ClassIDE}, tool.Class) {
			t.Errorf("exposed a %s tool: %s", tool.Class, tool.Name)
		}
	}
	for _, name := range append(deny, "mcp__github__read_file", "mcp__fs__write_file") {
		if _, ok := mcpServeRow(name); ok {
			t.Errorf("%s has an allow-list row", name)
		}
	}
}

// Spec (MS-1): narrowed tools refuse the ops an outside agent may not use,
// in Validate and in Run.
func TestMCPServeNarrowedOps(t *testing.T) {
	aiTestModel(t)
	got := mcpServeCatalog()
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"workspace_folders", map[string]any{"op": "add", "path": "."}},
		{"workspace_folders", map[string]any{"op": "remove", "path": "x"}},
		{"terminal_tabs", map[string]any{"op": "new"}},
		{"terminal_tabs", map[string]any{"op": "close", "tab": "1"}},
		{"execute_action", map[string]any{"id": "save"}},
		{"execute_action", map[string]any{"id": "git-stage-all"}},
		{"execute_action", map[string]any{"id": "git-push"}},
		{"execute_action", map[string]any{"id": "toggle-auto-save"}},
		{"execute_action", map[string]any{"id": "theme"}},
		{"execute_action", map[string]any{"id": "quit"}},
		{"execute_action", map[string]any{"id": "reload-buffer"}},
		{"execute_action", map[string]any{"id": "services-start-all"}},
		{"execute_action", map[string]any{"id": "ext-foo.bar"}},
		{"execute_action", map[string]any{"id": "user-build"}},
		{"execute_action", map[string]any{"id": "ai-open-chat"}},
		{"execute_action", map[string]any{"id": "containers-copy-id"}},
	} {
		tool, ok := ai.FindTool(got, tc.tool)
		if !ok {
			t.Fatal(tc.tool)
		}
		if tool.Validate == nil || tool.Validate(tc.args) == nil {
			t.Errorf("Validate allowed %s %v", tc.tool, tc.args)
		}
		if _, err := tool.Run(context.Background(), tc.args); err == nil {
			t.Errorf("Run allowed %s %v", tc.tool, tc.args)
		}
	}
	ws, _ := ai.FindTool(got, "workspace_folders")
	if err := ws.Validate(map[string]any{"op": "list"}); err != nil {
		t.Errorf("workspace_folders list refused: %v", err)
	}
	if enum := ws.Spec().Schema["properties"].(map[string]any)["op"].(map[string]any)["enum"]; strings.Join(enum.([]string), ",") != "list" {
		t.Errorf("workspace_folders op enum = %v", enum)
	}
	for _, id := range []string{"fold-all", "next-diagnostic", "testing-run-all", "debug-stop", "close-buffer"} {
		if err := mcpServeActionAllowed(id); err != nil {
			t.Errorf("%s refused: %v", id, err)
		}
	}
}

// Spec (MS-1): list_actions shows an outside agent only the ids it can run.
func TestMCPServeListActionsFiltered(t *testing.T) {
	items := mcpServeActionItems([]picker.Item{
		{ID: "fold-all", Title: "View: Fold All"},
		{ID: "save", Title: "File: Save"},
		{ID: "git-stage", Title: "Git: Stage Current File"},
		{ID: "user-build", Title: "User: Build it"},
		{ID: "quit", Title: "File: Quit"},
	})
	if len(items) != 1 || items[0].ID != "fold-all" {
		t.Fatalf("items = %v", items)
	}
	m := aiTestModel(t)
	call := func(_ context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(&m)
		return out, err
	}
	root := t.TempDir()
	got := mcpServeTools(m.aiChatTools(root, call), root, call)
	la, _ := ai.FindTool(got, "list_actions")
	out, err := la.Run(context.Background(), map[string]any{"query": "save"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "- save:") || strings.Contains(out, "save-all") {
		t.Fatalf("list_actions offered save: %s", out)
	}
}
