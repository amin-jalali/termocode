//go:build unix

package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
)

// runMCP is `termocode mcp [--dir <workspace>]`: a stdio MCP server for
// an agent CLI, relayed to the termocode UI of the workspace.
func runMCP(args []string) int {
	cwd, _ := os.Getwd()
	return runMCPIn(args, os.Stdin, os.Stdout, os.Stderr, cwd)
}

// runMCPIn is runMCP with its streams and current folder passed in.
func runMCPIn(args []string, in io.Reader, out, errw io.Writer, cwd string) int {
	dir, explicit := "", false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help" || a == "help":
			fmt.Fprint(out, mcpUsage)
			return mcpExitOK
		case a == "--dir":
			if i+1 >= len(args) {
				fmt.Fprintf(errw, "termocode mcp: --dir needs a folder\n\n%s", mcpUsage)
				return mcpExitUsage
			}
			i++
			dir, explicit = args[i], true
		case strings.HasPrefix(a, "--dir="):
			dir, explicit = strings.TrimPrefix(a, "--dir="), true
		default:
			fmt.Fprintf(errw, "termocode mcp: unknown argument %q\n\n%s", a, mcpUsage)
			return mcpExitUsage
		}
	}
	if explicit {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			fmt.Fprintf(errw, "termocode mcp: %s is not a folder\n", dir)
			return mcpExitUsage
		}
	}
	target, err := findMCPSocket(dir, explicit, cwd)
	if err != nil {
		fmt.Fprintf(errw, "termocode mcp: %v\n", err)
		return mcpExitLost
	}
	reason := ""
	if target.Socket != "" {
		conn, err := net.DialTimeout("unix", target.Socket, 2*time.Second)
		if err == nil {
			return relayMCP(in, out, conn, errw, target.Workspace)
		}
		reason = "the socket does not answer"
		if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
			reason = "permission denied: socket owned by another user"
		}
	}
	return serveStaticMCP(in, out, errw, target.Workspace, reason, staticMCPTools())
}
