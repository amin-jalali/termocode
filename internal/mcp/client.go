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
}

func (o Options) log(format string, args ...any) {
	if o.Log != nil {
		o.Log(fmt.Sprintf(format, args...))
	}
}

// Client is one MCP connection (any transport that gives a byte stream).
type Client struct {
	conn *extproc.Conn
	opts Options

	mu      sync.Mutex
	version string
	info    ServerInfo
}

// NewClient starts the JSON-RPC connection on r / w (newline framing).
// Call Initialize before anything else.
func NewClient(r io.Reader, w io.Writer, opts Options) *Client {
	c := &Client{opts: opts}
	c.conn = extproc.NewConnWith(r, w, c.handle, extproc.ConnOptions{
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
	c.version, c.info = res.ProtocolVersion, res.ServerInfo
	c.mu.Unlock()
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
	if args == nil {
		args = map[string]any{}
	}
	raw, err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
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
func (c *Client) handle(conn *extproc.Conn, m extproc.Message) {
	switch m.Method {
	case "ping":
		_ = conn.Reply(m.ID, map[string]any{}, nil)
	case "roots/list":
		roots := []map[string]any{}
		if c.opts.Roots != nil {
			for _, r := range c.opts.Roots() {
				u := url.URL{Scheme: "file", Path: filepath.ToSlash(r)}
				roots = append(roots, map[string]any{"uri": u.String(), "name": filepath.Base(r)})
			}
		}
		_ = conn.Reply(m.ID, map[string]any{"roots": roots}, nil)
	case "notifications/tools/list_changed":
		c.opts.log("tools changed")
		if c.opts.ToolsChanged != nil {
			c.opts.ToolsChanged()
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
			_ = conn.Reply(m.ID, nil, &extproc.Error{Code: extproc.CodeMethodNotFound, Message: "termocode does not support " + m.Method})
		}
	}
}
