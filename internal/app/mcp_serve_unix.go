//go:build unix

package app

import (
	"net"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/session"
)

// mcpServeSocketPath is the workspace's MCP socket in the private session
// dir (0700): <session dir>/<key>.mcp.sock.
func mcpServeSocketPath(root string) (string, error) {
	p, err := session.PathsFor(root)
	if err != nil {
		return "", err
	}
	return p.MCPSocket, nil
}

// mcpServeListen opens the socket (0600; a stale file is replaced, a live
// one is mcp.ErrSocketBusy). Tests swap it.
var mcpServeListen = func(path string) (net.Listener, error) { return mcp.ListenUnix(path) }

// mcpServePeer lets in only processes of our own uid. Tests swap it.
var mcpServePeer = session.CheckPeer
