package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/session"
)

// A CLI step's own MCP server. A Codex or Claude Code step gets the
// step's full termocode tool set (the same list a model step gets, cut
// by its Tools setting) through a private socket the CLI reaches with
// `termocode mcp --socket <path>`. Every call goes through an ai.Agent's
// gate (aiAgent.CallTool): a read runs at once, a change asks on the
// chat's card, a hook or approval rule decides as in a model step, the
// result is redacted, and the call is a line on the step card and in
// the audit. This is not the outside agents' server (mcp_serve*.go):
// that one keeps its narrow allow-list.

// aiStepMCPName is the server's name for the CLI (tools mcp__termocode__*).
const aiStepMCPName = "termocode"

// aiStepMCPExe is the termocode program the CLI starts (tests swap it).
var aiStepMCPExe = os.Executable

// aiStepMCP is one running step server.
type aiStepMCP struct {
	srv  *mcp.SocketServer
	path string
	dir  string // a folder made for the socket ("": the socket sits in the session folder)
	spec *clistep.MCPServer
}

// Close stops the server, ends its connections and removes the socket.
func (s *aiStepMCP) Close() {
	_ = s.srv.Close()
	_ = os.Remove(s.path)
	if s.dir != "" {
		_ = os.RemoveAll(s.dir)
	}
}

// aiStepMCPSource serves the step's tools through agent's gate.
type aiStepMCPSource struct {
	agent *ai.Agent
	specs []mcp.Tool
	emit  func(ai.Event)
	n     atomic.Int64
}

func (s *aiStepMCPSource) List() []mcp.Tool { return s.specs }

func (s *aiStepMCPSource) Call(ctx context.Context, name string, args map[string]any) (mcp.CallResult, error) {
	if _, ok := ai.FindTool(s.agent.Tools, name); !ok {
		return mcp.CallResult{}, &mcp.CallError{Code: mcp.CodeInvalidParams, Message: fmt.Sprintf("unknown tool %q", name)}
	}
	id := "mcp-" + strconv.FormatInt(s.n.Add(1), 10)
	out := ai.RedactSecrets(s.agent.CallTool(ctx, ai.ToolCall{ID: id, Name: name, Args: args}, s.emit))
	return mcp.CallResult{Content: []mcp.Content{{Type: "text", Text: out}}, IsError: strings.HasPrefix(out, "error:")}, nil
}

// aiStepMCPSpecs are the MCP specs of tools (never a user's own MCP
// server's tool: those are not served again, MC-11).
func aiStepMCPSpecs(tools []ai.Tool) []mcp.Tool {
	out := make([]mcp.Tool, 0, len(tools))
	no := false
	for _, t := range tools {
		if strings.HasPrefix(t.Name, "mcp__") {
			continue
		}
		spec := t.Spec()
		readOnly := !t.Mutating && t.Gate == nil && t.Class != ai.ClassReview
		out = append(out, mcp.Tool{Name: spec.Name, Description: spec.Description, InputSchema: spec.Schema,
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: &readOnly, DestructiveHint: &no}})
	}
	return out
}

// aiStepMCPPath is a new socket path for root's step server: in the
// private session folder, else in a new private temp folder (dir).
func aiStepMCPPath(root string) (path, dir string, err error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", err
	}
	name := "step-" + hex.EncodeToString(b[:]) + ".sock"
	if p, err := session.PathsFor(root); err == nil {
		d := filepath.Dir(p.MCPSocket)
		if fi, err := os.Stat(d); err == nil && fi.IsDir() && fi.Mode().Perm() == 0o700 {
			return filepath.Join(d, name), "", nil
		}
	}
	d, err := os.MkdirTemp("", "tc-step-")
	if err != nil {
		return "", "", err
	}
	return filepath.Join(d, name), d, nil
}

// aiStartStepMCP serves the tools of agent for one CLI step. emit gets
// each call's events (the step card, the audit).
func aiStartStepMCP(root string, agent *ai.Agent, emit func(ai.Event)) (*aiStepMCP, error) {
	specs := aiStepMCPSpecs(agent.Tools)
	if len(specs) == 0 {
		return nil, nil
	}
	exe, err := aiStepMCPExe()
	if err != nil {
		return nil, err
	}
	path, dir, err := aiStepMCPPath(root)
	if err != nil {
		return nil, err
	}
	ln, err := mcpServeListen(path)
	if err != nil {
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
		return nil, err
	}
	src := &aiStepMCPSource{agent: agent, specs: specs, emit: emit}
	srv := mcp.Serve(ln, src, mcpServePeer, func(err error) { recordError("[ai] step tools: " + err.Error()) })
	return &aiStepMCP{srv: srv, path: path, dir: dir,
		spec: &clistep.MCPServer{Name: aiStepMCPName, Command: exe, Args: []string{"mcp", "--socket", path}}}, nil
}
