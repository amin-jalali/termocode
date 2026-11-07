package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// fakeAgent is the agent end of the pipe: it answers the client's
// requests with a script and can call the client back.
type fakeAgent struct {
	t   *testing.T
	in  *bufio.Reader
	out io.Writer
	mu  sync.Mutex
	got []string // methods the client sent
	id  int
}

func newPair(t *testing.T, h Handler) (*Client, *fakeAgent) {
	t.Helper()
	cr, aw := io.Pipe() // agent writes, client reads
	ar, cw := io.Pipe() // client writes, agent reads
	c := NewClient(cr, cw, h)
	t.Cleanup(func() { c.Close(); aw.Close(); ar.Close() })
	return c, &fakeAgent{t: t, in: bufio.NewReader(ar), out: aw}
}

func (a *fakeAgent) read() map[string]any {
	a.t.Helper()
	line, err := a.in.ReadBytes('\n')
	if err != nil {
		a.t.Fatalf("agent read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		a.t.Fatalf("agent got bad json %q: %v", line, err)
	}
	if meth, ok := m["method"].(string); ok {
		a.mu.Lock()
		a.got = append(a.got, meth)
		a.mu.Unlock()
	}
	return m
}

func (a *fakeAgent) send(v any) {
	b, _ := json.Marshal(v)
	if _, err := a.out.Write(append(b, '\n')); err != nil {
		a.t.Errorf("agent write: %v", err)
	}
}

func (a *fakeAgent) reply(req map[string]any, result any) {
	a.send(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": result})
}

func (a *fakeAgent) update(u map[string]any) {
	a.send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "s1", "update": u}})
}

// call sends a request to the client and returns its response.
func (a *fakeAgent) call(method string, params any) map[string]any {
	a.id++
	a.send(map[string]any{"jsonrpc": "2.0", "id": a.id, "method": method, "params": params})
	return a.read()
}

type recHandler struct {
	mu      sync.Mutex
	asked   []PermissionRequest
	choose  string
	writes  []WriteRequest
	allowWr bool
	root    string
}

func (h *recHandler) RequestPermission(_ context.Context, r PermissionRequest) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.asked = append(h.asked, r)
	return h.choose, nil
}

func (h *recHandler) WriteTextFile(_ context.Context, w WriteRequest) error {
	h.mu.Lock()
	h.writes = append(h.writes, w)
	ok := h.allowWr
	h.mu.Unlock()
	if !ok {
		return ErrDenied
	}
	return WriteInside(h.root, w.Path, w.Content)
}

func (h *recHandler) ReadTextFile(_ context.Context, r ReadRequest) (string, error) {
	return ReadInside(h.root, r.Path, r.Line, r.Limit)
}

// Spec (CMP-83): initialize names protocol 1 and the client's file
// abilities; session/new sends the workspace as cwd; a prompt streams
// message chunks, tool calls and the plan, and ends with the stop reason.
func TestClientHandshakeAndPrompt(t *testing.T) {
	root := t.TempDir()
	c, a := newPair(t, &recHandler{root: root})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		req := a.read()
		p := req["params"].(map[string]any)
		if p["protocolVersion"].(float64) != 1 {
			t.Errorf("protocolVersion %v", p["protocolVersion"])
		}
		fs := p["clientCapabilities"].(map[string]any)["fs"].(map[string]any)
		if fs["readTextFile"] != true || fs["writeTextFile"] != true {
			t.Errorf("fs caps %v", fs)
		}
		a.reply(req, map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{"loadSession": false}})
		req = a.read()
		if cwd := req["params"].(map[string]any)["cwd"]; cwd != root {
			t.Errorf("cwd %v", cwd)
		}
		a.reply(req, map[string]any{"sessionId": "s1"})
		req = a.read()
		blocks := req["params"].(map[string]any)["prompt"].([]any)
		if blocks[0].(map[string]any)["text"] != "fix the bug" {
			t.Errorf("prompt %v", blocks)
		}
		a.update(map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]any{"type": "text", "text": "thinking"}})
		a.update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "Looking"}})
		a.update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1", "title": "Read main.go", "kind": "read", "status": "pending"})
		a.update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "completed"})
		a.update(map[string]any{"sessionUpdate": "plan", "entries": []any{map[string]any{"content": "step one", "priority": "high", "status": "in_progress"}}})
		a.update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": " done."}})
		a.reply(req, map[string]any{"stopReason": "end_turn"})
	}()

	if err := c.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	sid, err := c.NewSession(ctx, root)
	if err != nil || sid != "s1" {
		t.Fatalf("session %q %v", sid, err)
	}
	var ups []Update
	stop, err := c.Prompt(ctx, sid, "fix the bug", func(u Update) { ups = append(ups, u) })
	if err != nil || stop != "end_turn" {
		t.Fatalf("stop %q err %v", stop, err)
	}
	var text strings.Builder
	kinds := []string{}
	for _, u := range ups {
		kinds = append(kinds, u.Kind)
		if u.Kind == UpdateMessage {
			text.WriteString(u.Text)
		}
	}
	if text.String() != "Looking done." {
		t.Errorf("text %q", text.String())
	}
	want := "agent_thought_chunk agent_message_chunk tool_call tool_call_update plan agent_message_chunk"
	if got := strings.Join(kinds, " "); got != want {
		t.Errorf("kinds\n got %s\nwant %s", got, want)
	}
	if ups[2].Tool.ID != "t1" || ups[2].Tool.Title != "Read main.go" || ups[3].Tool.Status != "completed" {
		t.Errorf("tool updates %+v %+v", ups[2].Tool, ups[3].Tool)
	}
	if len(ups[4].Plan) != 1 || ups[4].Plan[0].Content != "step one" {
		t.Errorf("plan %+v", ups[4].Plan)
	}
}

