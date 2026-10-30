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

var opMoreRows = []Policy{
	// Files: create and rename at once (an in-page confirm for rename);
	// delete waits for the laptop.
	{Op: OpTreeNewFile, New: newOf[ItemArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 8 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpTreeNewDir, New: newOf[ItemArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 8 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpTreeRename, New: newOf[RenameArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 8 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpTreeDelete, New: newOf[ItemArgs](), Role: RoleController, Rate: Rate{N: 1, Per: 5 * time.Second}, MaxBytes: 8 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

// The rows join the table before any init() runs (package variables are
// set first), so the result registrations of the view files find them.
var _ = func() bool { opTable = append(opTable, opMoreRows...); return true }()
