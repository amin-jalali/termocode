package webproto

// A new branch from the phone's Branches sheet ("Find or create a
// branch", "+ New branch from main"): the laptop's Git: Create Branch
// From… for the web. git.branch_create makes the branch where HEAD is
// and switches to it (git checkout -b), after a laptop card; the changes
// in the work tree go with it.

// OpGitBranchCreate makes a new branch at HEAD and switches to it.
const OpGitBranchCreate = "git.branch_create"

// GitBranchCreateArgs names the new branch.
type GitBranchCreateArgs struct {
	Name string `json:"name"`
}

func (a *GitBranchCreateArgs) validate(int) error {
	if len(a.Name) > MaxBranch || !validRefName(a.Name) {
		return invalid("bad branch name")
	}
	return nil
}

var opGitBranchRows = []Policy{
	{Op: OpGitBranchCreate, New: newOf[GitBranchCreateArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

// The rows join the table before any init() runs (as ops_more.go's).
var _ = func() bool { opTable = append(opTable, opGitBranchRows...); return true }()

func init() {
	registerResult(OpGitBranchCreate, AskRef{})
}
