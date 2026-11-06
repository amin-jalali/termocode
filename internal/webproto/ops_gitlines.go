package webproto

import "strings"

// The line and hunk ops of the desktop's diff editor (WEBP-9, Git): the
// laptop's Git: Stage Line, Unstage Line, Discard Line (or Selected
// Lines) and Discard Hunk (at cursor). Staging and unstaging rows undo
// each other: no card. Discarding rewrites the working file and cannot
// be undone: the laptop says yes first.

// Line and hunk op names.
const (
	OpGitStageLines   = "git.stage_lines"
	OpGitUnstageLines = "git.unstage_lines"
	OpGitDiscardLines = "git.discard_lines"
	OpGitDiscardHunk  = "git.discard_hunk"
)

// MaxGitLineSpan caps the rows of one line op.
const MaxGitLineSpan = 5000

// GitLinesArgs names rows From..To (1-based, inclusive) of a changed
// file as saved on the laptop: the working file's rows, or with Index
// (git.unstage_lines only) the rows of the index's text, as a staged
// diff shows them.
type GitLinesArgs struct {
	Path  string `json:"path"`
	From  int    `json:"from"`
	To    int    `json:"to"`
	Index bool   `json:"index,omitempty"`
}

var opGitLineRows = []Policy{
	{Op: OpGitStageLines, New: newOf[GitLinesArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 8 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpGitUnstageLines, New: newOf[GitLinesArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 8 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpGitDiscardLines, New: newOf[GitLinesArgs](), Role: RoleController, Rate: slow, MaxBytes: 8 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpGitDiscardHunk, New: newOf[GitHunkArgs](), Role: RoleController, Rate: slow, MaxBytes: 8 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

func (a *GitLinesArgs) validate(int) error {
	if !validPath(a.Path, false) || strings.HasSuffix(a.Path, "/") {
		return invalid("bad path")
	}
	if a.From < 1 || a.To < a.From || a.To-a.From >= MaxGitLineSpan {
		return invalid("bad rows")
	}
	return nil
}

// The rows join the table before any init() runs (as ops_more.go's).
var _ = func() bool { opTable = append(opTable, opGitLineRows...); return true }()

func init() {
	registerResult(OpGitDiscardLines, AskRef{})
	registerResult(OpGitDiscardHunk, AskRef{})
}
