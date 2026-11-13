//go:build unix

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
		"-c", "sandbox_workspace_write.network_access=false",
		"-c", `approval_policy="never"`,
		"-c", `mcp_servers.termocode.command="/bin/tc"`,
		"-c", `mcp_servers.termocode.args=["mcp","--socket","/s/a.sock"]`,
		"-c", "mcp_servers.termocode.tool_timeout_sec=900",
		// termocode's gate asks the owner; Codex itself must not (with
		// approval_policy never it fails every call that is not a read:
		// "MCP tool call requires approval, but approval policy is never").
		"-c", `mcp_servers.termocode.default_tools_approval_mode="approve"`,
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

// A step's reasoning level goes to Codex as model_reasoning_effort (max
// is Codex's high); an unknown level and Claude Code get nothing.
func TestArgsReasoning(t *testing.T) {
	base := []string{"exec", "--json", "--color", "never", "--sandbox", "read-only", "-c", `approval_policy="never"`}
	for level, want := range map[string][]string{
		"low":  {"-c", `model_reasoning_effort="low"`},
		"high": {"-c", `model_reasoning_effort="high"`},
		"max":  {"-c", `model_reasoning_effort="high"`},
		"huge": nil,
		"":     nil,
	} {
		got := Args(Spec{CLI: ai.CLICodex, Dir: "/w", Reasoning: level})
		w := append(append(append([]string(nil), base...), want...), "-C", "/w", "-")
		if !reflect.DeepEqual(got, w) {
			t.Errorf("%q:\n got %q\nwant %q", level, got, w)
		}
	}
	if got := Args(Spec{CLI: ai.CLIClaude, Reasoning: "low"}); len(got) != 6 {
		t.Errorf("claude: %q", got)
	}
}

// A step's Instructions (termocode's identity and tool guide) reach the
// model above the user's prompt: Codex as developer_instructions, with
// its own skills list off (its openai-docs skill claims "self-knowledge"
// questions); Claude Code with --append-system-prompt (the = form: a
// text that starts with "-" stays a value). A read-only step, its MCP
// server and the identity are pinned as one argv.
func TestArgsInstructions(t *testing.T) {
	text := "You are the AI inside termocode.\nUse \"db_query\"."
	codex := Args(Spec{CLI: ai.CLICodex, Dir: "/w", Model: "gpt-6-sol", Reasoning: "low", MCP: stepMCP, Instructions: text})
	want := []string{"exec", "--json", "--color", "never", "-m", "gpt-6-sol", "--sandbox", "read-only",
		"-c", `approval_policy="never"`,
		"-c", `model_reasoning_effort="low"`,
		"-c", `developer_instructions="You are the AI inside termocode.\nUse \"db_query\"."`,
		"-c", "skills.include_instructions=false",
		"-c", `mcp_servers.termocode.command="/bin/tc"`,
		"-c", `mcp_servers.termocode.args=["mcp","--socket","/s/a.sock"]`,
		"-c", "mcp_servers.termocode.tool_timeout_sec=900",
		"-c", `mcp_servers.termocode.default_tools_approval_mode="approve"`,
		"-C", "/w", "-"}
	if !reflect.DeepEqual(codex, want) {
		t.Errorf("codex:\n got %q\nwant %q", codex, want)
	}
	claude := Args(Spec{CLI: ai.CLIClaude, Write: true, MCP: stepMCP, Instructions: "-x " + text})
	want = []string{"-p", "--output-format", "stream-json", "--verbose", "--tools", "Read,Grep,Glob,Edit,Write", "--permission-mode", "acceptEdits",
		"--append-system-prompt=-x " + text,
		"--mcp-config", `{"mcpServers":{"termocode":{"command":"/bin/tc","args":["mcp","--socket","/s/a.sock"]}}}`,
		"--allowedTools", "mcp__termocode"}
	if !reflect.DeepEqual(claude, want) {
		t.Errorf("claude:\n got %q\nwant %q", claude, want)
	}
	// blank: nothing (and Codex keeps its skills)
	for _, cli := range []string{ai.CLICodex, ai.CLIClaude} {
		if got, plain := Args(Spec{CLI: cli, Dir: "/w", Instructions: " \n"}), Args(Spec{CLI: cli, Dir: "/w"}); !reflect.DeepEqual(got, plain) {
			t.Errorf("%s blank: %q", cli, got)
		}
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
