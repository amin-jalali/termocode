package app

import (
	"path/filepath"
	"strings"
	"testing"
)

// GITX-29 (O24, O25): Copy Remote Link to Line copies the web link of the
// cursor line at HEAD and says when HEAD is not on the remote yet; Copy
// Remote Link to Commit copies the commit on show (or HEAD).
func TestGitxCopyPermalinks(t *testing.T) {
	m, dir := covGitRepo(t)
	var got []string
	sink := func(s string) { got = append(got, s) }
	clipboardSink.Store(&sink)
	t.Cleanup(func() { clipboardSink.Store(nil) })

	m.editor.SetMeta(filepath.Join(dir, "a.txt"), false, "")
	m.cursorLine = 2
	m.dispatchPaletteAction("git-copy-line-link")
	if len(got) != 0 || !toastHas(m, "No remote") {
		t.Fatalf("no remote: copied %v", got)
	}
	covGit(t, dir, "remote", "add", "origin", "git@github.com:o/r.git")
	head := covGit(t, dir, "rev-parse", "HEAD")
	m.dispatchPaletteAction("git-copy-line-link")
	if len(got) != 1 || got[0] != "https://github.com/o/r/blob/"+head+"/a.txt#L2" {
		t.Fatalf("line link %v", got)
	}
	if !toastHas(m, "not on the remote") {
		t.Error("no warning for an unpushed HEAD")
	}
	m.dispatchPaletteAction("git-copy-commit-link")
	if len(got) != 2 || got[1] != "https://github.com/o/r/commit/"+head {
		t.Fatalf("commit link %v", got)
	}
	for _, id := range []string{"git-copy-line-link", "git-copy-commit-link"} {
		if _, ok := aiActionPolicies[id]; !ok {
			t.Errorf("%s: no AI policy", id)
		}
	}
	_ = strings.TrimSpace
}
