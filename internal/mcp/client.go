package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amin-jalali/termocode/internal/extproc"
)

// Options are the client's hooks into the app. All are optional and may
// be called from the connection's goroutines.
type Options struct {
	// Roots are the workspace folders (absolute paths), the answer to the
	// server's roots/list (filesystem servers need it).
	Roots func() []string
	// Log gets lifecycle and server lines: stderr, skipped stdout lines,
	// notifications/message. Lines are not redacted here.
	Log func(line string)
	// ToolsChanged is called on notifications/tools/list_changed.
	ToolsChanged func()
	// ResourcesChanged is called on notifications/resources/list_changed
	// (uri "") and notifications/resources/updated (the resource's uri).
	ResourcesChanged func(uri string)
	// Shared is called when the share broker says how many sessions use
	// a shared server, and its pid (share.go).
	Shared func(sessions, pid int)
}

func (o Options) log(format string, args ...any) {
	if o.Log != nil {
		o.Log(fmt.Sprintf(format, args...))
	}
}

// rpcConn is what a Client needs from a transport: the stdio one is an
// extproc.Conn (newline framing), the remote one an httpConn (Streamable
// HTTP, transport_http.go).
type rpcConn interface {
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
	Notify(method string, params any) error
	Close()
	Done() <-chan struct{}
}

// replyFunc answers one server→client request (nil for notifications).
type replyFunc func(result any, err *extproc.Error)

// Client is one MCP connection.
type Client struct {
	conn rpcConn
	opts Options

	mu       sync.Mutex
	version  string
	info     ServerInfo
	caps     ServerCaps
	progress map[string]func() // progress token → push the call's deadline back
	tokens   atomic.Int64
}

// NewClient starts the JSON-RPC connection on r / w (newline framing).
// Call Initialize before anything else.
func NewClient(r io.Reader, w io.Writer, opts Options) *Client {
	c := &Client{opts: opts}
	h := func(conn *extproc.Conn, m extproc.Message) {
		c.handle(m, func(result any, err *extproc.Error) { _ = conn.Reply(m.ID, result, err) })
	}
	c.conn = extproc.NewConnWith(r, w, h, extproc.ConnOptions{
		Framing: extproc.FramingNewline,
		Skipped: func(line string) { opts.log("stdout (not JSON): %s", line) },
		OnCancel: func(conn *extproc.Conn, id json.RawMessage) {
			_ = conn.Notify("notifications/cancelled", map[string]any{"requestId": id, "reason": "cancelled by the user"})
		},
	})
	return c
}

// Version is the negotiated protocol version ("" before Initialize).
func (c *Client) Version() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.version
}

// ServerInfo is the server's name and version from initialize.
func (c *Client) ServerInfo() ServerInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.info
}

// Done is closed when the connection ends.
func (c *Client) Done() <-chan struct{} { return c.conn.Done() }

// Close ends the connection (the transport closes the process).
func (c *Client) Close() { c.conn.Close() }

// call is conn.Call that reports the caller's ctx error (cancel /
// deadline) instead of the generic timeout.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	r, err := c.conn.Call(ctx, method, params)
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if errors.Is(err, extproc.ErrClosed) {
		return nil, errors.New("the MCP server closed the connection")
	}
	return r, err
}

// Initialize runs the handshake: initialize (we offer ProtocolVersion and
// accept any of SupportedVersions), then notifications/initialized.
func (c *Client) Initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{"roots": map[string]any{"listChanged": false}},
		"clientInfo":      map[string]any{"name": "termocode", "version": "1"},
	}
	raw, err := c.call(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	var res initializeResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("initialize: bad answer: %w", err)
	}
	if !supportedVersion(res.ProtocolVersion) {
		return fmt.Errorf("the server speaks MCP %q; termocode supports %s", res.ProtocolVersion, strings.Join(SupportedVersions, ", "))
	}
	c.mu.Lock()
	c.version, c.info, c.caps = res.ProtocolVersion, res.ServerInfo, parseCaps(res.Capabilities)
	c.mu.Unlock()
	// HTTP sends the negotiated version on every later request.
	if pv, ok := c.conn.(interface{ setProtocolVersion(string) }); ok {
		pv.setProtocolVersion(res.ProtocolVersion)
	}
	return c.conn.Notify("notifications/initialized", nil)
}

// maxToolPages guards against a server whose cursor never ends.
const maxToolPages = 50

