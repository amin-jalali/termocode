package ai

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRetryBackoffDoubles(t *testing.T) {
	for n, want := range map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second} {
		if got := retryBackoff(n); got != want {
			t.Errorf("retryBackoff(%d) = %v, want %v", n, got, want)
		}
	}
}

// Without a RetryWait hook the agent waits the real backoff, and a stop
// during that pause ends the turn at once.
func TestAgentDefaultRetryWaitStopsOnCancel(t *testing.T) {
	p := &flaky{fails: 99, code: http.StatusServiceUnavailable}
	a := &Agent{Provider: p, Native: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var evs []Event
	start := time.Now()
	a.Run(ctx, []Message{{Role: RoleUser, Content: "hi"}}, func(e Event) {
		evs = append(evs, e)
		if e.Kind == EventRetry {
			cancel()
		}
	})
	if p.calls != 1 {
		t.Errorf("calls = %d", p.calls)
	}
	last := evs[len(evs)-1]
	if last.Kind != EventError || !strings.Contains(last.Detail, "canceled") {
		t.Errorf("last event = %+v", last)
	}
	if time.Since(start) > 900*time.Millisecond {
		t.Errorf("the stop waited out the pause: %v", time.Since(start))
	}
}

func TestAgentDefaultRetryWaitThenSucceeds(t *testing.T) {
	if testing.Short() {
		t.Skip("waits the 1 s backoff")
	}
	p := &flaky{fails: 1, code: http.StatusTooManyRequests}
	a := &Agent{Provider: p, Native: true}
	start := time.Now()
	evs, _ := collectEvents(a, []Message{{Role: RoleUser, Content: "hi"}})
	if p.calls != 2 || evs[len(evs)-1].Kind != EventDone {
		t.Fatalf("calls %d events %v", p.calls, kinds(evs))
	}
	if time.Since(start) < 900*time.Millisecond {
		t.Errorf("no pause before the retry: %v", time.Since(start))
	}
}

func TestSystemPromptHints(t *testing.T) {
	if got := SystemPrompt("base", nil, true); got != "base" {
		t.Errorf("no tools: %q", got)
	}
	for tool, hint := range map[string]string{
		"terminal_send":   "terminal_read(wait_ms)",
		"tests_run":       "tests_results(wait_ms)",
		"debug_start":     "debug_breakpoint",
		"lsp_servers":     "dap_adapters",
		"http_send":       "http-client.private.env.json",
		"mcp__gh__issues": "external MCP servers",
	} {
		got := SystemPrompt("base", []Tool{{Name: tool}}, true)
		if !strings.Contains(got, hint) {
			t.Errorf("%s: hint %q missing", tool, hint)
		}
		if other := SystemPrompt("base", []Tool{{Name: "read_file"}}, true); strings.Contains(other, hint) {
			t.Errorf("%s hint shown without the tool", tool)
		}
	}
}

// chunky streams its reply in several deltas.
type chunky struct{ parts []string }

func (c *chunky) Name() string        { return "chunky" }
func (c *chunky) Configured() bool    { return true }
func (c *chunky) SupportsTools() bool { return false }
func (c *chunky) Stream(context.Context, Request) <-chan Chunk {
	ch := make(chan Chunk, len(c.parts)+1)
	for _, p := range c.parts {
		ch <- Chunk{Delta: p}
	}
	ch <- Chunk{Done: true}
	close(ch)
	return ch
}

func textOf(evs []Event) string {
	var b strings.Builder
	for _, e := range evs {
		if e.Kind == EventText {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}

func TestAgentTextProtocolStreamsProse(t *testing.T) {
	a := &Agent{Provider: &chunky{parts: []string{"  ", "Hello", " world"}}, Tools: []Tool{{Name: "read_file"}}}
	evs, _ := collectEvents(a, []Message{{Role: RoleUser, Content: "hi"}})
	if got := textOf(evs); got != "Hello world" {
		t.Errorf("text = %q", got)
	}
}

// A reply that looks like JSON but is no tool call is still shown.
func TestAgentTextProtocolShowsJSONAnswer(t *testing.T) {
	a := &Agent{Provider: &chunky{parts: []string{`{"name": `, `"value"}`}}, Tools: []Tool{{Name: "read_file"}}}
	evs, _ := collectEvents(a, []Message{{Role: RoleUser, Content: "give json"}})
	if got := textOf(evs); got != `{"name": "value"}` {
		t.Errorf("text = %q", got)
	}
	if evs[len(evs)-1].Kind != EventDone {
		t.Errorf("events %v", kinds(evs))
	}
}

func TestAgentStopDuringToolEndsTurn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tool := Tool{Name: "slow", Run: func(context.Context, map[string]any) (string, error) { cancel(); return "partial", nil }}
	p := &scripted{tools: true, replies: []Chunk{{ToolCalls: []ToolCall{{ID: "1", Name: "slow"}}}}}
	a := &Agent{Provider: p, Native: true, Tools: []Tool{tool}}
	var evs []Event
	added := a.Run(ctx, []Message{{Role: RoleUser, Content: "go"}}, func(e Event) { evs = append(evs, e) })
	if p.calls != 1 {
		t.Errorf("the model was asked again after stop: %d calls", p.calls)
	}
	if last := evs[len(evs)-1]; last.Kind != EventError {
		t.Errorf("last event = %+v", last)
	}
	// The tool result is kept so the chat can go on later.
	if len(added) != 2 || added[1].Content != "partial" {
		t.Errorf("added = %+v", added)
	}
}

func TestAgentToolPanicAndErrorReachModel(t *testing.T) {
	secret := "panic-secret-value-123"
	RegisterSecret(secret)
	tools := []Tool{
		{Name: "boom", Run: func(context.Context, map[string]any) (string, error) { panic("nil map") }},
		{Name: "fail", Run: func(context.Context, map[string]any) (string, error) { return "", errors.New("auth " + secret) }},
	}
	p := &scripted{tools: true, replies: []Chunk{{ToolCalls: []ToolCall{{ID: "1", Name: "boom"}, {ID: "2", Name: "fail"}}}}}
	a := &Agent{Provider: p, Native: true, Tools: tools}
	evs, added := collectEvents(a, []Message{{Role: RoleUser, Content: "go"}})
	if evs[len(evs)-1].Kind != EventDone {
		t.Fatalf("turn did not go on: %v", kinds(evs))
	}
	if added[1].Content != "error: nil map" {
		t.Errorf("panic result = %q", added[1].Content)
	}
	if added[2].Content != "error: auth [REDACTED]" {
		t.Errorf("error result = %q", added[2].Content)
	}
}

func TestAgentUnknownToolListsTools(t *testing.T) {
	p := &scripted{tools: true, replies: []Chunk{{ToolCalls: []ToolCall{{ID: "1", Name: "nope"}}}}}
	a := &Agent{Provider: p, Native: true, Tools: []Tool{{Name: "read_file"}, {Name: "search"}}}
	_, added := collectEvents(a, []Message{{Role: RoleUser, Content: "go"}})
	if want := `tool "nope" does not exist. Available: read_file, search.`; added[1].Content != want {
		t.Errorf("result = %q", added[1].Content)
	}
}

func TestParseTextToolCallRejectsBadJSON(t *testing.T) {
	for _, reply := range []string{
		"```json\n{\"tool\": \"x\", \"args\": [}\n```",
		"```json\n{\"args\": {}}\n```",
		"plain words",
	} {
		if _, ok := ParseTextToolCall(reply); ok {
			t.Errorf("parsed %q", reply)
		}
	}
	tc, ok := ParseTextToolCall(`{"tool": "git_status"}`)
	if !ok || tc.Name != "git_status" || tc.Args == nil {
		t.Errorf("naked call without args = %+v, %v", tc, ok)
	}
}

func TestFirstCodeBlockEdges(t *testing.T) {
	if _, ok := FirstCodeBlock("no fence here"); ok {
		t.Error("no fence")
	}
	if _, ok := FirstCodeBlock("text ```go"); ok {
		t.Error("a fence with no line after it is no block")
	}
	if got, ok := FirstCodeBlock("```go\nx := 1\n"); !ok || got != "x := 1" {
		t.Errorf("unclosed block = %q, %v", got, ok)
	}
}
