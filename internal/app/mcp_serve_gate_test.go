package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// mcpServeGateCase is one call an outside agent makes and whether it must
// ask on the card first.
type mcpServeGateCase struct {
	tool string
	args map[string]any
	ask  bool
}

// mcpServeGateCases covers every exposed tool: anything with a side
// effect (runs code, starts or steers the debugger, closes or clears
// something) asks; reads and view changes do not.
var mcpServeGateCases = []mcpServeGateCase{
	{"open_file", map[string]any{"path": "main.go"}, false},
	{"get_editor_state", nil, false},
	{"get_ide_state", nil, false},
	{"reveal_in_explorer", map[string]any{"path": "main.go"}, false},
	{"show_panel", map[string]any{"name": "problems"}, false},
	{"notify", map[string]any{"title": "hi"}, false},
	{"list_actions", map[string]any{"query": "fold"}, false},
	{"execute_action", map[string]any{"id": "fold-all"}, false},
	{"execute_action", map[string]any{"id": "testing-run-all"}, true},
	{"execute_action", map[string]any{"id": "debug-start"}, true},
	{"execute_action", map[string]any{"id": "close-buffer"}, true},
	{"read_file", map[string]any{"path": "main.go"}, false},
	{"list_dir", map[string]any{"path": "."}, false},
	{"search", map[string]any{"query": "package"}, false},
	{"workspace_folders", map[string]any{"op": "list"}, false},
	{"diagnostics", nil, false},
	{"lsp_hover", map[string]any{"path": "main.go", "line": 1, "column": 1}, false},
	{"lsp_symbols", map[string]any{"path": "main.go"}, false},
	{"lsp_nav", map[string]any{"path": "main.go", "line": 1, "column": 1, "kind": "definition"}, false},
	{"tasks_list", nil, false},
	{"tasks_run", map[string]any{"label": "say hi"}, true},
	{"tasks_run", map[string]any{"group": "build"}, true},
	{"tasks_status", nil, false},
	{"tests_run", map[string]any{"scope": "all"}, true},
	{"tests_results", nil, false},
	{"debug_state", nil, false},
	{"debug_breakpoint", map[string]any{"op": "list"}, false},
	{"debug_breakpoint", map[string]any{"op": "set", "path": "main.go", "line": 1}, false},
	{"debug_breakpoint", map[string]any{"op": "clear"}, true},
	{"debug_start", map[string]any{"config": "Launch"}, true},
	{"debug_start", nil, true},
	{"debug_control", map[string]any{"op": "continue"}, true},
	{"debug_control", map[string]any{"op": "stop"}, true},
	{"debug_evaluate", map[string]any{"expr": "os.Exit(1)"}, true},
	{"terminal_tabs", map[string]any{"op": "list"}, false},
	{"terminal_tabs", map[string]any{"op": "focus", "tab": "1"}, false},
	{"terminal_read", nil, false},
	{"git_status", nil, false},
	{"git_diff", nil, false},
	{"git_log", nil, false},
	{"git_blame", map[string]any{"path": "main.go"}, false},
	{"git_branch", nil, false},
	{"services_list", nil, false},
	{"services_status", nil, false},
	{"services_logs", map[string]any{"name": "all"}, false},
	{"http_list_requests", nil, false},
	{"http_environments", nil, false},
	{"http_history", nil, false},
	{"db_connections", nil, false},
	{"db_schema", map[string]any{"conn": "local"}, false},
	{"db_schema", map[string]any{"conn": "local", "schema": "public", "table": "users"}, false},
	{"db_ddl", map[string]any{"conn": "local", "table": "users"}, false},
	{"redis_keys", map[string]any{"conn": "cache", "pattern": "session:*"}, false},
}

// mcpServeGateDBs are the connections of mcpServeGateWorkspace: a local
// Postgres, a production one and a Redis (none is ever dialled).
const mcpServeGateDBs = `{"connections":[` +
	`{"id":"local","driver":"postgres","host":"localhost","port":5432,"user":"app","database":"app"},` +
	`{"id":"prod","driver":"postgres","host":"db.example","port":5432,"database":"app","environment":"production"},` +
	`{"id":"cache","driver":"redis","host":"localhost","port":6379}]}`

// mcpServeGateWorkspace is a workspace with a real task and a launch
// configuration, so the calls are valid and only the gate decides.
func mcpServeGateWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range map[string]string{
		"main.go":                   "package main\n\nfunc main() {}\n",
		".termocode/tasks.json":     `{"version":"2.0.0","tasks":[{"label":"say hi","type":"shell","command":"echo hi > ran.txt","group":"build"}]}`,
		".termocode/launch.json":    `{"version":"0.2.0","configurations":[{"name":"Launch","type":"go","request":"launch","program":"."}]}`,
		".termocode/databases.json": mcpServeGateDBs,
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	return root
}

