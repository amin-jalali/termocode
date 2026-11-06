package wire

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/workflow/run"
)

// Worktrees is the run.Each of a workflow run (WFX-10): an each with
// worktree: true gives every item its own agent worktree (the same code
// as the AI's agent worktrees, so the worktree list, review and discard
// screens show them), from the each's base branch.
type Worktrees struct {
	// Workspace is the repository the worktrees branch from.
	Workspace string
	// RunID names the run; the worktree id is run + step + item.
	RunID string
}

// worktreeID is the agent worktree id of one item of one each step. A
// short hash of the key keeps two keys that clean to the same text apart.
func worktreeID(runID, step, key string) string {
	sum := sha256.Sum256([]byte(key))
	return git.AgentWorktreeID("wf-"+runID, step+"-"+key+"-"+hex.EncodeToString(sum[:3]))
}

// Enter makes (or finds again, on a retry or a resume) the item's
// worktree. An each without worktree: true gets no environment.
func (w *Worktrees) Enter(_ context.Context, c run.EachCall) (run.ItemEnv, error) {
	if c.Step == nil || c.Step.Each == nil || !c.Step.Each.Worktree {
		return run.ItemEnv{}, nil
	}
	id := worktreeID(w.RunID, c.Step.ID, c.Item.Key)
	wt, err := git.CreateAgentWorktreeFrom(w.Workspace, id, c.Base)
	if err != nil {
		return run.ItemEnv{}, err
	}
	return run.ItemEnv{Worktree: wt.Dir(), ID: wt.ID, Branch: wt.Branch}, nil
}

// Leave ends an item: cleanup "discard" removes the worktree of a good
// item (its branch stays, so the work is still in git); every other item
// keeps its worktree for review.
func (w *Worktrees) Leave(_ context.Context, c run.EachCall, env run.ItemEnv, ok bool) error {
	if env.ID == "" || !ok || c.Step == nil || c.Step.Each == nil || c.Step.Each.Cleanup != "discard" {
		return nil
	}
	return git.RemoveAgentWorktree(w.Workspace, env.ID, false)
}
