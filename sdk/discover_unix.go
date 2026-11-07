//go:build unix

package sdk

import "github.com/amin-jalali/termocode/internal/session"

// workspaceSocket is the control socket of the termocode that has dir
// open as its workspace.
func workspaceSocket(dir string) (string, error) {
	p, err := session.PathsFor(dir)
	if err != nil {
		return "", err
	}
	return p.AgentSocket, nil
}
