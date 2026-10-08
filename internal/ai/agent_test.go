package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// scripted is a fake Provider replaying one canned reply per call.
type scripted struct {
	replies []Chunk // Delta = full text; ToolCalls on the same chunk
	calls   int
	seen    [][]Message
	tools   bool
}

func (s *scripted) Name() string        { return "fake" }
func (s *scripted) Configured() bool    { return true }
func (s *scripted) SupportsTools() bool { return s.tools }
func (s *scripted) Stream(_ context.Context, req Request) <-chan Chunk {
	s.seen = append(s.seen, req.Messages)
	ch := make(chan Chunk, 4)
	r := Chunk{Delta: "done"}
	if s.calls < len(s.replies) {
		r = s.replies[s.calls]
	}
	s.calls++
	ch <- Chunk{Delta: r.Delta}
	ch <- Chunk{Done: true, ToolCalls: r.ToolCalls, Err: r.Err}
	close(ch)
	return ch
}

func collectEvents(a *Agent, msgs []Message) ([]Event, []Message) {
	var evs []Event
	added := a.Run(context.Background(), msgs, func(e Event) { evs = append(evs, e) })
	return evs, added
}

func TestAgentTextProtocolRunsReadTool(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello file"), 0o644)
	p := &scripted{replies: []Chunk{
		{Delta: "```json\n{\"tool\":\"read_file\",\"args\":{\"path\":\"a.txt\"}}\n```"},
		{Delta: "The file says hello."},
	}}
	a := &Agent{Provider: p, Tools: WorkspaceTools(Workspace{Root: dir})}
	evs, added := collectEvents(a, []Message{{Role: RoleUser, Content: "read a.txt"}})
	var text strings.Builder
	sawResult := false
	for _, e := range evs {
		if e.Kind == EventText {
			text.WriteString(e.Text)
		}
		if e.Kind == EventToolResult && e.Detail == "hello file" {
			sawResult = true
		}
	}
	if !sawResult || text.String() != "The file says hello." || strings.Contains(text.String(), "tool") {
		t.Fatalf("events: %+v", evs)
	}
	if evs[len(evs)-1].Kind != EventDone || len(added) != 3 {
		t.Fatalf("added=%d last=%+v", len(added), evs[len(evs)-1])
	}
	if !strings.Contains(p.seen[1][len(p.seen[1])-1].Content, "hello file") {
		t.Error("tool result not fed back")
	}
}

func TestAgentConfirmGateAndAutoApprove(t *testing.T) {
	dir := t.TempDir()
	call := ToolCall{ID: "c1", Name: "write_file", Args: map[string]any{"path": "x.txt", "content": "hi"}}
	mk := func() *scripted {
		return &scripted{tools: true, replies: []Chunk{{ToolCalls: []ToolCall{call}}, {Delta: "ok"}}}
	}
	asked := 0
	a := &Agent{Provider: mk(), Native: true, Tools: WorkspaceTools(Workspace{Root: dir}),
		Confirm: func(context.Context, Tool, string) bool { asked++; return false }}
	evs, added := collectEvents(a, []Message{{Role: RoleUser, Content: "write"}})
	if asked != 1 {
		t.Fatalf("confirm asked %d times", asked)
	}
	if _, err := os.Stat(filepath.Join(dir, "x.txt")); err == nil {
		t.Fatal("declined write must not happen")
	}
	if added[1].Role != RoleTool || added[1].ToolCallID != "c1" || !strings.Contains(added[1].Content, "declined") {
		t.Fatalf("declined result: %+v", added[1])
	}
	_ = evs

	a = &Agent{Provider: mk(), Native: true, Tools: WorkspaceTools(Workspace{Root: dir}),
		AutoApprove: map[string]bool{ClassFileWrite: true},
		Confirm:     func(context.Context, Tool, string) bool { t.Fatal("auto-approved class must not ask"); return false }}
	collectEvents(a, []Message{{Role: RoleUser, Content: "write"}})
	if b, _ := os.ReadFile(filepath.Join(dir, "x.txt")); string(b) != "hi" {
		t.Fatal("auto-approved write did not happen")
	}
}

func TestAgentStepCapAndUnknownTool(t *testing.T) {
	loop := Chunk{ToolCalls: []ToolCall{{ID: "z", Name: "nope"}}}
	p := &scripted{tools: true, replies: []Chunk{loop, loop, loop, loop, loop, loop, loop}}
	a := &Agent{Provider: p, Native: true}
	evs, _ := collectEvents(a, nil)
	if p.calls != DefaultMaxSteps || evs[len(evs)-1].Kind != EventDone {
		t.Fatalf("calls=%d", p.calls)
	}
	if !strings.Contains(p.seen[1][1].Content, "does not exist") {
		t.Errorf("unknown tool message: %+v", p.seen[1])
	}
}

