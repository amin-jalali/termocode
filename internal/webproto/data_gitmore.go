package webproto

// Results of the git ops of WEB-42.

func init() {
	registerResult(OpGitBranches, GitBranches{})
	for _, op := range []string{OpGitPush, OpGitPull, OpGitCheckout, OpGitStash, OpGitStashPop} {
		registerResult(op, AskRef{})
	}
}

// MaxGitBranches caps the branches listed.
const MaxGitBranches = 500

// GitBranches answers git.branches: the local branches and the stash.
type GitBranches struct {
	Current  string   `json:"current"`
	Branches []string `json:"branches"`
	More     int      `json:"more,omitempty"` // branches past MaxGitBranches
	Stashes  int      `json:"stashes"`        // entries in the stash
	Upstream bool     `json:"upstream"`       // the branch tracks a remote branch
}
