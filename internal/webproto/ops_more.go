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

// Container op names (WEB-44). Reads list and tail the logs; start and
// restart ask in the page; stop and remove wait for the laptop.
const (
	OpContainersList    = "containers.list"
	OpContainersLogs    = "containers.logs"
	OpContainersStart   = "containers.start"
	OpContainersRestart = "containers.restart"
	OpContainersStop    = "containers.stop"
	OpContainersRemove  = "containers.remove"
)

// Database op names (WEB-43). Browsing is a read; a typed read query is
// a controller op (the page asks first); anything the classifier does not
// call a read waits for the laptop.
const (
	OpDBConns  = "db.conns"
	OpDBTables = "db.tables"
	OpDBRows   = "db.rows"
	OpDBQuery  = "db.query"
	OpDBExec   = "db.exec"
)

// File transfer op names (WEB-49). Bytes travel base64 in chunks inside
// the JSON frames; every chunk is one request, so the rate and size rules
// of the table hold for them.
const (
	OpFileDownload      = "file.download"
	OpFileUploadBegin   = "file.upload_begin"
	OpFileUploadChunk   = "file.upload_chunk"
	OpFileUploadSave    = "file.upload_save"
	OpFileUploadReplace = "file.upload_replace"
	OpFileUploadCancel  = "file.upload_cancel"
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

	// Databases.
	{Op: OpDBConns, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpDBTables, New: newOf[DBConnArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpDBRows, New: newOf[DBRowsArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 2 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpDBQuery, New: newOf[DBSQLArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 72 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpDBExec, New: newOf[DBSQLArgs](), Role: RoleController, Rate: slow, MaxBytes: 72 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},

	// File transfer: controller only; replacing a file waits for the laptop.
	{Op: OpFileDownload, New: newOf[DownloadArgs](), Role: RoleController, Rate: perSec(40), MaxBytes: 8 * kib, Live: 1, Audit: AuditCounted, Phase: PhaseV2},
	{Op: OpFileUploadBegin, New: newOf[UploadBeginArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 8 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpFileUploadChunk, New: newOf[UploadChunkArgs](), Role: RoleController, Rate: perSec(40), MaxBytes: TransferChunk*4/3 + 2*kib, BytesPerSec: 32 * mib, Live: 1, Audit: AuditCounted, Phase: PhaseV2},
	{Op: OpFileUploadSave, New: newOf[UploadArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpFileUploadReplace, New: newOf[UploadArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpFileUploadCancel, New: newOf[UploadArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},

	// Containers.
	{Op: OpContainersList, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpContainersLogs, New: newOf[ContainerLogsArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpContainersStart, New: newOf[ContainerArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpContainersRestart, New: newOf[ContainerArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpContainersStop, New: newOf[ContainerArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpContainersRemove, New: newOf[ContainerArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

// The rows join the table before any init() runs (package variables are
// set first), so the result registrations of the view files find them.
var _ = func() bool { opTable = append(opTable, opMoreRows...); return true }()
