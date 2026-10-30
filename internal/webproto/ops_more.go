package webproto

import "time"

// The ops of the v2 views on files, git, the database, containers and
// transfers (WEB-41 … WEB-49). They join the op table below; every rule
// of ops.go holds for them (one row each, the gateway and the hub read
// the same row).

// File op names (WEB-41).
const (
	OpTreeNewFile = "tree.new_file"
	OpTreeNewDir  = "tree.new_dir"
	OpTreeRename  = "tree.rename"
	OpTreeDelete  = "tree.delete"
)

// Git op names (WEB-42). Every one that changes the repository or talks
// to the remote waits for a laptop card; credentials stay on the laptop
// (git runs there with its own credential helper, no prompt).
const (
	OpGitBranches = "git.branches"
	OpGitPush     = "git.push"
	OpGitPull     = "git.pull"
	OpGitCheckout = "git.checkout"
	OpGitStash    = "git.stash"
	OpGitStashPop = "git.stash_pop"
)

// slow is the rate of an op that waits for the laptop.
var slow = Rate{N: 1, Per: 5 * time.Second}

var opMoreRows = []Policy{
	// Files: create and rename at once (an in-page confirm for rename);
	// delete waits for the laptop.
	{Op: OpTreeNewFile, New: newOf[ItemArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 8 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpTreeNewDir, New: newOf[ItemArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 8 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpTreeRename, New: newOf[RenameArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 8 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpTreeDelete, New: newOf[ItemArgs](), Role: RoleController, Rate: slow, MaxBytes: 8 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},

	// Git: the branch list is a read; the rest waits for the laptop.
	{Op: OpGitBranches, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpGitPush, New: newOf[EmptyArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpGitPull, New: newOf[EmptyArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpGitCheckout, New: newOf[GitCheckoutArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpGitStash, New: newOf[GitStashArgs](), Role: RoleController, Rate: slow, MaxBytes: 2 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpGitStashPop, New: newOf[EmptyArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

// The rows join the table before any init() runs (package variables are
// set first), so the result registrations of the view files find them.
var _ = func() bool { opTable = append(opTable, opMoreRows...); return true }()
