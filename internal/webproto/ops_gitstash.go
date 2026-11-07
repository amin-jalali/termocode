package webproto

// The STASHES section of the desktop's Source Control view (GITX-10), as
// the laptop's panel has it: the git topic lists the stash; git.stash_show
// reads one entry's changes; git.stash_act applies, pops, drops or makes
// a branch from one entry, each after a laptop card. Both take only an
// entry the laptop has on show.

// Stash panel op names.
const (
	OpGitStashShow = "git.stash_show"
	OpGitStashAct  = "git.stash_act"
)

// Stash actions of git.stash_act.
const (
	StashApply  = "apply"  // apply, keep the entry
	StashPop    = "pop"    // apply, then drop the entry
	StashDrop   = "drop"   // delete the entry
	StashBranch = "branch" // a new branch from the entry's commit, the entry applied there and dropped
)

// GitStashShowArgs names a stash entry on show; From pages its diff.
type GitStashShowArgs struct {
	Ref  string `json:"ref"`
	From int    `json:"from,omitempty"`
}

// GitStashActArgs is one action on a stash entry on show. Branch is the
// new branch's name (do "branch" only).
type GitStashActArgs struct {
	Ref    string `json:"ref"`
	Do     string `json:"do" enum:"apply|pop|drop|branch"`
	Branch string `json:"branch,omitempty"`
}

var opGitStashRows = []Policy{
	{Op: OpGitStashShow, New: newOf[GitStashShowArgs](), Role: RoleViewer, Rate: perSec(10), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpGitStashAct, New: newOf[GitStashActArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

func (a *GitStashShowArgs) validate(int) error {
	if stashIndex(a.Ref) < 0 {
		return invalid("bad stash ref")
	}
	if a.From < 0 {
		return invalid("bad page")
	}
	return nil
}

func (a *GitStashActArgs) validate(int) error {
	if stashIndex(a.Ref) < 0 {
		return invalid("bad stash ref")
	}
	switch a.Do {
	case StashApply, StashPop, StashDrop:
		if a.Branch != "" {
			return invalid("a branch name is for do branch only")
		}
	case StashBranch:
		if len(a.Branch) > MaxBranch || !validRefName(a.Branch) {
			return invalid("bad branch")
		}
	default:
		return invalid("bad stash action")
	}
	return nil
}

// The rows join the table before any init() runs (as ops_more.go's).
var _ = func() bool { opTable = append(opTable, opGitStashRows...); return true }()

func init() {
	registerResult(OpGitStashShow, GitDiff{})
	registerResult(OpGitStashAct, AskRef{})
}
