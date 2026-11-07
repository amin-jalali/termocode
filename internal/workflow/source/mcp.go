package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Mcp source limits.
const (
	DefaultMCPTimeout = 60 * time.Second
	MaxMCPTimeout     = 10 * time.Minute
	maxToolErrText    = 300
)

// ToolCaller calls one MCP tool (the app passes an adapter over the MCP
// manager).
//
// Contract: the caller looks at the tool's annotations before it runs
// it. A tool that is not marked read-only must NOT be run unless
// WriteAllowed(ctx) is true; the caller then returns (nil, false, nil).
// readOnly reports the annotation. A server that is not connected gives
// an error that wraps ErrNotConnected, so the UI can offer Connect.
type ToolCaller interface {
	CallTool(ctx context.Context, server, tool string, args map[string]any) (result json.RawMessage, readOnly bool, err error)
}

// ToolInfo is one tool a server lists (its tools/list entry): the name
// and the JSON Schema of its arguments.
type ToolInfo struct {
	Name        string
	InputSchema map[string]any
}

// ToolLister is an optional part of a ToolCaller: it lists a server's
// tools, so a message step can find the server's send tool. A server
// that is not connected gives an error that wraps ErrNotConnected.
type ToolLister interface {
	ListTools(ctx context.Context, server string) ([]ToolInfo, error)
}

// ErrNeedsConfirm is returned for a tool that is not read-only when the
// step was not confirmed on a card. The tool did not run.
var ErrNeedsConfirm = errors.New("the tool is not read-only and needs a confirm")

// ErrNotConnected is wrapped by a ToolCaller when the server is not
// connected.
var ErrNotConnected = errors.New("the MCP server is not connected")

// ErrToolFailed is returned when the tool answered with isError.
var ErrToolFailed = errors.New("the tool failed")

type writeAllowedKey struct{}

// WithWriteAllowed marks ctx: the user confirmed running a tool that is
// not read-only.
func WithWriteAllowed(ctx context.Context) context.Context {
	return context.WithValue(ctx, writeAllowedKey{}, true)
}

// WriteAllowed reports whether ctx was marked by WithWriteAllowed.
func WriteAllowed(ctx context.Context) bool {
	v, _ := ctx.Value(writeAllowedKey{}).(bool)
	return v
}

// MCPInput is a `"from": "mcp"` source.
type MCPInput struct {
	Server string
	Tool   string
	// Args are the tool arguments (templates already filled in). No
	// string in them may hold a ${…} reference.
	Args map[string]any
	// Confirmed is set when the user allowed a tool that is not
	// read-only on the card; without it such a tool never runs.
	Confirmed bool
	// Timeout ends the call (0: DefaultMCPTimeout).
	Timeout time.Duration
	Shape
}

var mcpNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

// MCP calls the tool and returns the items of its answer. The answer is
// the tool's structuredContent when it has one, else the JSON in its
// first text content, else the whole result.
func (s *Sources) MCP(ctx context.Context, in MCPInput) (Result, error) {
	if !mcpNameRe.MatchString(in.Server) || !mcpNameRe.MatchString(in.Tool) {
		return Result{}, fmt.Errorf("mcp: server %q / tool %q is not a valid name", in.Server, in.Tool)
	}
	where := "mcp__" + in.Server + "__" + in.Tool
	if s.Tools == nil {
		return Result{}, fmt.Errorf("mcp: %s: %w", where, ErrNotConnected)
	}
	if err := checkArgs(in.Args, "args"); err != nil {
		return Result{}, fmt.Errorf("mcp: %s: %w", where, err)
	}
	timeout := in.Timeout
	if timeout <= 0 {
		timeout = DefaultMCPTimeout
	}
	if timeout > MaxMCPTimeout {
		return Result{}, fmt.Errorf("mcp: timeout %s is over the limit of %s", timeout, MaxMCPTimeout)
	}
	col, err := newCollector(in.Shape)
	if err != nil {
		return Result{}, fmt.Errorf("mcp: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if in.Confirmed {
		ctx = WithWriteAllowed(ctx)
	}
	raw, readOnly, err := s.Tools.CallTool(ctx, in.Server, in.Tool, in.Args)
	if err != nil {
		return Result{}, fmt.Errorf("mcp: %s: %w", where, err)
	}
	if !readOnly && !in.Confirmed {
		// Even if a caller broke the contract and ran it, its answer is
		// not used.
		return Result{}, fmt.Errorf("mcp: %s: %w", where, ErrNeedsConfirm)
	}
	doc, err := unwrapToolResult(raw)
	if err != nil {
		return Result{}, fmt.Errorf("mcp: %s: %w", where, err)
	}
	if err := col.addDoc(doc); err != nil {
		return Result{}, fmt.Errorf("mcp: %s: %w", where, err)
	}
	return col.res, nil
}

// checkArgs refuses ${…} references in argument strings (secrets are
// never sent as tool arguments).
func checkArgs(v any, at string) error {
	switch x := v.(type) {
	case string:
		if strings.Contains(x, "${") {
			return fmt.Errorf("%s holds a ${…} reference; references are not allowed in args", at)
		}
	case map[string]any:
		for k, e := range x {
			if err := checkArgs(e, at+"."+k); err != nil {
				return err
			}
		}
	case []any:
		for i, e := range x {
			if err := checkArgs(e, fmt.Sprintf("%s[%d]", at, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

type toolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

func unwrapToolResult(raw json.RawMessage) ([]byte, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || !json.Valid(raw) {
		return nil, errors.New("the tool answer is not JSON")
	}
	var tr toolResult
	if raw[0] != '{' || json.Unmarshal(raw, &tr) != nil {
		return raw, nil
	}
	if tr.IsError {
		msg := ""
		for _, c := range tr.Content {
			if c.Type == "text" {
				msg = strings.TrimSpace(c.Text)
				break
			}
		}
		if len(msg) > maxToolErrText {
			msg = msg[:maxToolErrText] + "…"
		}
		if msg == "" {
			return nil, ErrToolFailed
		}
		return nil, fmt.Errorf("%w: %s", ErrToolFailed, msg)
	}
	if len(tr.StructuredContent) > 0 && !bytes.Equal(tr.StructuredContent, []byte("null")) {
		return tr.StructuredContent, nil
	}
	for _, c := range tr.Content {
		if c.Type == "text" {
			t := []byte(strings.TrimSpace(c.Text))
			if json.Valid(t) {
				return t, nil
			}
			return nil, errors.New("the tool answer text is not JSON")
		}
	}
	return raw, nil
}
