package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/aimemory"
	"github.com/amin-jalali/termocode/internal/clock"
)

// The request head is what every chat request sends before the
// conversation: the system prompt and the tool schemas. It is paid on
// every step of every turn, and a head that changes between turns
// misses the provider's prompt cache.

// Budgets of the request head of a plain chat turn (default profile, no
// MCP servers, the test machine's pinned env). They are the measured
// numbers: lower them when a change makes the head smaller, never raise
// them.
const (
	aiHeadToolsBudget  = 15   // tool schemas sent
	aiHeadTokensBudget = 5650 // estimated tokens of the system prompt and the tool schemas
)

// aiHead is the size of one request's head.
type aiHead struct {
	tools       int
	schemaBytes int
	systemBytes int
}

// tokens estimates tokens as bytes/4 (mcp.SpecTokens does the same).
func (h aiHead) schemaTokens() int { return h.schemaBytes / 4 }
func (h aiHead) systemTokens() int { return h.systemBytes / 4 }
func (h aiHead) tokens() int       { return h.schemaTokens() + h.systemTokens() }

// aiRequestHead measures r's head: each tool as the provider sends it
// (name, description, input schema) and the system message.
func aiRequestHead(r ai.Request) aiHead {
	h := aiHead{tools: len(r.Tools)}
	for _, t := range r.Tools {
		b, _ := json.Marshal(map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Schema})
		h.schemaBytes += len(b)
	}
	if len(r.Messages) > 0 && r.Messages[0].Role == ai.RoleSystem {
		h.systemBytes = len(r.Messages[0].Content)
	}
	return h
}

// aiHeadToolNames are the names of r's tools, in order.
func aiHeadToolNames(r ai.Request) string {
	names := make([]string, len(r.Tools))
	for i, t := range r.Tools {
		names[i] = t.Name
	}
	return strings.Join(names, ",")
}

// aiLinesOnlyIn are the lines of a that b does not have.
func aiLinesOnlyIn(a, b string) []string {
	have := map[string]bool{}
	for _, l := range strings.Split(b, "\n") {
		have[l] = true
	}
	var out []string
	for _, l := range strings.Split(a, "\n") {
		if !have[l] && strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// aiHeadTestModel is a chat on the default profile with the machine
// pinned like the default-turn golden (no container engine, a fixed env
// block), so the head is the same on every machine.
func aiHeadTestModel(t *testing.T, p ai.Provider) (*Model, string) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	old := aiEnvInfo
	aiEnvInfo = func() ai.EnvInfo {
		return ai.EnvInfo{Now: clock.Now(), GOOS: "linux", GOARCH: "amd64", Shell: "/bin/bash"}
	}
	t.Cleanup(func() { aiEnvInfo = old })
	m, dir := aiMemoryModel(t, p, nil, aiMemoryRead)
	t.Cleanup(clock.Set(func() time.Time { return time.Date(2026, 10, 18, 9, 5, 0, 0, time.UTC) }))
	return m, dir
}

// aiHeadTwoTurns runs two chat turns; between them the todo list and
// the memory lessons change, the way they do mid-chat (todo_write, a
// lesson learned after turn 1). It returns the first request of each.
func aiHeadTwoTurns(t *testing.T) (ai.Request, ai.Request) {
	t.Helper()
	p := &recordingAIProvider{replies: []ai.Chunk{{Delta: "one"}, {Delta: "two"}}}
	m, _ := aiHeadTestModel(t, p)
	m.ai.memFake = &[]aimemory.Lesson{{ID: "a", Date: "2026-10-01", Title: "Build", Text: "Run make build"}}
	m.ai.conv.KeepGoingOff = true // the open todo must not push a turn of its own
	m.ai.conv.Todos = []ai.Todo{{Text: "read the code", Status: ai.TodoActive}}
	aiHeadSend(t, m, "hello")
	*m.ai.memFake = append(*m.ai.memFake, aimemory.Lesson{ID: "b", Date: "2026-10-02", Title: "Tests", Text: "Run make test"})
	m.ai.conv.Todos = []ai.Todo{{Text: "read the code", Status: ai.TodoDone}, {Text: "fix the bug", Status: ai.TodoActive}}
	aiHeadSend(t, m, "go on")
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.reqs) != 2 {
		t.Fatalf("requests %d, want 2", len(p.reqs))
	}
	return p.reqs[0], p.reqs[1]
}

