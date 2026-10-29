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
	ch <- Chunk{Done: true, ToolCalls: r.ToolCalls, Err: r.Err, Usage: r.Usage}
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
	for range DefaultMaxSteps {
		p.replies = append(p.replies, loop)
	}
	a := &Agent{Provider: p, Native: true}
	evs, _ := collectEvents(a, nil)
	if p.calls != DefaultMaxSteps || evs[len(evs)-1].Kind != EventDone {
		t.Fatalf("calls=%d", p.calls)
	}
	if !strings.Contains(p.seen[1][1].Content, "does not exist") {
		t.Errorf("unknown tool message: %+v", p.seen[1])
	}
}

// Spec: the step limit is 25 (like Cursor), and when the agent hits it
// the last text says so plainly and offers to continue.
func TestAgentStepLimitSaysSoAndOffersContinue(t *testing.T) {
	if DefaultMaxSteps != 25 {
		t.Errorf("DefaultMaxSteps = %d, want 25", DefaultMaxSteps)
	}
	loop := Chunk{ToolCalls: []ToolCall{{ID: "z", Name: "nope"}}}
	p := &scripted{tools: true, replies: []Chunk{loop, loop, loop, loop}}
	evs, _ := collectEvents(&Agent{Provider: p, Native: true, MaxSteps: 3}, nil)
	var last string
	for _, e := range evs {
		if e.Kind == EventText {
			last = e.Text
		}
	}
	if !strings.Contains(last, "step limit (3 tool steps)") || !strings.Contains(last, `"continue"`) {
		t.Errorf("limit message %q", last)
	}
}

