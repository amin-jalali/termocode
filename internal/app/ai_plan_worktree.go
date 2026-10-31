package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
)

// Run a plan in a worktree, then ship it (OMX-15): AI: Run Next Plan in a
// Worktree, Then Ship… arms the next plan run. That run works in one new
// git worktree (G-WT, made from the last commit: your uncommitted edits
// stay out of a branch that may be pushed), its steps one at a time, so
// your branch and files never change. When it is done a card (Not now
// first) offers to ship the branch: Ship commits the work in the worktree
// and starts AI: Ship Branch to Merge-Ready on it (ai_ship.go), where
// each push and the pull request ask first and CI fixes are made in the
// worktree too.

const confirmKindAIPlanShip confirmKindEnum = 2950

// The worktree calls (vars: tests use fakes, no git).
var (
	createPlanWorktree = git.CreateAgentWorktreeFromHead
	commitPlanWorktree = git.CommitAgentWorktree
)

// aiPlanWT is the plan-in-a-worktree state of the chat.
type aiPlanWT struct {
	armed bool            // the next plan run works in a worktree
	runID string          // the run that does
	task  string          // its message: the commit's subject
	wts   *aiRunWorktrees // its worktrees (whole: one for every step)
	ready *git.AgentWorktree
}

// aiPlanShipMsg: the plan's work was committed in its worktree.
type aiPlanShipMsg struct {
	wt  git.AgentWorktree
	err error
}

// toggleAIPlanWorktree arms the next plan run, or turns it off; with a
// finished plan's branch waiting it asks to ship it again.
func (m *Model) toggleAIPlanWorktree() tea.Cmd {
	s := m.aiS()
	pw := s.planWT
	switch {
	case pw != nil && pw.armed:
		pw.armed = false
		return m.aiInfo("The next plan works in your tree again")
	case pw != nil && pw.ready != nil && pw.runID == "":
		return m.askAIPlanShip()
	}
	m.gitEnsureLoaded()
	if !m.gitIsRepo {
		return m.aiInfo("A plan worktree needs a git repository: open a folder with one")
	}
	s.planWT = &aiPlanWT{armed: true}
	m.storeAIProfile(ai.ProfilePlan)
	return tea.Batch(m.aiInfo("Profile: plan · the next plan works in its own git worktree (your files stay as they are), then asks to ship it · type the task"),
		m.openAIPanel(true))
}

// aiPlanWTTake reports whether run (a planned run the user started)
// works in one worktree, and takes the armed state.
func (s *aiState) aiPlanWTTake(runID string, planned bool, by *ai.RunBy, task string) bool {
	pw := s.planWT
	if pw == nil || !pw.armed || !planned || by != nil {
		return false
	}
	pw.armed, pw.runID, pw.task, pw.ready = false, runID, task, nil
	return true
}

// aiPlanWTEnd runs when run rm ended: its steps' worktree is the
// branch now; a done run asks to ship it.
func (m *Model) aiPlanWTEnd(rm *ai.StoredMessage) {
	s := m.aiS()
	pw := s.planWT
	if pw == nil || pw.runID == "" || rm.Run == nil || rm.Run.ID != pw.runID {
		return
	}
	pw.runID = ""
	var wt *git.AgentWorktree
	if pw.wts != nil {
		wt = pw.wts.sharedWorktree()
	}
	pw.wts = nil
	if wt == nil {
		return // no step got to run
	}
	for k := range rm.Steps {
		if st := rm.Steps[k].Worktree; st != nil && st.ID == wt.ID {
			st.State = ai.WorktreeOnBranch
		}
	}
	pw.ready = wt
	if rm.Run.State == ai.RunStateDone && !m.confirmOpen {
		m.askAIPlanShip()
	}
}

// aiPlanSubject is the commit subject of the plan's work.
func aiPlanSubject(task string) string {
	sub := aiOneLine(task)
	if r := []rune(sub); len(r) > 72 {
		sub = string(r[:71]) + "…"
	}
	if sub == "" {
		sub = "termocode plan"
	}
	return sub
}

// askAIPlanShip shows the ship card of the finished plan (Not now first).
func (m *Model) askAIPlanShip() tea.Cmd {
	pw := m.aiS().planWT
	if pw == nil || pw.ready == nil {
		return nil
	}
	text := fmt.Sprintf("The plan's work is on branch %s, in its own git worktree. Your branch and files are as they were.\n\n"+
		"Ship commits that work there (%q) and starts Ship Branch to Merge-Ready on that branch: each push and the pull request asks first, and CI fixes are made in the worktree.\n\n"+
		"Not now keeps the branch: AI: Agent Worktrees… lists it, and AI: Run Next Plan in a Worktree… offers this again.",
		pw.ready.Branch, aiPlanSubject(pw.task))
	m.confirm = confirm.Risky("Ship the plan's branch?", text,
		confirm.Button{ID: "cancel", Title: "Not now"},
		confirm.Button{ID: "ship", Title: "Ship", Style: confirm.StylePrimary}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIPlanShip
	return nil
}

// answerAIPlanShip answers the card: Ship commits and starts the ship;
// anything else (Esc too) keeps the branch.
func (m *Model) answerAIPlanShip(id string) tea.Cmd {
	pw := m.aiS().planWT
	if pw == nil || pw.ready == nil {
		return nil
	}
	wt := *pw.ready
	if id != "ship" {
		return m.aiInfo("Not shipped: the work stays on branch " + wt.Branch)
	}
	pw.ready = nil
	root, sub := aiWorkspaceRoot(), aiPlanSubject(pw.task)
	return func() tea.Msg {
		_, err := commitPlanWorktree(root, wt.ID, sub)
		return aiPlanShipMsg{wt: wt, err: err}
	}
}

// onAIPlanShip starts the ship on the committed branch.
func (m *Model) onAIPlanShip(msg aiPlanShipMsg) tea.Cmd {
	if msg.err != nil {
		if pw := m.aiS().planWT; pw != nil {
			pw.ready = &msg.wt // the command offers it again
		}
		return m.aiInfo("Could not commit the plan's work: " + ai.Redact(aiOneLine(msg.err.Error())) + " · nothing was pushed")
	}
	return m.startAIShip(msg.wt.Branch, msg.wt.Dir())
}
