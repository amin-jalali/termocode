package main

import (
	"context"
	"fmt"
	"io"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/extproc"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// staticMCPTools is the tool list answered when no UI runs: the same
// allow-list the UI serves, so the agent CLI sees a healthy server.
func staticMCPTools() []mcp.Tool { return app.MCPServeToolSpecs() }

// staticSource lists the tools and refuses every call with a clear error.
type staticSource struct {
	tools []mcp.Tool
	msg   string
}

func (s staticSource) List() []mcp.Tool { return s.tools }

func (s staticSource) Call(context.Context, string, map[string]any) (mcp.CallResult, error) {
	return mcp.CallResult{}, &mcp.CallError{Code: -32001, Message: s.msg}
}

// staticMCPMessage is the error every tools/call gets in static mode.
func staticMCPMessage(workspace, reason string) string {
	if reason != "" {
		return fmt.Sprintf("termocode cannot be reached in %s (%s); open termocode in that folder, then restart this agent", workspace, reason)
	}
	return fmt.Sprintf("termocode is not open in %s; open termocode in that folder, then restart this agent", workspace)
}

// serveStaticMCP is the stdio MCP server used when no UI serves the
// workspace: initialize, ping and tools/list work, tools/call fails with
// -32001. It runs until stdin ends.
func serveStaticMCP(in io.Reader, out io.Writer, errw io.Writer, workspace, reason string, tools []mcp.Tool) int {
	msg := staticMCPMessage(workspace, reason)
	fmt.Fprintf(errw, "termocode mcp: %s\n", msg)
	srv := mcp.NewServer(staticSource{tools: tools, msg: msg})
	conn := extproc.NewConnWith(in, out, srv.Handle, extproc.ConnOptions{Framing: extproc.FramingNewline})
	<-conn.Done()
	conn.Close()
	return mcpExitOK
}
