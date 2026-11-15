package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/afteradd"
	"github.com/amin-jalali/termocode/internal/mcp"
)

func mcpTestTool(name string, readOnly *bool, schemaWords int) mcp.Tool {
	t := mcp.Tool{Name: name, Description: "does " + name}
	if readOnly != nil {
		t.Annotations = &mcp.ToolAnnotations{ReadOnlyHint: readOnly}
	}
	if schemaWords > 0 {
		t.InputSchema = map[string]any{"description": strings.Repeat("word ", schemaWords)}
	}
	return t
}

// afterAddFakeMCP is a probe of one fake server; it records every call
// so the test can tell that no tool of the server was called.
func afterAddFakeMCP(tools []mcp.Tool, prompts int, conns []string, remote string) (afterAddMCPProbe, *[]string) {
	var calls []string
	var mu sync.Mutex
	rec := func(c string) { mu.Lock(); calls = append(calls, c); mu.Unlock() }
	return afterAddMCPProbe{
		connect:   func(context.Context, string) error { rec("connect"); return nil },
		known:     func(string) ([]mcp.Tool, bool) { rec("tools"); return tools, true },
		signIn:    func(string) bool { return false },
		prompts:   func(context.Context, string) (int, error) { return prompts, nil },
		resources: func(context.Context, string) (int, error) { return 0, nil },
		builtins:  aiCoreTools,
		links:     func() ([]string, string) { return conns, remote },
	}, &calls
}

func afterAddMCPRun(p afterAddMCPProbe, name string) afteradd.Result {
	return afteradd.New(nil, afterAddMCPRule(p)).Run(context.Background(), afterAddMCPEvent(name))
}

func afterAddText(r afteradd.Result) string {
	var b []string
	for _, f := range r.Findings {
		b = append(b, f.Text)
	}
	return strings.Join(b, "\n")
}

// Spec (owner 2026-11-15, after-add 3): a newly added MCP server is
// checked without calling any of its tools: the handshake and its time,
// the tools by risk (no annotation counts as risky), the schema cost
// (big: on demand), names that overlap termocode's own tools, and links
// to what is already there (a Postgres server and a saved Postgres
// connection). Risky tools: the one card offers to review them.
func TestAfterAddMCPChecks(t *testing.T) {
	yes, no := true, false
	tools := []mcp.Tool{
		mcpTestTool("query", &yes, 0),
		mcpTestTool("insert_row", &no, 0),
		mcpTestTool("read_file", nil, 5000), // no annotation, a big schema, a built-in name
	}
	p, calls := afterAddFakeMCP(tools, 0, []string{"shop"}, "")
	res := afterAddMCPRun(p, "postgres")
	text := afterAddText(res)
	for _, want := range []string{
		"connected, answered in",
		"3 tools: 1 read-only, 1 write, 1 unknown",
		"big, so they stay on demand",
		"same names as termocode's own tools: read_file",
		"the PostgreSQL connection shop",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("findings miss %q:\n%s", want, text)
		}
	}
	for _, c := range *calls {
		if c != "connect" && c != "tools" {
			t.Errorf("a check called %q", c)
		}
	}
	s := res.Suggestion
	if s == nil || s.Action.Op != "ai-mcp-servers" || !strings.Contains(s.Title, "review its tools") {
		t.Fatalf("suggestion %+v", s)
	}
}

// Read-only tools only: prompts come next as the suggestion, then the
// chat. A GitHub server links to this repo's GitHub remote. A failed
// handshake is a warning line, not a lost card.
func TestAfterAddMCPSuggestsPromptsThenChat(t *testing.T) {
	yes := true
	ro := []mcp.Tool{mcpTestTool("list_issues", &yes, 0)}
	p, _ := afterAddFakeMCP(ro, 2, nil, "https://github.com/acme/shop")
	res := afterAddMCPRun(p, "github")
	if s := res.Suggestion; s == nil || s.Action.Op != "ai-mcp-insert" {
		t.Fatalf("prompts: %+v", s)
	}
	if !strings.Contains(afterAddText(res), "acme/shop on GitHub") {
		t.Errorf("no repo link:\n%s", afterAddText(res))
	}
	p, _ = afterAddFakeMCP(ro, 0, nil, "")
	p.connect = func(context.Context, string) error { return errors.New("connection refused") }
	res = afterAddMCPRun(p, "notes")
	if s := res.Suggestion; s == nil || s.Action.Op != "ai-open-chat" {
		t.Fatalf("chat: %+v", s)
	}
	if !strings.Contains(afterAddText(res), "could not check: connection refused") {
		t.Errorf("handshake failure lost:\n%s", afterAddText(res))
	}
}

// The wizard's Connected card comes first; the after-add card waits for
// it and opens when it closes.
func TestAfterAddMCPWaitsForTheConnectCard(t *testing.T) {
	m := agentMCPTestModel(t)
	yes := true
	p, _ := afterAddFakeMCP([]mcp.Tool{mcpTestTool("q", &yes, 0)}, 0, nil, "")
	old := afterAddMCPProbeFor
	afterAddMCPProbeFor = func(*Model) afterAddMCPProbe { return p }
	t.Cleanup(func() { afterAddMCPProbeFor = old })
	m.connectOpen = true
	msg := m.afterAddMCPAdded("notes")().(afterAddMsg)
	m.onAfterAdd(msg)
	if m.confirmOpen {
		t.Fatal("the card opened over the connect card")
	}
	m.connectOpen = false
	m.onAfterAddRetry(afterAddRetryMsg{})
	if !m.confirmOpen || m.confirmKind != confirmKindAfterAdd || !strings.Contains(m.confirm.View(), "Connected notes") {
		t.Fatal("the after-add card did not open after the connect card")
	}
}