// aiHeadSend runs one chat turn of text.
func aiHeadSend(t *testing.T, m *Model, text string) {
	t.Helper()
	cmd := m.sendAIChat(text)
	if !m.ai.streaming {
		t.Fatalf("turn %q did not start: %+v", text, m.toast.Live())
	}
	driveAIChat(t, m, cmd, nil)
}

// aiHeadCLIStepTools are the termocode tools the MCP server of a CLI
// step (Codex, Claude Code) serves with the owner's profile shape: one
// codex step with `tools: {}` (no preset: every tool of a run step).
func aiHeadCLIStepTools(t *testing.T) aiHead {
	t.Helper()
	m, root := aiHeadTestModel(t, &recordingAIProvider{})
	ts := aiFilterTools(m.aiChatToolsBy(root, newAIIDECaller(1, make(chan tea.Msg, 1)), aiRunCaller{}), agentdag.ToolRule{})
	var r ai.Request
	for _, s := range aiStepMCPSpecs(ts) {
		r.Tools = append(r.Tools, ai.ToolSpec{Name: s.Name, Description: s.Description, Schema: s.InputSchema})
	}
	return aiRequestHead(r)
}

// TestAIRequestHeadReport prints the head of a plain chat request and
// of a CLI step's tool server (go test -run TestAIRequestHeadReport -v),
// and which system prompt lines change between two turns.
func TestAIRequestHeadReport(t *testing.T) {
	r1, r2 := aiHeadTwoTurns(t)
	h := aiRequestHead(r1)
	t.Logf("chat request head: %d tools, schemas %d bytes (~%d tokens), system prompt %d bytes (~%d tokens), head ~%d tokens",
		h.tools, h.schemaBytes, h.schemaTokens(), h.systemBytes, h.systemTokens(), h.tokens())
	t.Logf("tools: %s", aiHeadToolNames(r1))
	for _, tl := range r1.Tools {
		if tl.Name == ai.ToolSearchTool {
			t.Logf("tool_search: %s", tl.Description)
		}
	}
	sys1, sys2 := r1.Messages[0].Content, r2.Messages[0].Content
	t.Logf("system prompt same in turn 2: %v; tool list same: %v", sys1 == sys2, aiHeadToolNames(r1) == aiHeadToolNames(r2))
	for _, l := range aiLinesOnlyIn(sys1, sys2) {
		t.Logf("  turn 1 only: %s", l)
	}
	for _, l := range aiLinesOnlyIn(sys2, sys1) {
		t.Logf("  turn 2 only: %s", l)
	}
	c := aiHeadCLIStepTools(t)
	t.Logf("CLI step MCP server: %d tools, schemas %d bytes (~%d tokens)", c.tools, c.schemaBytes, c.schemaTokens())
}

// Spec (lean request): the head of a plain chat request does not grow.
// Its tool count and its estimated tokens stay at or under the budgets
// above; a change that adds to them needs a reason and a smaller head
// somewhere else.
func TestAIRequestHeadBudget(t *testing.T) {
	r1, _ := aiHeadTwoTurns(t)
	h := aiRequestHead(r1)
	if h.tools > aiHeadToolsBudget {
		t.Errorf("the chat request sends %d tools, budget %d", h.tools, aiHeadToolsBudget)
	}
	if h.tokens() > aiHeadTokensBudget {
		t.Errorf("the chat request head is ~%d tokens (schemas %d, system %d), budget %d",
			h.tokens(), h.schemaTokens(), h.systemTokens(), aiHeadTokensBudget)
	}
}
