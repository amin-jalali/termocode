package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	// StartTimeout caps the start of the server (0: initTimeout).
	StartTimeout time.Duration
}

// startTimeout is how long a start may take.
func (o Options) startTimeout() time.Duration {
	if o.StartTimeout > 0 {
		return o.StartTimeout
	}
	return initTimeout
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
	progress map[string]func(Progress) // progress token → push the call's deadline back, tell the ctx
	tokens   atomic.Int64

	// elicit asks the user for the running tool call (one call at a
	// time per server); elicitCtx is that call's ctx (elicit.go).
	elicit    Elicitor
	elicitCtx context.Context
	hold      func(waiting bool) // stops / restarts the call's timeout
	toolsTTL  atomic.Int64       // ttlMs of the last tools/list (spec2026_ttl.go)
	stateless bool               // the 2026-07-28 revision (spec2026_client.go)
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
	if c.Stateless() {
		return c.roundTrips(ctx, method, params) // _meta and MRTR (spec2026_mrtr.go)
	}
	return c.send(ctx, method, params)
}

// send is one request on the connection.
func (c *Client) send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	r, err := c.conn.Call(ctx, method, params)
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if errors.Is(err, extproc.ErrClosed) {
		return nil, errors.New("the MCP server closed the connection")
	}
	return r, err
}

// Initialize starts the connection: the server/discover probe picks the
// stateless revision when the server speaks it (spec2026_client.go);
// otherwise the handshake runs: initialize (we offer ProtocolVersion and
// accept any of SupportedVersions), then notifications/initialized.
func (c *Client) Initialize(ctx context.Context) error {
	if modern, err := c.discover(ctx); modern || err != nil {
		if modern {
			c.startListen() // change notifications (spec2026_listen.go)
		}
		return err
	}
	params := map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    clientCapabilities(),
		"clientInfo":      clientInfo(),
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

// Ping checks that the server answers (a health check). A server that
// does not know ping still answers (an error reply), so that counts too.
func (c *Client) Ping(ctx context.Context) error {
	method := "ping"
	if c.Stateless() {
		method = "server/discover" // the stateless revision has no ping
	}
	_, err := c.call(ctx, method, nil)
	var re *extproc.Error
	if errors.As(err, &re) {
		return nil
	}
	return err
}

// maxToolPages guards against a server whose cursor never ends.
const maxToolPages = 50

// ListTools returns every tool, following nextCursor.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var all []Tool
	cursor := ""
	var ttl int64 // the shortest ttlMs of the pages (0: none)
	for page := 0; page < maxToolPages; page++ {
		var params any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}
		raw, err := c.call(ctx, "tools/list", params)
		if err != nil {
			return nil, fmt.Errorf("tools/list: %w", err)
		}
		if err := checkResultType(raw); err != nil {
			return nil, fmt.Errorf("tools/list: %w", err)
		}
		var res listToolsResult
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, fmt.Errorf("tools/list: bad answer: %w", err)
		}
		tools, dropped := parseTools(res.Tools)
		for _, d := range dropped {
			c.opts.log("tool dropped: %s", d)
		}
		all = append(all, tools...)
		if res.TTLMs > 0 && (ttl == 0 || res.TTLMs < ttl) {
			ttl = res.TTLMs
		}
		if res.NextCursor == "" {
			c.toolsTTL.Store(ttl)
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
	report := progressFrom(ctx) // MCPX-19: progress text for the chat
	if timeout <= 0 && report == nil {
		return c.CallTool(ctx, name, args)
	}
	token := fmt.Sprintf("termocode-%d", c.tokens.Add(1))
	cctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	reset := func() {}
	hold := func(bool) {}
	if timeout > 0 {
		timer := time.AfterFunc(timeout, func() { cancel(ErrCallTimeout) })
		defer timer.Stop()
		reset = func() { timer.Reset(timeout) }
		hold = func(waiting bool) { // the user answers a question (elicit.go)
			if waiting {
				timer.Stop()
			} else {
				timer.Reset(timeout)
			}
		}
	}
	c.mu.Lock()
	if c.progress == nil {
		c.progress = map[string]func(Progress){}
	}
	c.progress[token] = func(p Progress) {
		reset()
		if report != nil {
			report(p)
		}
	}
	c.hold = hold
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.progress, token)
		c.hold = nil
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
	if e := elicitorFrom(ctx); e != nil {
		c.mu.Lock()
		c.elicit, c.elicitCtx = e, ctx
		c.mu.Unlock()
		defer func() {
			c.mu.Lock()
			c.elicit, c.elicitCtx = nil, nil
			c.mu.Unlock()
		}()
	}
	if meta != nil {
		params["_meta"] = meta
	}
	raw, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return CallResult{}, err
	}
	return decodeCallResult(raw)
}

// handle answers the server's requests and notifications. It runs on the
// reader goroutine, so it never blocks.
func (c *Client) handle(m extproc.Message, reply replyFunc) {
	switch m.Method {
	case "ping":
		reply(map[string]any{}, nil)
	case "roots/list":
		reply(c.rootsResult(), nil)
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
	case "notifications/subscriptions/acknowledged":
		c.opts.log("listening for changes")
	case "notifications/progress":
		var p struct {
			Token    json.RawMessage `json:"progressToken"`
			Progress float64         `json:"progress"`
			Total    float64         `json:"total"`
			Message  string          `json:"message"`
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
			f(Progress{Progress: p.Progress, Total: p.Total, Message: p.Message})
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
	case "elicitation/create":
		c.handleElicit(m, reply)
	default:
		// sampling/createMessage is refused on purpose: a server must
		// never drive our model.
		if m.IsRequest() {
			reply(nil, &extproc.Error{Code: extproc.CodeMethodNotFound, Message: "termocode does not support " + m.Method})
		}
	}
}
