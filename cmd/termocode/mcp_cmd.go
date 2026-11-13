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

	"github.com/amin-jalali/termocode/internal/session"
)

// runMCP is `termocode mcp [--dir <workspace> | --workspace <id>]`: a stdio MCP server for
// an agent CLI, relayed to the termocode UI of the workspace.
func runMCP(args []string) int {
	cwd, _ := os.Getwd()
	return runMCPIn(args, os.Stdin, os.Stdout, os.Stderr, cwd)
}

// runMCPIn is runMCP with its streams and current folder passed in.
func runMCPIn(args []string, in io.Reader, out, errw io.Writer, cwd string) int {
	dir, explicit, sock, wsID := "", false, "", ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help" || a == "help":
			fmt.Fprint(out, mcpUsage)
			return mcpExitOK
		case a == "--socket":
			if i+1 >= len(args) || args[i+1] == "" {
				fmt.Fprintf(errw, "termocode mcp: --socket needs a path\n\n%s", mcpUsage)
				return mcpExitUsage
			}
			i++
			sock = args[i]
		case a == "--dir":
			if i+1 >= len(args) {
				fmt.Fprintf(errw, "termocode mcp: --dir needs a folder\n\n%s", mcpUsage)
				return mcpExitUsage
			}
			i++
			dir, explicit = args[i], true
		case strings.HasPrefix(a, "--dir="):
			dir, explicit = strings.TrimPrefix(a, "--dir="), true
		case a == "--workspace" || strings.HasPrefix(a, "--workspace="):
			v, found := strings.CutPrefix(a, "--workspace=")
			if !found {
				if i+1 >= len(args) {
					fmt.Fprintf(errw, "termocode mcp: --workspace needs a workspace ID\n\n%s", mcpUsage)
					return mcpExitUsage
				}
				i++
				v = args[i]
			}
			if _, err := session.PathsForID(v); err != nil {
				fmt.Fprintf(errw, "termocode mcp: %v\n\n%s", err, mcpUsage)
				return mcpExitUsage
			}
			wsID = v
		default:
			fmt.Fprintf(errw, "termocode mcp: unknown argument %q\n\n%s", a, mcpUsage)
			return mcpExitUsage
		}
	}
	if sock != "" { // a CLI step's own server: that socket or nothing
		conn, err := net.DialTimeout("unix", sock, 2*time.Second)
		if err != nil {
			fmt.Fprintf(errw, "termocode mcp: the AI step's tools are not open (%v); the step may have ended\n", err)
			return mcpExitLost
		}
		return relayMCP(in, out, conn, errw, "this AI step")
	}
	if wsID != "" && explicit {
		fmt.Fprintf(errw, "termocode mcp: give --workspace or --dir, not both\n\n%s", mcpUsage)
		return mcpExitUsage
	}
	if wsID != "" {
		p, _ := session.PathsForID(wsID) // checked above
		if isSocket(p.MCPSocket) {
			if conn, err := net.DialTimeout("unix", p.MCPSocket, 2*time.Second); err == nil {
				return relayMCP(in, out, conn, errw, wsID)
			}
			return serveStaticMCP(in, out, errw, wsID, "the socket does not answer", staticMCPTools())
		}
		return serveStaticMCP(in, out, errw, wsID, "", staticMCPTools())
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
