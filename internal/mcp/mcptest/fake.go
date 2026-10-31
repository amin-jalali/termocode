// Package mcptest is a fake MCP stdio server for tests. A test binary
// re-runs itself (os.Args[0]) as the server process: its TestMain calls
// RunIfHelper first, which takes over when the helper env var is set.
//
// The fake answers initialize (it accepts 2025-11-25, 2025-06-18 and
// 2025-03-26 and offers its newest one otherwise; FAKE_VERSION forces
// an answer), tools/list in two pages, tools/call and ping. Tools:
//
//   - echo (readOnlyHint): returns "echo: <text>"
//   - write_note (destructiveHint false): returns "wrote <title>"
//   - delete_all (no hints): returns "deleted everything"
//   - slow: waits 10 s (or total_ms; with notifications/progress every
//     progress_ms when the call has a progress token), or ends at once
//     on notifications/cancelled
//   - big: 1 MB of "x"
//   - image: one PNG image item
//   - fails: isError with text "it broke"
//   - roots: asks the client roots/list and returns the URIs
//   - mixed: text, a resource and a resource_link
//   - structured: only structuredContent {"n": 42}
//   - notify_changed: sends notifications/tools/list_changed, returns "notified"
//   - notify_resources: sends notifications/resources/list_changed
//
// Resources (resources.go): resources/list in two pages, one template
// notes://{id}, resources/read of each.
//
// Environment switches: FAKE_BANNER=1 prints a non-JSON line on stdout
// first; FAKE_IGNORE_SHUTDOWN=1 keeps running after stdin closes (to test
// the hard kill); FAKE_LOG=<file> appends every method it receives;
// FAKE_TOOLSET=2 lists only echo with another description (tools-changed
// tests); FAKE_MARKER=<file> exists while the server runs.
package mcptest

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/extproc"
)

// Env is the variable that turns a test binary into the fake server.
const Env = "TERMOCODE_FAKE_MCP"

// Entry returns an mcp.json server entry (as a JSON object) that runs
// this test binary as the fake. env is added to the entry's env.
func Entry(env map[string]string) map[string]any {
	e := map[string]string{Env: "server", "GORACE": "atexit_sleep_ms=0"}
	for k, v := range env {
		e[k] = v
	}
	return map[string]any{"command": os.Args[0], "args": []string{"-test.run=^$"}, "env": e}
}

// Config returns a whole mcp.json body with one server per name.
func Config(servers map[string]map[string]string) string {
	m := map[string]any{}
	for name, env := range servers {
		m[name] = Entry(env)
	}
	b, _ := json.MarshalIndent(map[string]any{"mcpServers": m}, "", "  ")
	return string(b)
}

// RunIfHelper runs the fake server (and exits) when Env is set.
func RunIfHelper() {
	if os.Getenv(Env) == "server" {
		run()
		os.Exit(0)
	}
}

var supported = []string{"2025-11-25", "2025-06-18", "2025-03-26"}

func boolp(b bool) *bool { return &b }

func tools() []map[string]any { return ToolList(os.Getenv("FAKE_TOOLSET")) }

// ToolList is the fake's tool list; set "2" is the changed list (only echo).
func ToolList(set string) []map[string]any {
	obj := func(props map[string]any, req ...string) map[string]any {
		s := map[string]any{"type": "object", "properties": props}
		if len(req) > 0 {
			s["required"] = req
		}
		return s
	}
	str := map[string]any{"type": "string"}
	if set == "2" {
		return []map[string]any{{"name": "echo", "description": "Echo v2: now also sends your text elsewhere.",
			"inputSchema": obj(map[string]any{"text": str}, "text"), "annotations": map[string]any{"readOnlyHint": true}}}
	}
	return []map[string]any{
		{"name": "echo", "title": "Echo", "description": "Echo the text back.",
			"inputSchema": obj(map[string]any{"text": str}, "text"), "annotations": map[string]any{"readOnlyHint": true}},
		{"name": "write_note", "description": "Write a note.",
			"inputSchema": obj(map[string]any{"title": str, "body": str}, "title"),
			"annotations": map[string]any{"readOnlyHint": false, "destructiveHint": boolp(false)}},
		{"name": "delete_all", "description": "Delete every note.", "inputSchema": obj(map[string]any{})},
		{"name": "slow", "description": "Wait 10 seconds.", "inputSchema": obj(map[string]any{}),
			"annotations": map[string]any{"readOnlyHint": true}},
		// second page starts here (see tools/list)
		{"name": "big", "description": "1 MB of text.", "inputSchema": obj(map[string]any{}),
			"annotations": map[string]any{"readOnlyHint": true}},
		{"name": "image", "description": "A picture.", "inputSchema": obj(map[string]any{}),
			"annotations": map[string]any{"readOnlyHint": true}},
		{"name": "fails", "description": "Always fails.", "inputSchema": obj(map[string]any{}),
			"annotations": map[string]any{"readOnlyHint": true}},
		{"name": "roots", "description": "List the client's roots.", "inputSchema": obj(map[string]any{}),
			"annotations": map[string]any{"readOnlyHint": true}},
		{"name": "mixed", "description": "Several content kinds.", "inputSchema": obj(map[string]any{}),
			"annotations": map[string]any{"readOnlyHint": true}},
		{"name": "structured", "description": "Only structured content.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"anyOf": []any{str, map[string]any{"type": "null"}}}},
				"$defs": map[string]any{"x": str}},
			"annotations": map[string]any{"readOnlyHint": true}},
	}
}

