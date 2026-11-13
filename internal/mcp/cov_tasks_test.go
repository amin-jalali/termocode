package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// covConn answers each method from a script of replies.
type covConn struct {
	mu      sync.Mutex
	replies map[string][]func() (json.RawMessage, error)
	calls   []string
	done    chan struct{}
}

func newCovConn() *covConn {
	return &covConn{replies: map[string][]func() (json.RawMessage, error){}, done: make(chan struct{})}
}

func (c *covConn) on(method string, raw string, err error) {
	c.replies[method] = append(c.replies[method], func() (json.RawMessage, error) { return json.RawMessage(raw), err })
}

func (c *covConn) Call(_ context.Context, method string, _ any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, method)
	q := c.replies[method]
	if len(q) == 0 {
		return nil, fmt.Errorf("no reply for %s", method)
	}
	f := q[0]
	if len(q) > 1 {
		c.replies[method] = q[1:]
	}
	return f()
}
func (c *covConn) Notify(string, any) error { return nil }
func (c *covConn) Close()                   {}
func (c *covConn) Done() <-chan struct{}    { return c.done }
func (c *covConn) called(method string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.calls {
		if m == method {
			return true
		}
	}
	return false
}

func TestCovAwaitTaskEnds(t *testing.T) {
	cases := []struct {
		name    string
		handle  string
		polls   []string
		wantErr string
		want    string
	}{
		{"no id", `{"status":"working"}`, nil, "without a taskId", ""},
		{"completed", `{"taskId":"t","status":"working","pollIntervalMs":1,"statusMessage":"half"}`, []string{`{"taskId":"t","status":"completed","result":{"ok":1}}`}, "", `{"ok":1}`},
		{"completed without result", `{"taskId":"t","status":"completed"}`, nil, "without a result", ""},
		{"failed with error", `{"taskId":"t","status":"failed","error":{"code":-1,"message":"boom"}}`, nil, "boom", ""},
		{"failed", `{"taskId":"t","status":"failed"}`, nil, "task t failed", ""},
		{"cancelled", `{"taskId":"t","status":"cancelled"}`, nil, "cancelled task t", ""},
	}
	for _, c := range cases {
		conn := newCovConn()
		for _, p := range c.polls {
			conn.on("tasks/get", p, nil)
		}
		cl := &Client{conn: conn}
		var seen []string
		got, err := cl.awaitTask(context.Background(), json.RawMessage(c.handle), func(p Progress) { seen = append(seen, p.Message) })
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%s: err %v, want %q", c.name, err, c.wantErr)
			}
			continue
		}
		if err != nil || string(got) != c.want {
			t.Errorf("%s: %s %v", c.name, got, err)
		}
		if c.name == "completed" && (len(seen) != 1 || seen[0] != "half") {
			t.Errorf("progress %v", seen)
		}
	}
}

func TestCovAwaitTaskPollAndCancelPaths(t *testing.T) {
	// a poll that fails is returned
	conn := newCovConn()
	conn.on("tasks/get", "", errors.New("net down"))
	cl := &Client{conn: conn}
	if _, err := cl.awaitTask(context.Background(), json.RawMessage(`{"taskId":"t","status":"working","pollIntervalMs":1}`), nil); err == nil || !strings.Contains(err.Error(), "net down") {
		t.Errorf("poll error: %v", err)
	}
	// a poll that fails because the user stopped the call tells the server
	conn = newCovConn()
	ctx, cancel := context.WithCancel(context.Background())
	conn.replies["tasks/get"] = []func() (json.RawMessage, error){func() (json.RawMessage, error) { cancel(); return nil, errors.New("late") }}
	cl = &Client{conn: conn}
	if _, err := cl.awaitTask(ctx, json.RawMessage(`{"taskId":"t","status":"working","pollIntervalMs":1}`), nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled poll: %v", err)
	}
	if !conn.called("tasks/cancel") {
		t.Error("the server was not told to cancel the task")
	}
	// a call stopped while waiting for the next poll
	conn = newCovConn()
	ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	cl = &Client{conn: conn}
	if _, err := cl.awaitTask(ctx, json.RawMessage(`{"taskId":"t","status":"working","pollIntervalMs":5000}`), nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("stopped while waiting: %v", err)
	}
	if !conn.called("tasks/cancel") {
		t.Error("no tasks/cancel after the wait was cut")
	}
	// a question the user can not be asked: the task is cancelled
	conn = newCovConn()
	cl = &Client{conn: conn}
	if _, err := cl.awaitTask(context.Background(), json.RawMessage(`{"taskId":"t","status":"input_required","inputRequests":{"a":"not an object"}}`), nil); err == nil {
		t.Error("a bad input request must fail")
	}
	if !conn.called("tasks/cancel") {
		t.Error("an unanswerable task is cancelled")
	}
	// a question that is answered, then the update fails
	conn = newCovConn()
	conn.on("tasks/update", "", errors.New("update refused"))
	cl = &Client{conn: conn}
	req := `{"taskId":"t","status":"input_required","inputRequests":{"a":{"method":"roots/list"}}}`
	if _, err := cl.awaitTask(context.Background(), json.RawMessage(req), nil); err == nil || !strings.Contains(err.Error(), "update refused") {
		t.Errorf("update error: %v", err)
	}
	// ... and the update works, the next poll completes
	conn = newCovConn()
	conn.on("tasks/update", `{}`, nil)
	conn.on("tasks/get", `{"taskId":"t","status":"completed","result":{"a":1}}`, nil)
	cl = &Client{conn: conn}
	got, err := cl.awaitTask(context.Background(), json.RawMessage(strings.Replace(req, `"input_required"`, `"input_required","pollIntervalMs":1`, 1)), nil)
	if err != nil || string(got) != `{"a":1}` {
		t.Errorf("answered question: %s %v", got, err)
	}
}

