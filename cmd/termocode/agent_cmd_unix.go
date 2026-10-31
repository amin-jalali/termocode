//go:build unix

package main

import (
	"os"

	"github.com/amin-jalali/termocode/internal/session"
)

// agentSocketOfCwd is the control socket of the current folder's
// workspace ("" when unknown).
func agentSocketOfCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	p, err := session.PathsFor(cwd)
	if err != nil {
		return ""
	}
	return p.AgentSocket
}