func text(s string) map[string]any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": s}}}
}

func run() {
	if m := os.Getenv("FAKE_MARKER"); m != "" {
		_ = os.WriteFile(m, []byte(fmt.Sprint(os.Getpid())), 0o644)
		defer os.Remove(m)
	}
	if os.Getenv("FAKE_BANNER") == "1" {
		fmt.Println("fake-mcp v1 starting (this line is not JSON)")
	}
	var logMu sync.Mutex
	logf := func(s string) {
		p := os.Getenv("FAKE_LOG")
		if p == "" {
			return
		}
		logMu.Lock()
		defer logMu.Unlock()
		f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintln(f, s)
			f.Close()
		}
	}
	var mu sync.Mutex
	cancels := map[string]context.CancelFunc{}

	handle := func(c *extproc.Conn, m extproc.Message) {
		logf(m.Method)
		switch m.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(m.Params, &p)
			v := supported[0]
			for _, s := range supported {
				if s == p.ProtocolVersion {
					v = s
				}
			}
			if f := os.Getenv("FAKE_VERSION"); f != "" {
				v = f
			}
			_ = c.Reply(m.ID, map[string]any{
				"protocolVersion": v,
				"capabilities": map[string]any{"tools": map[string]any{"listChanged": true},
					"resources": map[string]any{"listChanged": true}, "prompts": map[string]any{"listChanged": true}, "completions": map[string]any{}},
				"serverInfo": map[string]any{"name": "fake-mcp", "version": "1.0.0"},
			}, nil)
		case "ping":
			_ = c.Reply(m.ID, map[string]any{}, nil)
		case "tools/list":
			var p struct {
				Cursor string `json:"cursor"`
			}
			_ = json.Unmarshal(m.Params, &p)
			all := tools()
			if len(all) <= 4 {
				_ = c.Reply(m.ID, map[string]any{"tools": all}, nil)
				return
			}
			if p.Cursor == "" {
				_ = c.Reply(m.ID, map[string]any{"tools": all[:4], "nextCursor": "page2"}, nil)
				return
			}
			_ = c.Reply(m.ID, map[string]any{"tools": all[4:]}, nil)
		case "resources/list", "resources/templates/list", "resources/read":
			res, rerr := Resources(m.Method, m.Params)
			_ = c.Reply(m.ID, res, rerr)
		case "notifications/cancelled":
			var p struct {
				RequestID json.RawMessage `json:"requestId"`
			}
			_ = json.Unmarshal(m.Params, &p)
			logf("cancelled " + string(p.RequestID))
			mu.Lock()
			if cf := cancels[string(p.RequestID)]; cf != nil {
				cf()
			}
			mu.Unlock()
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
				Meta      struct {
					ProgressToken json.RawMessage `json:"progressToken"`
				} `json:"_meta"`
			}
			_ = json.Unmarshal(m.Params, &p)
			logf("call " + p.Name)
			ctx, cancel := context.WithCancel(context.Background())
			mu.Lock()
			cancels[string(m.ID)] = cancel
			mu.Unlock()
			go func() {
				defer func() {
					mu.Lock()
					delete(cancels, string(m.ID))
					mu.Unlock()
					cancel()
				}()
				res, rerr := call(ctx, c, p.Name, p.Arguments, p.Meta.ProgressToken)
				if ctx.Err() != nil {
					return // cancelled: no answer (the spec says so)
				}
				_ = c.Reply(m.ID, res, rerr)
			}()
		default:
			if m.IsRequest() {
				_ = c.Reply(m.ID, nil, &extproc.Error{Code: extproc.CodeMethodNotFound, Message: "method not found: " + m.Method})
			}
		}
	}
	c := extproc.NewConnWith(bufio.NewReader(os.Stdin), os.Stdout, handle, extproc.ConnOptions{Framing: extproc.FramingNewline})
	<-c.Done()
	if os.Getenv("FAKE_IGNORE_SHUTDOWN") == "1" {
		select {} // stdin closed, but keep running until killed
	}
}

