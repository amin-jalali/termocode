//go:build unix

package main

import (
	"time"

	"github.com/amin-jalali/termocode/internal/agentctl"
	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/traybar"
)

// barAsk bounds the wait for one session's control socket, so a busy UI
// cannot hang the menu bar.
const barAsk = 2 * time.Second

// barSessions lists the running sessions and asks each for its tabs.
func barSessions() ([]traybar.Session, error) {
	list, err := session.List()
	if err != nil {
		return nil, err
	}
	out := make([]traybar.Session, 0, len(list))
	for _, in := range list {
		s := traybar.Session{Workspace: in.Workspace, Attached: in.Attached}
		p, err := session.PathsFor(in.Workspace)
		if err != nil {
			s.Unreachable = true
		} else if rep, err := agentCall(p.AgentSocket, agentctl.Request{Op: agentctl.OpList}, barAsk); err != nil {
			s.Unreachable = true
		} else {
			s.Tabs = rep.Tabs
		}
		out = append(out, s)
	}
	return out, nil
}