// Spec (CMP-83): the agent's permission request goes to the handler with
// its tool call and options; the chosen option goes back as "selected",
// an empty choice as "cancelled".
func TestClientPermission(t *testing.T) {
	h := &recHandler{choose: "allow"}
	_, a := newPair(t, h)
	params := map[string]any{"sessionId": "s1",
		"toolCall": map[string]any{"toolCallId": "t2", "title": "Run go test", "kind": "execute"},
		"options": []any{
			map[string]any{"optionId": "reject", "name": "Reject", "kind": "reject_once"},
			map[string]any{"optionId": "allow", "name": "Allow", "kind": "allow_once"},
		}}
	resp := a.call("session/request_permission", params)
	out := resp["result"].(map[string]any)["outcome"].(map[string]any)
	if out["outcome"] != "selected" || out["optionId"] != "allow" {
		t.Fatalf("outcome %v", out)
	}
	h.mu.Lock()
	if len(h.asked) != 1 || h.asked[0].Tool.Title != "Run go test" || len(h.asked[0].Options) != 2 || h.asked[0].Options[0].Kind != "reject_once" {
		t.Errorf("asked %+v", h.asked)
	}
	h.choose = ""
	h.mu.Unlock()
	resp = a.call("session/request_permission", params)
	if out := resp["result"].(map[string]any)["outcome"].(map[string]any); out["outcome"] != "cancelled" {
		t.Fatalf("empty choice: %v", out)
	}
}

// Spec (CMP-83): the agent reads and writes files only inside the
// workspace; a write the handler denies is an error and changes nothing.
func TestClientFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &recHandler{root: root}
	_, a := newPair(t, h)
	resp := a.call("fs/read_text_file", map[string]any{"sessionId": "s1", "path": filepath.Join(root, "a.txt"), "line": 2, "limit": 1})
	if got := resp["result"].(map[string]any)["content"]; got != "two\n" {
		t.Errorf("read %q", got)
	}
	resp = a.call("fs/read_text_file", map[string]any{"sessionId": "s1", "path": "/etc/passwd"})
	if resp["error"] == nil {
		t.Error("read outside the workspace")
	}
	resp = a.call("fs/write_text_file", map[string]any{"sessionId": "s1", "path": filepath.Join(root, "b.txt"), "content": "new\n"})
	if resp["error"] == nil {
		t.Error("a denied write did not fail")
	}
	if _, err := os.Stat(filepath.Join(root, "b.txt")); err == nil {
		t.Error("a denied write wrote")
	}
	h.mu.Lock()
	h.allowWr = true
	h.mu.Unlock()
	resp = a.call("fs/write_text_file", map[string]any{"sessionId": "s1", "path": filepath.Join(root, "b.txt"), "content": "new\n"})
	if resp["error"] != nil {
		t.Fatalf("write: %v", resp["error"])
	}
	if b, _ := os.ReadFile(filepath.Join(root, "b.txt")); string(b) != "new\n" {
		t.Errorf("wrote %q", b)
	}
	resp = a.call("fs/write_text_file", map[string]any{"sessionId": "s1", "path": filepath.Join(root, "..", "evil.txt"), "content": "x"})
	if resp["error"] == nil {
		t.Error("write outside the workspace")
	}
	resp = a.call("terminal/create", map[string]any{"sessionId": "s1", "command": "rm"})
	if e, _ := resp["error"].(map[string]any); e == nil || e["code"].(float64) != -32601 {
		t.Errorf("unknown method: %v", resp)
	}
}

func TestInsideRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"rel.txt", filepath.Join(root, "..", "x"), "/tmp/../etc/passwd"} {
		if _, err := Inside(root, p); err == nil {
			t.Errorf("%q accepted", p)
		}
	}
	if err := os.Symlink("/etc", filepath.Join(root, "link")); err == nil {
		if _, err := Inside(root, filepath.Join(root, "link", "passwd")); err == nil {
			t.Error("a symlink out of the workspace was accepted")
		}
	}
	if got, err := Inside(root, filepath.Join(root, "sub", "new.go")); err != nil || !strings.HasSuffix(got, filepath.Join("sub", "new.go")) {
		t.Errorf("new file in a new folder: %q %v", got, err)
	}
}

// Spec (CMP-83): the write card shows a diff of the change.
func TestDiffPreview(t *testing.T) {
	got := DiffPreview("a\nb\nc\n", "a\nB\nc\nd\n", 50)
	for _, want := range []string{"-b", "+B", "+d", " a"} {
		if !strings.Contains(got, want) {
			t.Errorf("preview lacks %q:\n%s", want, got)
		}
	}
	long := DiffPreview("", strings.Repeat("x\n", 100), 10)
	if !strings.Contains(long, "more lines") || strings.Count(long, "\n") > 12 {
		t.Errorf("long preview not cut:\n%s", long)
	}
}

// Spec (CMP-83): an ACP agent is an ai.Provider: the newest user message
// is the prompt, message chunks are deltas, tool calls show as short
// lines, and the turn ends with one Done chunk.
func TestProviderStreams(t *testing.T) {
	c, a := newPair(t, &recHandler{})
	p := &Provider{Client: c, Session: "s1", Title: "Helper"}
	if p.Name() != "Helper" || !p.Configured() || !p.SupportsTools() { // native: its text is never parsed as termocode tool calls
		t.Fatalf("provider %s %v %v", p.Name(), p.Configured(), p.SupportsTools())
	}
	go func() {
		req := a.read()
		blocks := req["params"].(map[string]any)["prompt"].([]any)
		if blocks[0].(map[string]any)["text"] != "second" {
			t.Errorf("prompt %v", blocks)
		}
		a.update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "Hi"}})
		a.update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1", "title": "Edit x.go", "kind": "edit", "status": "in_progress"})
		a.reply(req, map[string]any{"stopReason": "end_turn"})
	}()
	ch := p.Stream(context.Background(), ai.Request{Messages: []ai.Message{
		{Role: "system", Content: "sys"}, {Role: "user", Content: "first"}, {Role: "assistant", Content: "ok"}, {Role: "user", Content: "second"}}})
	text, last := ai.Collect(ch)
	if last.Err != nil {
		t.Fatal(last.Err)
	}
	if !strings.HasPrefix(text, "Hi") || !strings.Contains(text, "Edit x.go") {
		t.Errorf("text %q", text)
	}
}

func TestLoadAgents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "acp_agents.json")
	if as, err := LoadAgents(path); err != nil || len(as) != 0 {
		t.Fatalf("missing file: %v %v", as, err)
	}
	os.WriteFile(path, []byte(`{"agents":[{"name":"Helper","command":"helper-agent","args":["--acp"]},{"name":"","command":"x"},{"name":"Bad","command":""}]}`), 0o600)
	as, err := LoadAgents(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(as) != 1 || as[0].Name != "Helper" || as[0].Args[0] != "--acp" {
		t.Fatalf("agents %+v", as)
	}
}

// A dangling link inside the workspace must not let a write create its
// target outside.
func TestInsideRejectsDanglingLink(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(t.TempDir(), "made.txt")
	if err := os.Symlink(out, filepath.Join(root, "dangle")); err != nil {
		t.Skip("no symlinks")
	}
	if err := WriteInside(root, filepath.Join(root, "dangle"), "x"); err == nil {
		t.Error("wrote through a dangling link")
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("the link target was created")
	}
}