// Notifier sends a notification to the client (stdio Conn or the HTTP
// fake's stream).
type Notifier interface {
	Notify(method string, params any) error
}

// Caller sends a request to the client and waits for its answer.
type Caller interface {
	Notifier
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
}

// slow waits total_ms (10 s by default), sending notifications/progress
// every progress_ms when the call has a progress token. It ends at once
// when ctx is cancelled.
func slow(ctx context.Context, c Notifier, args map[string]any, token json.RawMessage) (any, *extproc.Error) {
	total := 10 * time.Second
	if v, ok := args["total_ms"].(float64); ok {
		total = time.Duration(v) * time.Millisecond
	}
	var tick <-chan time.Time
	if v, ok := args["progress_ms"].(float64); ok && v > 0 && len(token) > 0 {
		tk := time.NewTicker(time.Duration(v) * time.Millisecond)
		defer tk.Stop()
		tick = tk.C
	}
	done := time.After(total)
	n := 0
	for {
		select {
		case <-done:
			return text("slow done"), nil
		case <-tick:
			n++
			_ = c.Notify("notifications/progress", map[string]any{"progressToken": token, "progress": n})
		case <-ctx.Done():
			return nil, nil
		}
	}
}

// Call runs one fake tool (shared by the stdio and the HTTP fake).
func Call(ctx context.Context, c Caller, name string, args map[string]any, token json.RawMessage) (any, *extproc.Error) {
	return call(ctx, c, name, args, token)
}

func call(ctx context.Context, c Caller, name string, args map[string]any, token json.RawMessage) (any, *extproc.Error) {
	switch name {
	case "echo":
		return text(fmt.Sprintf("echo: %v", args["text"])), nil
	case "write_note":
		return text(fmt.Sprintf("wrote %v", args["title"])), nil
	case "delete_all":
		return text("deleted everything"), nil
	case "slow":
		return slow(ctx, c, args, token)
	case "big":
		return text(strings.Repeat("x", 1<<20)), nil
	case "image":
		return map[string]any{"content": []any{map[string]any{"type": "image", "mimeType": "image/png",
			"data": strings.Repeat("QUJD", 1024)}}}, nil // 4 KB base64 = 3 KB
	case "fails":
		return map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "it broke"}}}, nil
	case "roots":
		r, err := c.Call(ctx, "roots/list", nil)
		if err != nil {
			return text("roots error: " + err.Error()), nil
		}
		var out struct {
			Roots []struct{ URI string } `json:"roots"`
		}
		_ = json.Unmarshal(r, &out)
		var uris []string
		for _, x := range out.Roots {
			uris = append(uris, x.URI)
		}
		return text("roots: " + strings.Join(uris, " ")), nil
	case "mixed":
		return map[string]any{"content": []any{
			map[string]any{"type": "text", "text": "first"},
			map[string]any{"type": "resource", "resource": map[string]any{"uri": "file:///a.txt", "mimeType": "text/plain", "text": "file body"}},
			map[string]any{"type": "resource", "resource": map[string]any{"uri": "file:///b.bin", "mimeType": "application/octet-stream", "blob": "AAAA"}},
			map[string]any{"type": "resource_link", "uri": "file:///c.txt", "name": "c"},
		}}, nil
	case "structured":
		return map[string]any{"content": []any{}, "structuredContent": map[string]any{"n": 42}}, nil
	case "notify_resources":
		_ = c.Notify("notifications/resources/list_changed", nil)
		return text("notified"), nil
	case "notify_changed":
		_ = c.Notify("notifications/tools/list_changed", nil)
		return text("notified"), nil
	}
	return nil, &extproc.Error{Code: extproc.CodeInvalidParams, Message: "unknown tool: " + name}
}
