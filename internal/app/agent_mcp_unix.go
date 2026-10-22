//go:build unix

package app

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/session"
)

// agentMCPFilePathOS is <session dir>/<key>.mcp-<name>: the dir is the
// private one (0700) of the sockets.
func agentMCPFilePathOS(root, name string) (string, error) {
	p, err := session.PathsFor(root)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(p.MCPSocket, ".mcp.sock") + ".mcp-" + name, nil
}
