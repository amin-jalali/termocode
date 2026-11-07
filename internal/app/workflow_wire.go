package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/workflow/source"
)

// ── Workflows: the app's adapters (WFX wiring) ───────────────────────────
//
// internal/workflow/wire fits the engine together; these are the parts
// only the app has: the MCP manager and the database connections.

// wfMCP calls MCP tools for workflow sources and writes. It is both
// source.ToolCaller and run.ToolCheck: the tool's annotation (through
// mcp.Gate, the same rule as the chat) is checked BEFORE every call.
type wfMCP struct {
	known func(name string) (mcp.Server, []mcp.Tool, bool) // Manager.KnownTools
	call  mcp.CallFunc                                     // Manager.Call without trust prompts
	trust bool                                             // mcp_trust_annotations
}

func newWFMCP(mgr *mcp.Manager, trustAnnotations bool) *wfMCP {
	if mgr == nil {
		return nil
	}
	return &wfMCP{known: mgr.KnownTools, call: mgr.Caller(nil), trust: trustAnnotations}
}

// tool finds a tool and says whether it is offered and read-only.
func (c *wfMCP) tool(server, tool string) (exposed, readOnly bool, err error) {
	if c == nil || c.known == nil {
		return false, false, source.ErrNotConnected
	}
	srv, tools, ok := c.known(server)
	if !ok {
		return false, false, fmt.Errorf("%s: %w (connect it in Itermoc → Services)", server, source.ErrNotConnected)
	}
	for _, t := range tools {
		if t.Name == tool {
			exposed, mutating, _ := mcp.Gate(srv, t, c.trust)
			return exposed, exposed && !mutating, nil
		}
	}
	return false, false, fmt.Errorf("%s has no tool %q", server, tool)
}

// ListTools is source.ToolLister: the tools the server listed (its
// tools/list, as the MCP manager keeps it), without the ones turned off
// for termocode.
func (c *wfMCP) ListTools(_ context.Context, server string) ([]source.ToolInfo, error) {
	if c == nil || c.known == nil {
		return nil, source.ErrNotConnected
	}
	srv, tools, ok := c.known(server)
	if !ok {
		return nil, fmt.Errorf("%s: %w (connect it in Itermoc → Services)", server, source.ErrNotConnected)
	}
	out := make([]source.ToolInfo, 0, len(tools))
	for _, t := range tools {
		if exposed, _, _ := mcp.Gate(srv, t, c.trust); exposed {
			out = append(out, source.ToolInfo{Name: t.Name, InputSchema: t.InputSchema})
		}
	}
	return out, nil
}

// ReadOnly is run.ToolCheck.
func (c *wfMCP) ReadOnly(_ context.Context, server, tool string) (bool, error) {
	_, ro, err := c.tool(server, tool)
	return ro, err
}

// CallTool is source.ToolCaller: a tool that is not read-only runs only
// when ctx carries the user's yes.
func (c *wfMCP) CallTool(ctx context.Context, server, tool string, args map[string]any) (json.RawMessage, bool, error) {
	exposed, ro, err := c.tool(server, tool)
	if err != nil {
		return nil, false, err
	}
	if !exposed {
		return nil, false, fmt.Errorf("%s/%s is turned off for termocode", server, tool)
	}
	if !ro && !source.WriteAllowed(ctx) {
		return nil, false, nil
	}
	if c.call == nil {
		return nil, ro, source.ErrNotConnected
	}
	res, err := c.call(ctx, server, tool, args)
	if err != nil {
		return nil, ro, err
	}
	b, err := json.Marshal(res)
	return b, ro, err
}

// wfDB opens the same read-only sessions the AI's database tools use.
type wfDB struct{ root string }

func (d wfDB) OpenReadOnly(ctx context.Context, conn string) (dbclient.Session, string, error) {
	if d.root == "" {
		return nil, "", errors.New("db: no workspace")
	}
	c, err := dbAIConn(d.root, conn)
	if err != nil {
		return nil, "", err
	}
	var snap dbAISnapshot
	s, err := dbAIOpen(ctx, d.root, c, &snap, true)
	return s, c.Driver, err
}
