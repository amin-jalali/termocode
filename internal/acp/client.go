package acp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"
)

// cancelGrace is how long a cancelled prompt waits for the agent's
// answer before it gives up (a var for tests).
var cancelGrace = 10 * time.Second

// ProtocolVersion is the protocol version termocode speaks.
const ProtocolVersion = 1

// ErrDenied is a Handler's answer when the user said no.
var ErrDenied = errors.New("the user denied it")

// Update kinds (the sessionUpdate field).
const (
	UpdateMessage    = "agent_message_chunk"
	UpdateThought    = "agent_thought_chunk"
	UpdateUser       = "user_message_chunk"
	UpdateToolCall   = "tool_call"
	UpdateToolUpdate = "tool_call_update"
	UpdatePlan       = "plan"
)

// ToolCall is the agent's tool call, as it reports it.
type ToolCall struct {
	ID       string        `json:"toolCallId"`
	Title    string        `json:"title,omitempty"`
	Kind     string        `json:"kind,omitempty"`   // read, edit, delete, move, search, execute, think, fetch, other
	Status   string        `json:"status,omitempty"` // pending, in_progress, completed, failed
	Content  []ToolContent `json:"content,omitempty"`
	Location []struct {
		Path string `json:"path"`
		Line int    `json:"line,omitempty"`
	} `json:"locations,omitempty"`
}

// ToolContent is one piece of a tool call's output: text, or a diff.
type ToolContent struct {
	Type    string `json:"type"` // content, diff, terminal
	Path    string `json:"path,omitempty"`
	OldText string `json:"oldText,omitempty"`
	NewText string `json:"newText,omitempty"`
	Content *struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"content,omitempty"`
}

// PlanEntry is one line of the agent's plan.
type PlanEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority,omitempty"`
	Status   string `json:"status,omitempty"` // pending, in_progress, completed
}

// Update is one session/update, flattened.
type Update struct {
	Kind string
	Text string   // message, thought and user chunks
	Tool ToolCall // tool_call, tool_call_update
	Plan []PlanEntry
}

// PermissionOption is one choice the agent offers.
type PermissionOption struct {
	ID   string `json:"optionId"`
	Name string `json:"name"`
	Kind string `json:"kind"` // allow_once, allow_always, reject_once, reject_always
}

// PermissionRequest is the agent asking before a tool call.
type PermissionRequest struct {
	Session string             `json:"sessionId"`
	Tool    ToolCall           `json:"toolCall"`
	Options []PermissionOption `json:"options"`
}