// ListTools returns every tool, following nextCursor.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var all []Tool
	cursor := ""
	for page := 0; page < maxToolPages; page++ {
		var params any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}
		raw, err := c.call(ctx, "tools/list", params)
		if err != nil {
			return nil, fmt.Errorf("tools/list: %w", err)
		}
		var res listToolsResult
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, fmt.Errorf("tools/list: bad answer: %w", err)
		}
		all = append(all, res.Tools...)
		if res.NextCursor == "" {
			return all, nil
		}
		cursor = res.NextCursor
	}
	return all, nil
}

// CallTool runs one tool. A JSON-RPC error is an error; a tool failure
// (isError) is a result whose Text starts with "error: ".
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (CallResult, error) {
	return c.callTool(ctx, name, args, nil)
}

// ErrCallTimeout: a tools/call got no answer and no progress within its
// timeout.
var ErrCallTimeout = errors.New("no answer in time")

// CallToolTimeout is CallTool with a deadline that each progress
// notification of the call pushes back (long Playwright runs report
// progress). It returns ErrCallTimeout when the server stays silent for
// timeout; timeout <= 0 means no deadline.
func (c *Client) CallToolTimeout(ctx context.Context, name string, args map[string]any, timeout time.Duration) (CallResult, error) {
	if timeout <= 0 {
		return c.CallTool(ctx, name, args)
	}
	token := fmt.Sprintf("termocode-%d", c.tokens.Add(1))
	cctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	timer := time.AfterFunc(timeout, func() { cancel(ErrCallTimeout) })
	defer timer.Stop()
	c.mu.Lock()
	if c.progress == nil {
		c.progress = map[string]func(){}
	}
	c.progress[token] = func() { timer.Reset(timeout) }
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.progress, token)
		c.mu.Unlock()
	}()
	res, err := c.callTool(cctx, name, args, map[string]any{"progressToken": token})
	if err != nil && ctx.Err() == nil && errors.Is(context.Cause(cctx), ErrCallTimeout) {
		return res, ErrCallTimeout
	}
	return res, err
}

func (c *Client) callTool(ctx context.Context, name string, args map[string]any, meta map[string]any) (CallResult, error) {
	if args == nil {
		args = map[string]any{}
	}
	params := map[string]any{"name": name, "arguments": args}
	if meta != nil {
		params["_meta"] = meta
	}
	raw, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return CallResult{}, err
	}
	var res CallResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return CallResult{}, fmt.Errorf("tools/call: bad answer: %w", err)
	}
	return res, nil
}

// handle answers the server's requests and notifications. It runs on the
// reader goroutine, so it never blocks.
func (c *Client) handle(m extproc.Message, reply replyFunc) {
	switch m.Method {
	case "ping":
		reply(map[string]any{}, nil)
	case "roots/list":
		roots := []map[string]any{}
		if c.opts.Roots != nil {
			for _, r := range c.opts.Roots() {
				u := url.URL{Scheme: "file", Path: filepath.ToSlash(r)}
				roots = append(roots, map[string]any{"uri": u.String(), "name": filepath.Base(r)})
			}
		}
		reply(map[string]any{"roots": roots}, nil)
	case "notifications/tools/list_changed":
		c.opts.log("tools changed")
		if c.opts.ToolsChanged != nil {
			c.opts.ToolsChanged()
		}
	case "notifications/resources/list_changed", "notifications/resources/updated":
		var p struct {
			URI string `json:"uri"`
		}
		_ = json.Unmarshal(m.Params, &p)
		c.opts.log("resources changed %s", p.URI)
		if c.opts.ResourcesChanged != nil {
			c.opts.ResourcesChanged(p.URI)
		}
	case "notifications/progress":
		var p struct {
			Token json.RawMessage `json:"progressToken"`
		}
		_ = json.Unmarshal(m.Params, &p)
		var tok string
		if json.Unmarshal(p.Token, &tok) != nil {
			tok = string(p.Token)
		}
		c.mu.Lock()
		f := c.progress[tok]
		c.mu.Unlock()
		if f != nil {
			f()
		}
	case shareNotify:
		var p struct {
			Sessions int `json:"sessions"`
			Pid      int `json:"pid"`
		}
		if json.Unmarshal(m.Params, &p) == nil && c.opts.Shared != nil {
			c.opts.Shared(p.Sessions, p.Pid)
		}
	case "notifications/message":
		var p struct {
			Level string          `json:"level"`
			Data  json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(m.Params, &p)
		var s string
		if json.Unmarshal(p.Data, &s) != nil {
			s = string(p.Data)
		}
		c.opts.log("%s: %s", p.Level, s)
	default:
		// sampling/createMessage and elicitation/create are refused on
		// purpose: a server must never drive our model or the user.
		if m.IsRequest() {
			reply(nil, &extproc.Error{Code: extproc.CodeMethodNotFound, Message: "termocode does not support " + m.Method})
		}
	}
}
