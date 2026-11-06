package webproto

import "strings"

// The gutter peek of the desktop editor (GITX-21 on the web; the
// laptop's Git: Peek Change Under Cursor): the one change under a line of
// a saved workspace file. A read for viewers; the card's Stage, Unstage
// and Revert… use git.stage_hunk, git.unstage_hunk and git.discard_hunk
// with the hunk header it answers.

// OpGitPeek is the peek op's name.
const OpGitPeek = "git.peek"

// GitPeekArgs names a 1-based line of a workspace file.
type GitPeekArgs struct {
	Path string `json:"path"`
	Line int    `json:"line"`
}

func (a *GitPeekArgs) validate(int) error {
	if !validPath(a.Path, false) || strings.HasSuffix(a.Path, "/") {
		return invalid("bad path")
	}
	if a.Line < 1 {
		return invalid("bad line")
	}
	return nil
}

// GitPeek is the change under the line: its exact header, its rows
// (" context", "-old", "+new"), which of how many changes it is, whether
// it is only in the index, and the first changed line of every change
// (where its gutter mark is).
type GitPeek struct {
	Path   string   `json:"path"`
	Line   int      `json:"line"`
	Header string   `json:"header"`
	Lines  []string `json:"lines"`
	Index  int      `json:"index"`
	Count  int      `json:"count"`
	Staged bool     `json:"staged,omitempty"`
	Starts []int    `json:"starts"`
}

var opGitPeekRows = []Policy{
	{Op: OpGitPeek, New: newOf[GitPeekArgs](), Role: RoleViewer, Rate: perSec(4), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opGitPeekRows...); return true }()

func init() {
	registerResult(OpGitPeek, GitPeek{})
}
