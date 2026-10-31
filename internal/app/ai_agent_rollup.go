package app

import (
	"os"
	"strings"

	"github.com/amin-jalali/termocode/internal/agentroll"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
)

// ── Workspace roll-up of agent states (OMX-35) ────────────────────────
//
// This window writes what its agents do (AI runs, coding CLIs in
// terminal tabs) to a small shared file when that changes; the Runs view
// lists the other windows' workspaces whose agents work or wait for you.
// Nothing ticks: the file is written only when a state changes, and read
// when the Runs view opens.

// agentRollupItems are this window's agents now.
func (m *Model) agentRollupItems() []agentroll.Item {
	var out []agentroll.Item
	if s := m.ai; s != nil {
		if rm := s.aiRunMessage(); rm != nil && s.run != nil {
			state := "working"
			if rm.Run.PlanWait || rm.Run.State == ai.RunStatePaused || (m.confirmOpen && m.confirmKind == confirmKindAITool) {
				state = "waiting"
			}
			out = append(out, agentroll.Item{Name: "run " + rm.Run.ID + " · " + rm.Run.Profile, State: state})
		} else if s.streaming && m.confirmOpen && m.confirmKind == confirmKindAITool {
			out = append(out, agentroll.Item{Name: "AI chat", State: "waiting"})
		} else if s.streaming {
			out = append(out, agentroll.Item{Name: "AI chat", State: "working"})
		}
		for _, b := range s.bgRuns {
			if !b.done {
				out = append(out, agentroll.Item{Name: "run " + b.id + " (background)", State: "working"})
			}
		}
	}
	for _, t := range m.terminalTabs {
		if t.Agent == "" {
			continue
		}
		switch t.AgentState {
		case agents.Working:
			out = append(out, agentroll.Item{Name: t.agentTitle(), State: "working"})
		case agents.Waiting:
			out = append(out, agentroll.Item{Name: t.agentTitle(), State: "waiting"})
		}
	}
	return out
}

// agentRollupSync writes the roll-up when it changed.
func (m *Model) agentRollupSync() {
	items := m.agentRollupItems()
	var sig strings.Builder
	for _, it := range items {
		sig.WriteString(it.Name + "=" + it.State + ";")
	}
	if sig.String() == m.agentRollSig {
		return
	}
	m.agentRollSig = sig.String()
	dir, err := agentroll.Dir()
	if err != nil {
		return
	}
	r := agentroll.Rollup{Workspace: aiWorkspaceRoot(), PID: os.Getpid(), UpdatedMs: clock.Now().UnixMilli(), Items: items}
	if err := agentroll.Write(dir, r); err != nil {
		recordError("[agents] roll-up: " + err.Error())
	}
}

// agentRollupRemove drops this window's file (quit).
func (m *Model) agentRollupRemove() {
	if dir, err := agentroll.Dir(); err == nil && m.agentRollSig != "" {
		agentroll.Remove(dir, os.Getpid())
	}
}

// agentRollupOthers are the other windows' roll-ups.
func agentRollupOthers() []agentroll.Rollup {
	dir, err := agentroll.Dir()
	if err != nil {
		return nil
	}
	return agentroll.List(dir, os.Getpid(), pidAlive)
}