func TestAgentError(t *testing.T) {
	p := &scripted{replies: []Chunk{{Err: fmt.Errorf("boom sk-ant-abcdefghijklmnop")}}}
	evs, _ := collectEvents(&Agent{Provider: p}, nil)
	last := evs[len(evs)-1]
	if last.Kind != EventError || strings.Contains(last.Detail, "abcdefghijkl") {
		t.Fatalf("error event %+v", last)
	}
}

func TestParseTextToolCallAndCodeBlock(t *testing.T) {
	if c, ok := ParseTextToolCall(`{"tool":"search","args":{"query":"x"}}`); !ok || c.Name != "search" || c.Args["query"] != "x" {
		t.Error("naked object")
	}
	if _, ok := ParseTextToolCall("just prose"); ok {
		t.Error("prose is not a call")
	}
	if b, ok := FirstCodeBlock("text\n```go\nfunc a() {}\n```\nmore"); !ok || b != "func a() {}" {
		t.Errorf("code block %q", b)
	}
}

func TestWorkspaceResolveAndRun(t *testing.T) {
	w := Workspace{Root: t.TempDir()}
	if _, err := w.Resolve("../etc/passwd"); err != ErrOutsideWorkspace {
		t.Error("escape not blocked")
	}
	if _, err := w.Resolve("/etc/passwd"); err != ErrOutsideWorkspace {
		t.Error("absolute escape not blocked")
	}
	tools := WorkspaceTools(w)
	run, _ := FindTool(tools, "run_command")
	out, err := run.Run(context.Background(), map[string]any{"command": "echo hi; exit 3"})
	if err != nil || !strings.Contains(out, "hi") || !strings.Contains(out, "exit status 3") {
		t.Fatalf("run: %q %v", out, err)
	}
	if !strings.Contains(ToolsPromptSpec(tools), `read_file {"path": string}`) {
		t.Errorf("prompt spec:\n%s", ToolsPromptSpec(tools))
	}
	del, _ := FindTool(tools, "delete")
	if _, err := del.Run(context.Background(), map[string]any{"path": "."}); err == nil {
		t.Error("deleting the root must fail")
	}
}

func TestOAuthPKCEAndExchange(t *testing.T) {
	p, err := NewPKCE()
	if err != nil || p.Challenge != S256Challenge(p.Verifier) || strings.ContainsAny(p.Challenge, "+/=") {
		t.Fatalf("pkce %+v %v", p, err)
	}
	if !strings.Contains(p.AuthorizeURL(), "code_challenge_method=S256") || !strings.Contains(p.AuthorizeURL(), "client_id="+ClaudeOAuthClientID) {
		t.Error("authorize url")
	}
	for in, want := range map[string][2]string{
		"https://x/cb?code=abc&state=st": {"abc", "st"},
		"abc#st":                         {"abc", "st"},
		"abc":                            {"abc", ""},
	} {
		c, s, ok := ParseCallback(in)
		if !ok || c != want[0] || s != want[1] {
			t.Errorf("%q → %q %q %v", in, c, s, ok)
		}
	}
	if _, _, ok := ParseCallback("two words"); ok {
		t.Error("spaces are not a code")
	}

	var grants atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		grants.Add(1)
		switch body["grant_type"] {
		case "authorization_code":
			if body["code_verifier"] != "ver" {
				w.WriteHeader(400)
				return
			}
			fmt.Fprint(w, `{"access_token":"acc-1-abcdefgh","refresh_token":"ref-1-abcdefgh","expires_in":3600}`)
		case "refresh_token":
			fmt.Fprint(w, `{"access_token":"acc-2-abcdefgh","expires_in":3600}`)
		}
	}))
	defer srv.Close()
	now := time.Unix(1_000_000, 0)
	cl := OAuthClient{TokenURL: srv.URL, Now: func() time.Time { return now }}
	tok, err := cl.Exchange(context.Background(), "abc", "st", "ver")
	if err != nil || tok.AccessToken != "acc-1-abcdefgh" {
		t.Fatalf("exchange %+v %v", tok, err)
	}
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := SaveCredentials(path, Credentials{ClaudeOAuth: &tok}); err != nil {
		t.Fatal(err)
	}
	src := &TokenSource{Path: path, Client: cl}
	if got, _ := src.Token(context.Background()); got != "acc-1-abcdefgh" {
		t.Fatalf("fresh token %q", got)
	}
	now = now.Add(2 * time.Hour)
	got, err := src.Token(context.Background())
	if err != nil || got != "acc-2-abcdefgh" {
		t.Fatalf("refresh %q %v", got, err)
	}
	creds, _ := LoadCredentials(path)
	if creds.ClaudeOAuth.RefreshToken != "ref-1-abcdefgh" {
		t.Error("refresh token must be kept when the server omits it")
	}
	if grants.Load() != 2 {
		t.Errorf("grants %d", grants.Load())
	}
}
