//go:build unix

package app

import (
	"errors"
	"net"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/session"
)

// errWebHubBusy: another termocode already serves the web hub here.
var errWebHubBusy = errors.New("another termocode already serves the web for this folder")

// webHubSocketPath is the workspace's web hub socket in the private
// session dir (0700): <session dir>/<key>.web.sock.
func webHubSocketPath(root string) (string, error) {
	p, err := session.PathsFor(root)
	if err != nil {
		return "", err
	}
	return p.WebSocket, nil
}

// webHubListen opens the hub socket with mode 0600. A socket file nobody
// answers on (a crashed run) is removed first; one that answers is
// errWebHubBusy; any other kind of file is left alone and is an error.
// Tests swap it.
var webHubListen = func(path string) (net.Listener, error) {
	ln, err := mcp.ListenUnix(path) // the same rules as the MCP socket
	if errors.Is(err, mcp.ErrSocketBusy) {
		return nil, errWebHubBusy
	}
	return ln, err
}

// webHubPeer lets in only processes of our own uid. Tests swap it.
var webHubPeer = session.CheckPeer
