//go:build unix

package app

import (
	"net"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// mcpServeSocketPath is the workspace's MCP socket in the private session
// dir (0700): <session dir>/<key>.mcp.sock. A container workspace's key
// is its workspace ID (RC-5; `termocode mcp --workspace <id>`), as its
// root is a path inside the container, not a local folder.
func mcpServeSocketPath(root string) (string, error) {
	if ws := procWorkspace(); ws.Kind == workspace.KindContainer && ws.Root == root {
		p, err := session.PathsForID(ws.ID)
		if err != nil {
			return "", err
		}
		return p.MCPSocket, nil
	}
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
