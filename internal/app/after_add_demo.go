package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/afteradd"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// afterAddDemo is the dev action "afteradddemo:mcp" (the GIFs): the
// after-add checks of a made-up MCP server "postgres" that was just
// added, over a fake probe (nothing connects, no tool is called), with a
// saved PostgreSQL connection "shop" to link to. The card shows as it
// does for a real server.
func (m *Model) afterAddDemo(string) tea.Cmd {
	yes, no := true, false
	tools := []mcp.Tool{
		{Name: "query", Description: "Run a read-only SQL query", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: &yes}},
		{Name: "list_tables", Description: "List the tables of a schema", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: &yes}},
		{Name: "describe_table", Description: "Show the columns of a table", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: &yes}},
		{Name: "insert_row", Description: "Insert a row into a table", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: &no}},
		{Name: "read_file", Description: "Read a file from the server's disk"},
	}
	p := afterAddMCPProbe{
		connect:   func(context.Context, string) error { return nil },
		known:     func(string) ([]mcp.Tool, bool) { return tools, true },
		signIn:    func(string) bool { return false },
		prompts:   func(context.Context, string) (int, error) { return 2, nil },
		resources: func(context.Context, string) (int, error) { return 0, nil },
		builtins:  aiCoreTools,
		links:     func() ([]string, string) { return []string{"shop"}, "" },
	}
	rule := afterAddMCPRule(p)
	// The demo clock stands still: say a plausible handshake time.
	rule.Checks[0].Run = func(context.Context, afteradd.Event) ([]afteradd.Finding, error) {
		return []afteradd.Finding{{Level: afteradd.OK, Text: "connected, answered in 84 ms"}}, nil
	}
	m.afterAddEngine().Add(rule)
	return m.afterAdd(afterAddMCPEvent("postgres"))
}