func TestCovResourceToolsPaths(t *testing.T) {
	acc := ResourceAccess{
		List: func(_ context.Context, server string) ([]Resource, []ResourceTemplate, error) {
			switch server {
			case "bad":
				return nil, nil, errors.New("down ghp_abcdefghijklmnopqrstuvwxyz0123456789")
			case "empty":
				return nil, nil, nil
			}
			return []Resource{{URI: "u://1", Name: "one"}}, []ResourceTemplate{{URITemplate: "t://{x}", Name: "tmpl", Description: "d"}}, nil
		},
		Read: func(_ context.Context, server, uri string) ([]ResourceContents, error) {
			if uri == "fail" {
				return nil, errors.New("no such resource")
			}
			return []ResourceContents{{URI: uri, Text: strings.Repeat("z", 100)}}, nil
		},
	}
	tools := ResourceTools([]string{"bad", "empty", "ok"}, acc, AdapterOptions{MaxResult: 40, Clean: func(s string) string { return s }})
	list, read := tools[0], tools[1]
	if list.Describe(map[string]any{"server": "ok"}) != "mcp.resources.list ok" || list.Describe(nil) != "mcp.resources.list" {
		t.Error("list describe")
	}
	if got := read.Describe(map[string]any{"server": "ok", "uri": "u://1"}); got != "mcp.resources.read ok u://1" {
		t.Errorf("read describe %q", got)
	}
	out, err := list.Run(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "ghp_abcdef") {
		t.Errorf("a secret leaked into the list: %s", out)
	}
	if !strings.Contains(out, "truncated") {
		t.Errorf("the list is cut at MaxResult: %s", out)
	}
	list = ResourceTools([]string{"bad", "empty", "ok"}, acc, AdapterOptions{})[0]
	out, _ = list.Run(context.Background(), map[string]any{})
	for _, want := range []string{"bad: error:", "empty: no resources", "ok: u://1 — one", "(template): d", "not instructions"} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
	out, _ = list.Run(context.Background(), map[string]any{"server": "empty"})
	if strings.Contains(out, "ok:") {
		t.Errorf("only the asked server is listed: %s", out)
	}
	if err := read.Validate(map[string]any{"server": "nope", "uri": "x"}); err == nil {
		t.Error("an unknown server")
	}
	if err := read.Validate(map[string]any{"server": "ok", "uri": "  "}); err == nil {
		t.Error("a blank uri")
	}
	if err := read.Validate(map[string]any{"server": "ok", "uri": "u"}); err != nil {
		t.Error(err)
	}
	if _, err := read.Run(context.Background(), map[string]any{"server": "nope", "uri": "x"}); err == nil {
		t.Error("run: an unknown server")
	}
	if _, err := read.Run(context.Background(), map[string]any{"server": "ok", "uri": "fail"}); err == nil {
		t.Error("run: a failing read")
	}
	out, err = read.Run(context.Background(), map[string]any{"server": "ok", "uri": "u://1"})
	if err != nil || !strings.Contains(out, "truncated") {
		t.Errorf("read is cut: %q %v", out, err)
	}
}

