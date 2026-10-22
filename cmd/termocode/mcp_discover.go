//go:build unix

package main

import (
	"os"
	"path/filepath"

	"github.com/amin-jalali/termocode/internal/session"
)

// mcpWalkLevels is how many folders `termocode mcp` looks at going up
// from the current folder (the start folder counts as the first).
const mcpWalkLevels = 8

// mcpTarget is the workspace `termocode mcp` talks to.
type mcpTarget struct {
	Workspace string // absolute folder (for messages)
	Socket    string // MCP socket path; "" when no UI serves this folder
}

// mcpPathsFor is the MCP socket path of a workspace folder.
func mcpPathsFor(workspace string) (string, error) {
	p, err := session.PathsFor(workspace)
	return p.MCPSocket, err
}

// isSocket reports whether path is a unix socket file.
func isSocket(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode()&os.ModeSocket != 0
}

// findMCPSocket finds the socket of the UI to relay to. With an explicit
// --dir (explicit true) exactly that folder is looked at. Otherwise it
// walks up from cwd (at most mcpWalkLevels folders) to the nearest one
// whose MCP socket exists. No socket is not an error: Socket is "" and
// Workspace names the folder to tell the user about.
func findMCPSocket(dir string, explicit bool, cwd string) (mcpTarget, error) {
	if explicit {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return mcpTarget{}, err
		}
		sock, err := mcpPathsFor(abs)
		if err != nil {
			return mcpTarget{}, err
		}
		t := mcpTarget{Workspace: abs}
		if isSocket(sock) {
			t.Socket = sock
		}
		return t, nil
	}
	start, err := filepath.Abs(cwd)
	if err != nil {
		return mcpTarget{}, err
	}
	d := start
	for i := 0; i < mcpWalkLevels; i++ {
		sock, err := mcpPathsFor(d)
		if err != nil {
			return mcpTarget{}, err
		}
		if isSocket(sock) {
			return mcpTarget{Workspace: d, Socket: sock}, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return mcpTarget{Workspace: start}, nil
}
