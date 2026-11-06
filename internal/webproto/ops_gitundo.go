package webproto

import "strings"

// Undo, redo and the reflog from the desktop (WEBP-9, Git): the laptop's
// Git: Undo Last Git Action…, Git: Redo Undone Git Action and Git:
// Reflog…. git.undo_plan reads what the undo (or a move of HEAD to a
// reflog line) would do, in the laptop's own words, and keeps that plan;
// git.undo runs it and git.redo takes the last undo back, each after a
// laptop card. Nothing here ever force-pushes or drops local changes.

// Undo op names.
const (
	OpGitReflog   = "git.reflog"
	OpGitUndoPlan = "git.undo_plan"
	OpGitUndo     = "git.undo"
	OpGitRedo     = "git.redo"
)

// MaxGitReflog caps the lines of git.reflog (the laptop's list is as long).
const MaxGitReflog = 50

// GitReflog is the result of git.reflog: HEAD's moves, newest first (the
// first is where HEAD is).
type GitReflog struct {
	Entries []GitReflogEntry `json:"entries"`
}

// GitReflogEntry is one move of HEAD: where it went (short hash), what
// the move was in plain words, and when (Unix ms).
type GitReflogEntry struct {
	Hash    string `json:"hash"`
	Summary string `json:"summary"`
	When    int64  `json:"when"`
}

// GitUndoPlanArgs plans the undo of the last git action, or (Reflog) a
// move of HEAD to line Index of the last git.reflog, whose short hash is
// Hash (a list that changed since is refused).
type GitUndoPlanArgs struct {
	Reflog bool   `json:"reflog,omitempty"`
	Index  int    `json:"index,omitempty"`
	Hash   string `json:"hash,omitempty"`
}

// GitUndoPlan is the result of git.undo_plan: the laptop card's title,
// text and action, as the page asks before git.undo.
type GitUndoPlan struct {
	Title  string `json:"title"`
	Text   string `json:"text"`
	Action string `json:"action"`
	Move   bool   `json:"move,omitempty"`
}

// GitUndoArgs runs the plan git.undo_plan gave last (Move: a reflog move).
type GitUndoArgs struct {
	Move bool `json:"move,omitempty"`
}

var opGitUndoRows = []Policy{
	{Op: OpGitReflog, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpGitUndoPlan, New: newOf[GitUndoPlanArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpGitUndo, New: newOf[GitUndoArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpGitRedo, New: newOf[EmptyArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

func (a *GitUndoPlanArgs) validate(int) error {
	if !a.Reflog {
		if a.Index != 0 || a.Hash != "" {
			return invalid("a reflog line without reflog")
		}
		return nil
	}
	if a.Index < 0 || a.Index >= MaxGitReflog || len(a.Hash) < 7 || len(a.Hash) > 40 || strings.Trim(a.Hash, "0123456789abcdef") != "" {
		return invalid("bad reflog line")
	}
	return nil
}

// The rows join the table before any init() runs (as ops_more.go's).
var _ = func() bool { opTable = append(opTable, opGitUndoRows...); return true }()

func init() {
	registerResult(OpGitReflog, GitReflog{})
	registerResult(OpGitUndoPlan, GitUndoPlan{})
	registerResult(OpGitUndo, AskRef{})
	registerResult(OpGitRedo, AskRef{})
}