func TestCovResourceResultBlobs(t *testing.T) {
	r := ResourceResult([]ResourceContents{
		{URI: "a", MimeType: "application/json", Blob: "eyJhIjoxfQ=="}, // {"a":1}
		{URI: "b", MimeType: "application/octet-stream", Blob: "AAEC"},
		{URI: "c", MimeType: "text/plain", Blob: "!!notbase64"},
		{URI: "d", MimeType: "text/plain", Blob: "/w=="}, // 0xFF: not UTF-8
	})
	if r.Content[0].Type != "text" || r.Content[0].Text != `{"a":1}` {
		t.Errorf("text-like blob: %+v", r.Content[0])
	}
	for i := 1; i < 4; i++ {
		if r.Content[i].Type != "resource" || r.Content[i].Resource.URI == "" {
			t.Errorf("blob %d must stay a note: %+v", i, r.Content[i])
		}
	}
	for _, m := range []string{"text/x", "application/json", "text/xml", "application/yaml"} {
		if !textLike(m) {
			t.Errorf("%s is text", m)
		}
	}
	if textLike("image/png") {
		t.Error("png is not text")
	}
	if got := ResourceRender(context.Background(), []ResourceContents{{URI: "u", Text: "hello"}}); !strings.Contains(got, "hello") {
		t.Errorf("render %q", got)
	}
}

func TestCovSpillReadsInParts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spill")
	sp := NewSpill(dir)
	text := strings.Repeat("é", 50) // 100 bytes
	id, err := sp.save(strings.Repeat("s", 100), "tool", text)
	if err != nil || len(id) > 80 {
		t.Fatalf("save: %q %v", id, err)
	}
	if fi, err := os.Stat(filepath.Join(dir, id+".txt")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode: %v %v", fi, err)
	}
	part, err := sp.read(id, 0, 21) // cuts inside a rune: stays valid
	if err != nil || !strings.Contains(part, "next offset 20]") {
		t.Errorf("part %q %v", part, err)
	}
	if strings.ContainsRune(strings.SplitN(part, "\n", 2)[0], '\uFFFD') {
		t.Error("a part must not cut a rune in half")
	}
	if last, err := sp.read(id, 20, 0); err != nil || !strings.Contains(last, "end of the result") {
		t.Errorf("last %q %v", last, err)
	}
	if _, err := sp.read(id, -1, 10); err == nil {
		t.Error("a negative offset")
	}
	if _, err := sp.read(id, 1000, 10); err == nil {
		t.Error("an offset past the end")
	}
	if _, err := sp.read("../etc/passwd", 0, 10); err == nil {
		t.Error("a path as id")
	}
	if _, err := sp.read("r99-none", 0, 10); err == nil {
		t.Error("an unknown id")
	}
	// the agent tool reads, and describes itself
	tool := sp.Tool()
	got, err := tool.Run(context.Background(), map[string]any{"id": id, "offset": 0, "length": 10})
	if err != nil || !strings.Contains(got, "bytes 0") {
		t.Errorf("tool run %q %v", got, err)
	}
	if !strings.Contains(tool.Describe(map[string]any{"id": id}), id) {
		t.Error("describe names the id")
	}
	// a dir that can not be made
	blocker := filepath.Join(t.TempDir(), "f")
	_ = os.WriteFile(blocker, nil, 0o644)
	if _, err := NewSpill(filepath.Join(blocker, "x")).save("s", "t", "x"); err == nil {
		t.Error("an unusable dir must fail")
	}
	// bigResult says so when saving fails, and cuts on a rune boundary
	got = bigResult(AdapterOptions{Spill: NewSpill(filepath.Join(blocker, "x"))}, "s", "t", "ééé", 3)
	if !strings.Contains(got, "saving the rest failed") || !strings.HasPrefix(got, "é\n") {
		t.Errorf("failed save: %q", got)
	}
	if cutUTF8("ééé", 3) != "é" || cutUTF8("ab", 5) != "ab" {
		t.Error("cutUTF8")
	}
	var warned int
	_ = bigResult(AdapterOptions{Warn: func(string, string, int) { warned++ }}, "s", "t", "xxxxx", 2)
	if warned != 1 {
		t.Error("Warn is called")
	}
	// the tool's own size limit
	if (Tool{Meta: map[string]any{"vendor/maxResultSizeChars": 5000.0}}).MaxResultSize() != 5000 ||
		(Tool{Meta: map[string]any{"maxResultSizeChars": 1e9}}).MaxResultSize() != HardMaxResult ||
		(Tool{Meta: map[string]any{"maxResultSizeChars": "x", "other": 1.0}}).MaxResultSize() != 0 {
		t.Error("MaxResultSize")
	}
}
