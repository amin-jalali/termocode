package ai

// G-WT: agents that run at the same time in their own git worktree.

// WorktreesOff is the ai_worktrees value that keeps every agent in the
// user's tree. Anything else (the default "") gives each agent that runs
// at the same time as another writing agent its own worktree, in a git
// repository.
const WorktreesOff = "off"

// UseWorktrees reports whether parallel agents get their own worktree.
func (s Settings) UseWorktrees() bool { return s.Worktrees != WorktreesOff }

// States of a step's worktree (StoredWorktree.State).
const (
	WorktreeRunning   = "running"   // the step works in it
	WorktreeReady     = "ready"     // the step ended: review, merge or discard
	WorktreeConflict  = "conflict"  // a merge found files the user changed too
	WorktreeMerged    = "merged"    // applied to the user's tree; removed
	WorktreeKept      = "kept"      // merged with conflicts: worktree removed, branch kept
	WorktreeOnBranch  = "branch"    // not merged: worktree removed, the work stays on the branch
	WorktreeDiscarded = "discarded" // removed with its branch
)

// StoredWorktree is the git worktree a step ran in.
type StoredWorktree struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
	State  string `json:"state,omitempty"`
	// Files is how many files the step changed in it (when it ended).
	Files int `json:"files,omitempty"`
	// Conflicts are the files a merge found changed on both sides.
	Conflicts []string `json:"conflicts,omitempty"`
}

// Open reports whether the worktree still waits for Merge or Discard.
func (w *StoredWorktree) Open() bool {
	return w != nil && w.State != WorktreeMerged && w.State != WorktreeDiscarded && w.State != WorktreeKept && w.State != WorktreeOnBranch
}
