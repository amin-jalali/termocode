package clistep

import (
	"context"
	"reflect"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// A CLI step gets termocode's tools through termocode's MCP server: the
// step's own server (its socket) goes to the CLI on the command line,
// never in a config file of the user's. termocode's own gate shows and
// audits each call, so the stream's copy of a termocode call is not
// shown twice.

var stepMCP = &MCPServer{Name: "termocode", Command: "/bin/tc", Args: []string{"mcp", "--socket", "/s/a.sock"}}

func TestArgsMCP(t *testing.T) {
	codex := Args(Spec{CLI: ai.CLICodex, Dir: "/w", Write: true, Model: "gpt-6-sol", MCP: stepMCP})
	want := []string{"exec", "--json", "--color", "never", "-m", "gpt-6-sol", "--sandbox", "workspace-write",
		"-c", `mcp_servers.termocode.command="/bin/tc"`,
		"-c", `mcp_servers.termocode.args=["mcp","--socket","/s/a.sock"]`,
		"-c", "mcp_servers.termocode.tool_timeout_sec=900",
		"-C", "/w", "-"}
	if !reflect.DeepEqual(codex, want) {
		t.Errorf("codex:\n got %q\nwant %q", codex, want)
	}
	claude := Args(Spec{CLI: ai.CLIClaude, MCP: stepMCP})
	want = []string{"-p", "--output-format", "stream-json", "--verbose", "--tools", "Read,Grep,Glob",
		"--mcp-config", `{"mcpServers":{"termocode":{"command":"/bin/tc","args":["mcp","--socket","/s/a.sock"]}}}`,
		"--allowedTools", "mcp__termocode"}
	if !reflect.DeepEqual(claude, want) {
		t.Errorf("claude:\n got %q\nwant %q", claude, want)
	}
}

func TestRunCodexOwnMCPCallsNotTwice(t *testing.T) {
	root := t.TempDir()
	f := newFake(t, stream(t,
		`{"type":"item.started","item":{"id":"m","type":"mcp_tool_call","server":"termocode","tool":"db_schema"}}`,
		`{"type":"item.completed","item":{"id":"m","type":"mcp_tool_call","server":"termocode","tool":"db_schema","status":"completed"}}`,
		`{"type":"item.started","item":{"id":"g","type":"mcp_tool_call","server":"gh","tool":"issues"}}`,
		`{"type":"item.completed","item":{"id":"g","type":"mcp_tool_call","server":"gh","tool":"issues","status":"completed"}}`,
		`{"type":"item.completed","item":{"id":"a","type":"agent_message","text":"12 tables."}}`,
		`{"type":"turn.completed"}`,
	))
	var r rec
	res, err := Run(context.Background(), Spec{CLI: ai.CLICodex, Dir: root, Env: f.env, MCP: stepMCP}, "x", r.emit)
	if err != nil || res.Text != "12 tables." {
		t.Fatalf("res %+v err %v", res, err)
	}
	want := []string{"start gh.issues: gh.issues", "end gh.issues: completed"}
	if got := r.tools(); !reflect.DeepEqual(got, want) {
		t.Errorf("tools\n got %q\nwant %q", got, want)
	}
}
