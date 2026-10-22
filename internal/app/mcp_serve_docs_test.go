package app

import "testing"

// Spec (MS-5): the generated "MCP server" column comes from the allow-list
// table and the tools' own gates, so it can not drift from what the server
// does.
func TestMCPServeGateDocs(t *testing.T) {
	got := MCPServeGateDocs()
	want := map[string]string{
		"read_file":        MCPServeYes,
		"tasks_run":        MCPServeAsks,
		"debug_evaluate":   MCPServeAsks,
		"terminal_tabs":    MCPServeYes, // narrowed to list and focus
		"debug_breakpoint": MCPServeSome,
		"execute_action":   MCPServeSome,
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s = %q, want %q", name, got[name], w)
		}
	}
	for _, name := range []string{"write_file", "run_command", "terminal_send", "git_commit", "settings_set", "http_send", "db_query"} {
		if g, ok := got[name]; ok {
			t.Errorf("%s is documented as offered (%q) but is not in the allow-list", name, g)
		}
	}
	for _, r := range mcpServeAllowList {
		if _, ok := got[r.name]; !ok {
			t.Errorf("allow-list row %s has no gate word", r.name)
		}
	}
}
