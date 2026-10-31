package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/charmbracelet/x/ansi"
)

// aiPlanWTFakes makes the plan worktree in a temp folder and records the
// commit (no git).
func aiPlanWTFakes(t *testing.T) (dir string, commits *[]string) {
	t.Helper()
	dir = t.TempDir()
	var made []string
	oldCreate, oldCommit := createPlanWorktree, commitPlanWorktree
	createPlanWorktree = func(_, id string) (git.AgentWorktree, error) {
		return git.AgentWorktree{ID: id, Path: dir, Branch: git.AgentBranchPrefix + id}, nil
	}
	commitPlanWorktree = func(_, id, msg string) (bool, error) { made = append(made, id+": "+msg); return true, nil }
	t.Cleanup(func() { createPlanWorktree, commitPlanWorktree = oldCreate, oldCommit })
	return dir, &made
}

// OMX-15: AI: Run Next Plan in a Worktree makes the next plan run work
// in one new worktree (its steps one at a time), then asks (Not now
// first) to ship the branch; Ship commits the work there and starts the
// ship loop on that branch and folder.
func TestAIPlanRunInWorktreeThenShip(t *testing.T) {
	wt, commits := aiPlanWTFakes(t)
	p := &funcAIProvider{reply: planReply(appDiamondPlan)}
	m := aiPlanModel(t, p)
	m.ai.profile = ""
	m.gitIsRepo, m.gitBranch.Name, m.gitLoaded = true, "main", true
	f := &aiShipFake{probes: []aiShipProbe{{branch: "x", base: "main"}}}
	var shipDirs []string
	old := aiShipOpsFor
	aiShipOpsFor = func(_ *Model, dir string) aiShipOps { shipDirs = append(shipDirs, dir); return f }
	t.Cleanup(func() { aiShipOpsFor = old })

	m.toggleAIPlanWorktree()
	if m.ai.profile != ai.ProfilePlan || m.ai.planWT == nil || !m.ai.planWT.armed {
		t.Fatalf("not armed: profile %q", m.ai.profile)
	}
	driveAIChat(t, m, m.sendAIChat("make the parser faster"), nil)
	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateDone {
		t.Fatalf("run %+v", rm.Run)
	}
	p.mu.Lock()
	for _, r := range p.reqs {
		if !isPlanner(r) && !strings.Contains(r.Messages[0].Content, "Workspace root: "+wt) {
			t.Errorf("a step did not work in the worktree:\n%s", r.Messages[0].Content)
		}
	}
	p.mu.Unlock()
	if m.ai.planWT.armed {
		t.Fatal("still armed after the run")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindAIPlanShip || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no Not-now-first ship card (open %v kind %v)", m.confirmOpen, m.confirmKind)
	}
	m.confirm.SetSize(160, 50)
	if body := strings.Join(strings.Fields(ansi.Strip(m.confirm.View())), " "); !strings.Contains(body, git.AgentBranchPrefix) || !strings.Contains(body, "asks first") {
		t.Fatalf("card text:\n%s", body)
	}
	cmd := m.answerAIPlanShip("ship")
	msg := cmd()
	m.handleAIShipMsg(msg)
	if len(*commits) != 1 || !strings.Contains((*commits)[0], "make the parser faster") {
		t.Fatalf("commits %v", *commits)
	}
	if sh := m.ai.ship; sh == nil || sh.dir != wt || !strings.HasPrefix(sh.branch, git.AgentBranchPrefix) || len(shipDirs) != 1 || shipDirs[0] != wt {
		t.Fatalf("ship %+v dirs %v", m.ai.ship, shipDirs)
	}
}

// Not now keeps the branch and starts nothing; the command again before a
// run turns it off; outside a git repository it says why.
func TestAIPlanWorktreeNotNowAndOff(t *testing.T) {
	aiPlanWTFakes(t)
	p := &funcAIProvider{reply: planReply(appDiamondPlan)}
	m := aiPlanModel(t, p)
	m.gitIsRepo, m.gitBranch.Name, m.gitLoaded = false, "", true
	m.toggleAIPlanWorktree()
	if m.ai.planWT != nil && m.ai.planWT.armed {
		t.Fatal("armed outside a git repository")
	}
	m.gitIsRepo, m.gitBranch.Name = true, "main"
	m.toggleAIPlanWorktree()
	m.toggleAIPlanWorktree()
	if m.ai.planWT != nil && m.ai.planWT.armed {
		t.Fatal("the command again did not turn it off")
	}
	m.toggleAIPlanWorktree()
	driveAIChat(t, m, m.sendAIChat("make the parser faster"), nil)
	if !m.confirmOpen || m.confirmKind != confirmKindAIPlanShip {
		t.Fatal("no ship card")
	}
	if cmd := m.answerAIPlanShip("cancel"); m.ai.ship != nil {
		t.Fatalf("Not now started a ship (cmd %v)", cmd != nil)
	}
}