// ReadRequest is fs/read_text_file (Line is 1-based; 0: from the start;
// Limit 0: to the end).
type ReadRequest struct {
	Session string `json:"sessionId"`
	Path    string `json:"path"`
	Line    int    `json:"line,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// WriteRequest is fs/write_text_file.
type WriteRequest struct {
	Session string `json:"sessionId"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Handler serves the agent's calls into the client. Each may block
// until the user answers; ctx ends when the connection closes.
type Handler interface {
	// RequestPermission returns the chosen option id; "" is cancelled.
	RequestPermission(ctx context.Context, r PermissionRequest) (string, error)
	ReadTextFile(ctx context.Context, r ReadRequest) (string, error)
	// WriteTextFile writes after the user's yes (ErrDenied: no).
	WriteTextFile(ctx context.Context, w WriteRequest) error
}

// Client is one connection to an ACP agent.
type Client struct {
	c      *conn
	h      Handler
	closer []io.Closer
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	sinks    map[string]func(Update) // session → the running prompt's sink
	initDone bool
	Agent    AgentInfo
}

// AgentInfo is what initialize told about the agent.
type AgentInfo struct {
	ProtocolVersion int `json:"protocolVersion"`
	Capabilities    struct {
		LoadSession bool `json:"loadSession"`
	} `json:"agentCapabilities"`
	AuthMethods []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"authMethods,omitempty"`
}

// NewClient talks to an agent that reads w and writes r. Close closes
// both when they are io.Closers.
func NewClient(r io.Reader, w io.Writer, h Handler) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	cl := &Client{c: newConn(w), h: h, ctx: ctx, cancel: cancel, sinks: map[string]func(Update){}}
	for _, x := range []any{r, w} {
		if c, ok := x.(io.Closer); ok {
			cl.closer = append(cl.closer, c)
		}
	}
	cl.c.onRequest = cl.serve
	cl.c.onNotify = cl.notified
	cl.c.start(r)
	return cl
}

// Close ends the connection; a running Prompt returns ErrClosed.
func (cl *Client) Close() error {
	cl.cancel()
	cl.c.shutdown()
	var first error
	for _, c := range cl.closer {
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Done is closed when the connection ends.
func (cl *Client) Done() <-chan struct{} { return cl.c.done }

// Initialize agrees on the protocol and tells the agent the client can
// read and write text files (no terminals).
func (cl *Client) Initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": ProtocolVersion,
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": true, "writeTextFile": true},
			"terminal": false,
		},
	}
	var info AgentInfo
	if err := cl.c.call(ctx, "initialize", params, &info); err != nil {
		return err
	}
	if info.ProtocolVersion != 0 && info.ProtocolVersion != ProtocolVersion {
		return errors.New("the agent speaks protocol version " + itoa(info.ProtocolVersion) + "; termocode speaks 1")
	}
	cl.mu.Lock()
	cl.Agent, cl.initDone = info, true
	cl.mu.Unlock()
	return nil
}

// NewSession starts a session in folder cwd (absolute) with no MCP
// servers; it returns the session id.
func (cl *Client) NewSession(ctx context.Context, cwd string) (string, error) {
	var res struct {
		SessionID string `json:"sessionId"`
	}
	if err := cl.c.call(ctx, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}}, &res); err != nil {
		return "", err
	}
	if res.SessionID == "" {
		return "", errors.New("the agent gave no session id")
	}
	return res.SessionID, nil
}

// Prompt sends one user message and streams the session's updates to
// sink until the agent answers; it returns the stop reason (end_turn,
// max_tokens, refusal, cancelled, …). Cancelling ctx sends session/cancel
// and waits for the agent's answer.
func (cl *Client) Prompt(ctx context.Context, session, text string, sink func(Update)) (string, error) {
	cl.mu.Lock()
	cl.sinks[session] = sink
	cl.mu.Unlock()
	defer func() {
		cl.mu.Lock()
		delete(cl.sinks, session)
		cl.mu.Unlock()
	}()
	params := map[string]any{"sessionId": session, "prompt": []any{map[string]any{"type": "text", "text": text}}}
	var res struct {
		StopReason string `json:"stopReason"`
	}
	callCtx, stop := context.WithCancel(cl.ctx)
	defer stop()
	go func() {
		select {
		case <-ctx.Done():
			_ = cl.Cancel(session)
			select { // the agent answers "cancelled"; do not wait forever
			case <-time.After(cancelGrace):
				stop()
			case <-callCtx.Done():
			}
		case <-callCtx.Done():
		}
	}()
	if err := cl.c.call(callCtx, "session/prompt", params, &res); err != nil {
		if ctx.Err() != nil {
			return "cancelled", ctx.Err()
		}
		return "", err
	}
	return res.StopReason, nil
}

// Cancel asks the agent to stop the session's running prompt.
func (cl *Client) Cancel(session string) error {
	return cl.c.notify("session/cancel", map[string]any{"sessionId": session})
}

func (cl *Client) notified(method string, params json.RawMessage) {
	if method != "session/update" {
		return
	}
	var n struct {
		Session string          `json:"sessionId"`
		Update  json.RawMessage `json:"update"`
	}
	if json.Unmarshal(params, &n) != nil {
		return
	}
	u, ok := parseUpdate(n.Update)
	if !ok {
		return
	}
	cl.mu.Lock()
	sink := cl.sinks[n.Session]
	cl.mu.Unlock()
	if sink != nil {
		sink(u)
	}
}

func parseUpdate(raw json.RawMessage) (Update, bool) {
	var head struct {
		Kind    string `json:"sessionUpdate"`
		Content *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Entries []PlanEntry `json:"entries"`
	}
	if json.Unmarshal(raw, &head) != nil || head.Kind == "" {
		return Update{}, false
	}
	u := Update{Kind: head.Kind}
	switch head.Kind {
	case UpdateMessage, UpdateThought, UpdateUser:
		if head.Content != nil && head.Content.Type == "text" {
			u.Text = head.Content.Text
		}
	case UpdateToolCall, UpdateToolUpdate:
		if json.Unmarshal(raw, &u.Tool) != nil {
			return Update{}, false
		}
	case UpdatePlan:
		u.Plan = head.Entries
	}
	return u, true
}

// serve answers the agent's requests through the Handler.
func (cl *Client) serve(method string, params json.RawMessage) (any, error) {
	switch method {
	case "session/request_permission":
		var r PermissionRequest
		if err := json.Unmarshal(params, &r); err != nil {
			return nil, paramsError{err}
		}
		id, err := cl.h.RequestPermission(cl.ctx, r)
		if err != nil || id == "" || !hasOption(r.Options, id) {
			return map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}, nil
		}
		return map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": id}}, nil
	case "fs/read_text_file":
		var r ReadRequest
		if err := json.Unmarshal(params, &r); err != nil {
			return nil, paramsError{err}
		}
		text, err := cl.h.ReadTextFile(cl.ctx, r)
		if err != nil {
			return nil, err
		}
		return map[string]any{"content": text}, nil
	case "fs/write_text_file":
		var w WriteRequest
		if err := json.Unmarshal(params, &w); err != nil {
			return nil, paramsError{err}
		}
		if err := cl.h.WriteTextFile(cl.ctx, w); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return nil, methodError{method}
}

func hasOption(os []PermissionOption, id string) bool {
	for _, o := range os {
		if o.ID == id {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
