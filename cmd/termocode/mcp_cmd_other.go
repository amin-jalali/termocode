//go:build !unix

package main

import (
	"fmt"
	"os"
)

// The MCP relay needs unix sockets.
func runMCP([]string) int {
	fmt.Fprintln(os.Stderr, "termocode mcp: not supported on this system")
	return mcpExitLost
}
