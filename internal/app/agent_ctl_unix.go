//go:build unix

package app

import "github.com/amin-jalali/termocode/internal/session"

// agentCtlSocketPath is where the control socket of root is ("" when
// unknown): <session dir>/<key>.agent.sock.
func agentCtlSocketPath(root string) string {
	p, err := session.PathsFor(root)
	if err != nil {
		return ""
	}
	return p.AgentSocket
}

// agentCtlPeer lets in only processes of our own uid.
var agentCtlPeer = session.CheckPeer
