package webproto

import "testing"

// Spec (GITX-21 on the web): git.peek reads the one change under a line
// of a saved workspace file (its exact hunk header, its rows, which of
// how many it is, whether it is staged, where every change starts): a
// v2 read for viewers, not audited. Stage, Unstage and Revert… on the
// peek card reuse git.stage_hunk, git.unstage_hunk and git.discard_hunk.
func TestGitPeekOp(t *testing.T) {
	checkArgs(t, OpGitPeek, []string{`{"path":"","line":1}`, `{"path":"../x","line":1}`, `{"path":"dir/","line":1}`, `{"path":"a.go","line":0}`, `{"path":"a.go","line":-3}`},
		[]string{`{"path":"src/a.go","line":12}`})
	checkRow(t, OpGitPeek, RoleViewer, false, AuditNo)
}
