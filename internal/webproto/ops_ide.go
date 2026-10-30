package webproto

// The ops of the desktop workbench's explorer sections: the TIMELINE
// (the commits of one file). They join the op table below; every rule of
// ops.go holds for them.

// Explorer op names.
const (
	OpGitFileLog = "git.file_log"
)

// GitFileLogArgs names the file whose commits the timeline lists.
type GitFileLogArgs struct {
	Path string `json:"path"`
}

// GitFileLog is the result of git.file_log: the last commits that
// changed the file (following renames), newest first.
type GitFileLog struct {
	Path string      `json:"path"`
	Log  []GitCommit `json:"log"`
}

// MaxFileLog caps the commits of one git.file_log.
const MaxFileLog = 50

var opIDERows = []Policy{
	{Op: OpGitFileLog, New: newOf[GitFileLogArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 8 * kib, Audit: AuditNo, Phase: PhaseV2},
}

func (a *GitFileLogArgs) validate(int) error {
	if !validPath(a.Path, false) {
		return invalid("bad path")
	}
	return nil
}

// The rows join the table before any init() runs (as ops_more.go's).
var _ = func() bool { opTable = append(opTable, opIDERows...); return true }()

func init() {
	registerResult(OpGitFileLog, GitFileLog{})
}