// Spec (MS-1): every exposed tool with a side effect asks an outside
// agent first; read-only calls never ask. Every exposed tool is covered,
// each case is a valid call, and the decision is the bridge's own
// (mcpServeGate, what Call uses).
func TestMCPServeEveryToolGate(t *testing.T) {
	aiTestModel(t)
	mcpServeGateWorkspace(t)
	catalog := mcpServeCatalog()
	covered := map[string]bool{}
	for _, tc := range mcpServeGateCases {
		covered[tc.tool] = true
		tool, ok := ai.FindTool(catalog, tc.tool)
		if !ok {
			t.Errorf("%s is not exposed", tc.tool)
			continue
		}
		if tool.Validate != nil {
			if err := tool.Validate(tc.args); err != nil {
				t.Errorf("%s %v: invalid (%v); the case must be a valid call", tc.tool, tc.args, err)
				continue
			}
		}
		if _, ask := mcpServeGate(tool, tc.args); ask != tc.ask {
			t.Errorf("%s %v: asks=%v, want %v", tc.tool, tc.args, ask, tc.ask)
		}
	}
	for _, r := range mcpServeAllowList {
		if !covered[r.name] {
			t.Errorf("exposed tool %s has no gate case", r.name)
		}
	}
	// tools/list must not call a tool that can ask read-only.
	for _, spec := range MCPServeToolSpecs() {
		for _, tc := range mcpServeGateCases {
			if tc.tool == spec.Name && tc.ask && spec.ReadOnly() {
				t.Errorf("%s is listed read-only but asks", spec.Name)
			}
		}
	}
}

// Spec (MS-1): through the bridge, every asking case of the table opens
// the confirm step and a deny never runs the tool (no UI step past the
// tool lookup).
func TestMCPServeEveryAskingToolDenied(t *testing.T) {
	m := aiTestModel(t)
	root := mcpServeGateWorkspace(t)
	var asked []string
	src, hub, _ := mcpServeTestSource(root, func(_ context.Context, _ string, tool ai.Tool, _ string) error {
		asked = append(asked, tool.Name)
		return &mcp.CallError{Code: mcp.CodeDeclined, Message: "declined by the user"}
	})
	defer hub.stop()
	for _, tc := range mcpServeGateCases {
		if !tc.ask {
			continue
		}
		asked = nil
		_, err, ui := mcpServeDrive(t, &m, hub, func() (mcp.CallResult, error) {
			return src.Call(mcpServeCtx("agent"), tc.tool, tc.args)
		})
		var ce *mcp.CallError
		if len(asked) != 1 || !errors.As(err, &ce) || ce.Code != mcp.CodeDeclined || ui != 1 {
			t.Errorf("%s %v: asked=%v err=%v ui=%d", tc.tool, tc.args, asked, err, ui)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "ran.txt")); err == nil {
		t.Fatal("a declined task ran")
	}
}

// Spec (MS-1): tasks_run (a task that exists), debug_start and
// debug_evaluate open the confirm card for an outside agent; Deny is
// -32000 and nothing happens: no task tab or run, no debug session,
// nothing evaluated (no UI step past the tool lookup).
func TestMCPServeRunToolsAskOnCard(t *testing.T) {
	m, _, hub := mcpServeTestModel(t)
	root := mcpServeGateWorkspace(t)
	src := newMCPServeSource(hub, root)
	for _, tc := range []mcpServeGateCase{
		{"tasks_run", map[string]any{"label": "say hi"}, true},
		{"debug_start", map[string]any{"config": "Launch"}, true},
		{"debug_evaluate", map[string]any{"expr": "panic(1)"}, true},
	} {
		uiSteps := 0
		call := mcpServeStartCall(src, "agent", tc.tool, tc.args)
		mcpServePumpUntil(t, m, hub, tc.tool+" card", func() bool { return mcpServeCardOpen(m, tc.tool) })
		before := len(m.terminalTabs)
		mcpServeAnswer(m, "deny")
		got := mcpServeAwaitCounting(t, m, hub, call, &uiSteps)
		var ce *mcp.CallError
		if !errors.As(got.err, &ce) || ce.Code != mcp.CodeDeclined || len(got.r.Content) != 0 {
			t.Fatalf("%s deny: %+v", tc.tool, got)
		}
		if uiSteps != 1 { // the audit line only
			t.Errorf("%s: %d UI steps after deny, want only the audit line", tc.tool, uiSteps)
		}
		if len(m.terminalTabs) != before || m.dapSessionActive {
			t.Errorf("%s: a side effect after deny", tc.tool)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "ran.txt")); err == nil {
		t.Fatal("the declined task ran")
	}
}

// mcpServeAwaitCounting is mcpServeAwait that counts UI call steps.
func mcpServeAwaitCounting(t *testing.T, m *Model, hub *mcpServeHub, ch chan mcpServeTestCall, n *int) mcpServeTestCall {
	t.Helper()
	var got mcpServeTestCall
	done := false
	for !done {
		select {
		case got = <-ch:
			done = true
		case msg := <-hub.ch:
			if _, ok := msg.(mcpServeCallMsg); ok {
				*n++
			}
			m.mcpServeTestHandle(msg)
		}
	}
	return got
}
