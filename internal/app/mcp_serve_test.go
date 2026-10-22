package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// mcpServeTestAudit records audit lines.
type mcpServeTestAudit struct {
	mu    sync.Mutex
	lines []string
}

func (a *mcpServeTestAudit) add(_ time.Time, client, tool, args, gate, result string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lines = append(a.lines, "by=mcp:"+client+" "+tool+" "+args+" gate="+gate+" → "+result)
}

func (a *mcpServeTestAudit) all() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return strings.Join(a.lines, "\n")
}

// mcpServeTestSource builds a source over a fresh hub for workspace root.
func mcpServeTestSource(root string, confirm mcpServeConfirmFunc) (*mcpServeSource, *mcpServeHub, *mcpServeTestAudit) {
	hub := newMCPServeHub()
	audit := &mcpServeTestAudit{}
	return &mcpServeSource{call: hub.caller(), root: root, specs: MCPServeToolSpecs(), confirm: confirm, audit: audit.add}, hub, audit
}

// mcpServeDrive runs call on a goroutine and plays Update for the hub
// until it returns. It returns the result and how many UI calls ran.
func mcpServeDrive(t *testing.T, m *Model, hub *mcpServeHub, call func() (mcp.CallResult, error)) (mcp.CallResult, error, int) {
	t.Helper()
	type res struct {
		r   mcp.CallResult
		err error
	}
	done := make(chan res, 1)
	go func() {
		r, err := call()
		done <- res{r, err}
	}()
	n := 0
	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg := <-hub.ch:
			if _, ok := msg.(mcpServeCallMsg); ok {
				n++
			}
			m.mcpServeTestHandle(msg)
		case r := <-done:
			return r.r, r.err, n
		case <-deadline:
			t.Fatal("MCP call did not finish")
		}
	}
}

// mcpServeTestHandle handles one hub message as Update does (the
// returned cmd is dropped: tests drive the hub themselves).
func (m *Model) mcpServeTestHandle(msg tea.Msg) {
	if _, ok := m.handleMCPServeMsg(msg); !ok {
		panic("not an MCP serve message")
	}
}

func mcpServeCtx(client string) context.Context {
	return mcp.WithClientName(context.Background(), client)
}

func mcpServeText(r mcp.CallResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}

// Spec (MS-1): a read tool runs at once, through the UI caller, and the
// result is the tool's text.
func TestMCPServeCallAutoTool(t *testing.T) {
	m := aiTestModel(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src, hub, audit := mcpServeTestSource(root, func(context.Context, string, ai.Tool, string) error {
		t.Error("a read tool asked")
		return nil
	})
	defer hub.stop()
	r, err, _ := mcpServeDrive(t, &m, hub, func() (mcp.CallResult, error) {
		return src.Call(mcpServeCtx("claude-code"), "read_file", map[string]any{"path": "main.go"})
	})
	if err != nil || r.IsError || !strings.Contains(mcpServeText(r), "package main") {
		t.Fatalf("read_file: %+v %v", r, err)
	}
	if got := audit.all(); !strings.Contains(got, "by=mcp:claude-code read_file path=main.go gate=auto") {
		t.Fatalf("audit: %s", got)
	}
}

// Spec (MS-1): a never tool, an mcp__* tool and a typo are refused by name
// with -32602, before anything runs.
func TestMCPServeNeverToolRefused(t *testing.T) {
	m := aiTestModel(t)
	root := t.TempDir()
	src, hub, audit := mcpServeTestSource(root, func(context.Context, string, ai.Tool, string) error {
		t.Error("a refused tool asked")
		return nil
	})
	defer hub.stop()
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"write_file", map[string]any{"path": "x.txt", "content": "pwned"}},
		{"run_command", map[string]any{"command": "touch pwned"}},
		{"terminal_send", map[string]any{"text": "rm -rf ~\n"}},
		{"settings_set", map[string]any{"key": "mcp_server", "value": "on"}},
		{"git_commit", map[string]any{"message": "x"}},
		{"mcp__github__create_issue", map[string]any{"title": "x"}},
		{"mcp__fs__read_file", map[string]any{"path": "x"}},
		{"no_such_tool", nil},
	} {
		r, err, ui := mcpServeDrive(t, &m, hub, func() (mcp.CallResult, error) {
			return src.Call(mcpServeCtx("evil"), tc.name, tc.args)
		})
		var ce *mcp.CallError
		if !errors.As(err, &ce) || ce.Code != mcp.CodeInvalidParams || len(r.Content) != 0 {
			t.Errorf("%s: %+v %v", tc.name, r, err)
		}
		if ui != 0 {
			t.Errorf("%s reached the UI %d times", tc.name, ui)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "x.txt")); err == nil {
		t.Fatal("write_file wrote a file")
	}
	for _, s := range MCPServeToolSpecs() {
		if strings.HasPrefix(s.Name, "mcp__") {
			t.Errorf("listed %s", s.Name)
		}
	}
	if !strings.Contains(audit.all(), "write_file") || !strings.Contains(audit.all(), "gate=refused") {
		t.Fatalf("refusals not audited: %s", audit.all())
	}
}