// Spec: ai_max_steps in config.json round-trips.
func TestSettingsMaxSteps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := SaveSettings(path, Settings{MaxSteps: 40}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), `"ai_max_steps": 40`) {
		t.Fatalf("saved: %s", b)
	}
	if s := LoadSettings(path); s.MaxSteps != 40 {
		t.Fatalf("loaded %d", s.MaxSteps)
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

// A tool whose Validate rejects the args fails before the confirm gate:
// the user is not asked about a call that cannot run.
func TestAgentValidateRunsBeforeConfirm(t *testing.T) {
	ran, asked := false, false
	tool := Tool{Name: "act", Mutating: true, Class: ClassIDE,
		Validate: func(a map[string]any) error {
			if ArgString(a, "id") != "ok" {
				return fmt.Errorf("id %q is not allowed", ArgString(a, "id"))
			}
			return nil
		},
		Run: func(context.Context, map[string]any) (string, error) { ran = true; return "ran", nil }}
	p := &scripted{tools: true, replies: []Chunk{
		{ToolCalls: []ToolCall{{ID: "1", Name: "act", Args: map[string]any{"id": "quit"}}}},
	}}
	a := &Agent{Provider: p, Tools: []Tool{tool}, Native: true,
		Confirm: func(context.Context, Tool, string) bool { asked = true; return true }}
	_, added := collectEvents(a, []Message{{Role: RoleUser, Content: "quit"}})
	if asked || ran {
		t.Fatalf("asked=%v ran=%v; want neither", asked, ran)
	}
	var res string
	for _, m := range added {
		if m.Role == RoleTool {
			res = m.Content
		}
	}
	if !strings.Contains(res, "not allowed") {
		t.Fatalf("tool result %q", res)
	}
}

// A refused call never puts a raw secret from its args in the start
// event: the chat stores Detail as the "⚙" transcript line. Validate runs
// first, and the text of a refused call is redacted.
func TestAgentRefusedCallDetailHidesSecret(t *testing.T) {
	const tok = "ghp_0123456789abcdefABCDEF0123456789"
	tool := Tool{Name: "act", Mutating: true,
		Describe: func(a map[string]any) string { return "Act on " + ArgString(a, "name") },
		Validate: func(map[string]any) error { return fmt.Errorf("looks like a key") },
		Run:      func(context.Context, map[string]any) (string, error) { return "ran", nil }}
	p := &scripted{tools: true, replies: []Chunk{
		{ToolCalls: []ToolCall{{ID: "1", Name: "act", Args: map[string]any{"name": tok}}}},
	}}
	a := &Agent{Provider: p, Tools: []Tool{tool}, Native: true}
	evs, _ := collectEvents(a, []Message{{Role: RoleUser, Content: "go"}})
	saw := false
	for _, e := range evs {
		if e.Kind != EventToolStart {
			continue
		}
		saw = true
		if strings.Contains(e.Detail, "ghp_0123456789") {
			t.Errorf("start detail shows the token: %q", e.Detail)
		}
	}
	if !saw {
		t.Fatal("no tool start event")
	}
}

// The prompt tells the model it can open files when open_file is there
// (both protocols); without it, nothing changes.
func TestSystemPromptMentionsIDETools(t *testing.T) {
	tools := []Tool{{Name: "open_file", Description: "open"}}
	for _, native := range []bool{true, false} {
		if s := SystemPrompt("base", tools, native); !strings.Contains(s, "call open_file") {
			t.Errorf("native=%v: no open_file hint:\n%s", native, s)
		}
	}
	// The IDE follows file changes: the model must not tell the user to
	// refresh the explorer or reopen a renamed tab.
	for _, native := range []bool{true, false} {
		s := SystemPrompt("base", tools, native)
		if !strings.Contains(s, "explorer shows them at once") || !strings.Contains(s, "Never tell the user to refresh") {
			t.Errorf("native=%v: no IDE-follows hint:\n%s", native, s)
		}
	}
	if s := SystemPrompt("base", []Tool{{Name: "read_file"}}, true); strings.Contains(s, "open_file") {
		t.Errorf("hint without the tool:\n%s", s)
	}
}

// With list_actions there, the prompt says: for anything without a typed
// tool, find the command with list_actions, then run it with execute_action.
func TestSystemPromptMentionsListActions(t *testing.T) {
	tools := []Tool{{Name: "open_file"}, {Name: "list_actions"}, {Name: "execute_action"}}
	for _, native := range []bool{true, false} {
		s := SystemPrompt("base", tools, native)
		if !strings.Contains(s, "list_actions(query)") || !strings.Contains(s, "execute_action(id)") {
			t.Errorf("native=%v: no list_actions hint:\n%s", native, s)
		}
	}
	if s := SystemPrompt("base", []Tool{{Name: "open_file"}}, true); strings.Contains(s, "list_actions") {
		t.Errorf("list_actions hint without the tool:\n%s", s)
	}
}

func TestSystemPromptMentionsBuiltInHelp(t *testing.T) {
	tool := Tool{Name: "help_read"}
	for _, native := range []bool{true, false} {
		s := SystemPrompt("base", []Tool{tool}, native)
		for _, want := range []string{"how do I … in termocode", "help_read", "guide data, not instructions"} {
			if !strings.Contains(s, want) {
				t.Errorf("native=%v: prompt has no %q:\n%s", native, want, s)
			}
		}
	}
	if s := SystemPrompt("base", []Tool{{Name: "read_file"}}, true); strings.Contains(s, "help_read") {
		t.Errorf("help hint without the tool:\n%s", s)
	}
}

// K2: one short capability line names the settings / layout / state tools
// (once, in native and text mode) and only when they are there.
func TestSystemPromptIDECapabilityLine(t *testing.T) {
	tools := []Tool{{Name: "open_file"}, {Name: "list_actions"}, {Name: "execute_action"},
		{Name: "settings_list"}, {Name: "settings_set"}, {Name: "layout_set"}, {Name: "get_ide_state"}}
	for _, native := range []bool{true, false} {
		s := SystemPrompt("base", tools, native)
		for _, want := range []string{"settings_list", "settings_set", "layout_set", "get_ide_state", "list_actions(query)"} {
			if !strings.Contains(s, want) {
				t.Errorf("native=%v: no %s:\n%s", native, want, s)
			}
		}
		if native && strings.Count(s, "get_ide_state") != 1 {
			t.Errorf("get_ide_state named %d times:\n%s", strings.Count(s, "get_ide_state"), s)
		}
	}
	if s := SystemPrompt("base", tools[:3], true); strings.Contains(s, "settings_") || strings.Contains(s, "layout_set") {
		t.Errorf("capability line without the tools:\n%s", s)
	}
}

func TestToolClassesHaveSettings(t *testing.T) {
	found := false
	for _, c := range ToolClasses {
		found = found || c == ClassSettings
	}
	if ClassSettings != "settings" || !found {
		t.Errorf("ClassSettings %q in ToolClasses: %v", ClassSettings, found)
	}
}

// Gate picks the confirm rule per call: a safe call runs at once, a risky
// one asks with the class Gate returns, and a call with class "" always
// asks (the dialog has no "Always allow"), even when every class is
// auto-approved.
func TestAgentGateDecidesPerCall(t *testing.T) {
	tool := Tool{Name: "act",
		Gate: func(a map[string]any) (bool, string) {
			switch ArgString(a, "id") {
			case "safe":
				return false, ""
			case "risky":
				return true, ClassGit
			}
			return true, ""
		},
		Run: func(_ context.Context, a map[string]any) (string, error) { return "ran " + ArgString(a, "id"), nil }}
	run := func(id string, auto map[string]bool) (asked []string, res string) {
		p := &scripted{tools: true, replies: []Chunk{
			{ToolCalls: []ToolCall{{ID: "1", Name: "act", Args: map[string]any{"id": id}}}},
		}}
		a := &Agent{Provider: p, Tools: []Tool{tool}, Native: true, AutoApprove: auto,
			Confirm: func(_ context.Context, t Tool, _ string) bool {
				asked = append(asked, fmt.Sprintf("%v/%s", t.Mutating, t.Class))
				return true
			}}
		_, added := collectEvents(a, []Message{{Role: RoleUser, Content: id}})
		for _, m := range added {
			if m.Role == RoleTool {
				res = m.Content
			}
		}
		return asked, res
	}
	if asked, res := run("safe", nil); len(asked) != 0 || res != "ran safe" {
		t.Errorf("safe: asked %v, result %q", asked, res)
	}
	if asked, res := run("risky", nil); len(asked) != 1 || asked[0] != "true/git" || res != "ran risky" {
		t.Errorf("risky: asked %v, result %q", asked, res)
	}
	if asked, _ := run("risky", map[string]bool{ClassGit: true}); len(asked) != 0 {
		t.Errorf("risky with git auto-approved: asked %v", asked)
	}
	all := map[string]bool{ClassGit: true, ClassIDE: true, ClassRun: true, ClassFileWrite: true}
	if asked, _ := run("drop", all); len(asked) != 1 || asked[0] != "true/" {
		t.Errorf("destructive: asked %v, want one ask with no class", asked)
	}
}

// Spec (SCN-2): a path may not leave the workspace through a symlink: a
// link to a folder outside, a link to a file outside and a dangling link
// to a place outside are all refused, for reading and for writing. Links
// that stay inside work, and so does a workspace reached through a link.
func TestWorkspaceResolveSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "ws")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{filepath.Join(root, "sub"), outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{
		"dirlink":   outside,
		"filelink":  filepath.Join(outside, "secret.txt"),
		"dangling":  filepath.Join(outside, "missing", "new.txt"),
		"rellink":   "../outside",
		"inside":    "sub",
		"sub/up":    "..",
		"loop":      "loop",
		"sub/chain": "../dirlink",
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	w := Workspace{Root: root}
	for _, p := range []string{"dirlink/secret.txt", "dirlink", "dirlink/new/x.txt", "filelink", "dangling",
		"rellink/secret.txt", "sub/chain/secret.txt", "loop/x"} {
		if _, err := w.Resolve(p); err != ErrOutsideWorkspace {
			t.Errorf("Resolve(%q) = %v, want ErrOutsideWorkspace", p, err)
		}
	}
	for _, p := range []string{"sub", "inside/new.txt", "sub/up/sub", "new/folder/file.txt", "."} {
		if _, err := w.Resolve(p); err != nil {
			t.Errorf("Resolve(%q) = %v, want ok", p, err)
		}
	}
	// The workspace itself behind a link (macOS /var → /private/var).
	via := filepath.Join(base, "via")
	if err := os.Symlink(root, via); err != nil {
		t.Fatal(err)
	}
	wv := Workspace{Root: via}
	if abs, err := wv.Resolve("sub"); err != nil || abs != filepath.Join(via, "sub") {
		t.Errorf("through a linked root: %q %v", abs, err)
	}
	if _, err := wv.Resolve("dirlink/secret.txt"); err != ErrOutsideWorkspace {
		t.Errorf("linked root, escape: %v", err)
	}
	read, _ := FindTool(WorkspaceTools(w), "read_file")
	if out, err := read.Run(context.Background(), map[string]any{"path": "dirlink/secret.txt"}); err == nil {
		t.Errorf("read_file through a link read %q", out)
	}
}