// Spec (MS-1): a mutating call asks; deny is an MCP error and the tool
// never runs (no UI call past the tool lookup). Allow runs it.
func TestMCPServeConfirmGate(t *testing.T) {
	m := aiTestModel(t)
	var asked []string
	answer := error(&mcp.CallError{Code: mcp.CodeDeclined, Message: "declined by the user"})
	src, hub, audit := mcpServeTestSource(t.TempDir(), func(_ context.Context, client string, tool ai.Tool, human string) error {
		asked = append(asked, client+":"+tool.Name+":"+tool.Class)
		return answer
	})
	defer hub.stop()
	args := map[string]any{"title": "hello"}
	// notify is auto; tasks_run asks with class run.
	r, err, ui := mcpServeDrive(t, &m, hub, func() (mcp.CallResult, error) {
		return src.Call(mcpServeCtx("codex"), "tasks_run", map[string]any{"group": "build"})
	})
	var ce *mcp.CallError
	if !errors.As(err, &ce) || ce.Code != mcp.CodeDeclined || len(r.Content) != 0 {
		t.Fatalf("deny: %+v %v", r, err)
	}
	if ui != 1 || len(asked) != 1 || asked[0] != "codex:tasks_run:run" {
		t.Fatalf("deny ran the tool: ui=%d asked=%v", ui, asked)
	}
	if !strings.Contains(audit.all(), "by=mcp:codex tasks_run group=build gate=confirm → declined") {
		t.Fatalf("audit: %s", audit.all())
	}
	// A per-call gate: debug_breakpoint list is auto, clear asks.
	asked = nil
	mcpServeDrive(t, &m, hub, func() (mcp.CallResult, error) {
		return src.Call(mcpServeCtx("codex"), "debug_breakpoint", map[string]any{"op": "list"})
	})
	if len(asked) != 0 {
		t.Fatalf("list asked: %v", asked)
	}
	mcpServeDrive(t, &m, hub, func() (mcp.CallResult, error) {
		return src.Call(mcpServeCtx("codex"), "debug_breakpoint", map[string]any{"op": "clear"})
	})
	if len(asked) != 1 {
		t.Fatalf("clear did not ask: %v", asked)
	}
	// Allowed: the tool runs.
	answer = nil
	r, err, _ = mcpServeDrive(t, &m, hub, func() (mcp.CallResult, error) {
		return src.Call(mcpServeCtx("codex"), "notify", args)
	})
	if err != nil || mcpServeText(r) != "shown" {
		t.Fatalf("notify: %+v %v", r, err)
	}
}

// Spec (MS-1): bad arguments are refused before any card (-32602).
func TestMCPServeValidateBeforeConfirm(t *testing.T) {
	m := aiTestModel(t)
	src, hub, _ := mcpServeTestSource(t.TempDir(), func(context.Context, string, ai.Tool, string) error {
		t.Error("asked about an invalid call")
		return nil
	})
	defer hub.stop()
	for _, args := range []map[string]any{{"id": "save"}, {"id": "git-push"}, {"id": "no-such-id"}} {
		_, err, _ := mcpServeDrive(t, &m, hub, func() (mcp.CallResult, error) {
			return src.Call(mcpServeCtx("x"), "execute_action", args)
		})
		var ce *mcp.CallError
		if !errors.As(err, &ce) || ce.Code != mcp.CodeInvalidParams {
			t.Errorf("%v: %v", args, err)
		}
	}
}

// Spec (MS-1): a registered secret never leaves through a result or an
// error, and never reaches the audit line.
func TestMCPServeRedactsSecrets(t *testing.T) {
	m := aiTestModel(t)
	const secret = "hunter2-mcp-serve-secret"
	ai.RegisterSecret(secret)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("DB_PASSWORD="+secret+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src, hub, audit := mcpServeTestSource(root, nil)
	defer hub.stop()
	r, err, _ := mcpServeDrive(t, &m, hub, func() (mcp.CallResult, error) {
		return src.Call(mcpServeCtx("x"), "read_file", map[string]any{"path": ".env"})
	})
	if err != nil || strings.Contains(mcpServeText(r), secret) || !strings.Contains(mcpServeText(r), "[REDACTED]") {
		t.Fatalf("result: %+v %v", r, err)
	}
	r, err, _ = mcpServeDrive(t, &m, hub, func() (mcp.CallResult, error) {
		return src.Call(mcpServeCtx("x"), "read_file", map[string]any{"path": secret + ".txt"})
	})
	if err != nil {
		var ce *mcp.CallError
		if errors.As(err, &ce) && strings.Contains(ce.Message, secret) {
			t.Fatalf("error leaked the secret: %v", ce.Message)
		}
	} else if strings.Contains(mcpServeText(r), secret) {
		t.Fatalf("error result leaked the secret: %+v", r)
	}
	if strings.Contains(audit.all(), secret) {
		t.Fatalf("audit leaked the secret: %s", audit.all())
	}
}

// Spec (MS-1): the client name is cleaned for the audit and the card.
func TestMCPServeClientName(t *testing.T) {
	for in, want := range map[string]string{
		"claude-code":                     "claude-code",
		"Claude Code":                     "Claude-Code",
		"":                                "client",
		"\x1b[31mevil\x1b[0m by=user":     "31mevil0m-byuser",
		"a\nb":                            "ab",
		strings.Repeat("x", 40):           strings.Repeat("x", 24),
		"-- ..":                           "client",
		"name=with spaces and = signs!!!": "namewith-spaces-and--sig",
	} {
		if got := mcpServeClient(in); got != want {
			t.Errorf("mcpServeClient(%q) = %q, want %q", in, got, want)
		}
	}
}

// Spec (MS-1): a stopped hub refuses calls instead of hanging.
func TestMCPServeStoppedHub(t *testing.T) {
	src, hub, _ := mcpServeTestSource(t.TempDir(), nil)
	hub.stop()
	_, err := src.Call(mcpServeCtx("x"), "notify", map[string]any{"title": "x"})
	if err == nil {
		t.Fatal("a stopped hub ran a call")
	}
}
