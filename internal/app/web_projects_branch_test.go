//go:build unix

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/session"
)

// Spec (WEB-55): a project row of the session picker shows the branch the
// folder is on, read from .git/HEAD (no git process). A branch name is one
// clean line; a detached HEAD shows the short commit; no repo: no branch.
func TestProjectsMergeShowsBranch(t *testing.T) {
	in := projectsInput{
		sessions: []session.Info{{Workspace: "/home/u/src/app", PID: 8}, {Workspace: "/srv/web", PID: 7}},
		branches: map[string]string{"/home/u/src/app": "feat/x", "/srv/web": "main"},
	}
	s := projectsMerge("/home/u/src/app", nil, &in, "/home/u")
	app, _ := projByName(s, "app")
	web, _ := projByName(s, "web")
	if app.Branch != "feat/x" || web.Branch != "main" {
		t.Fatalf("branches app=%q web=%q", app.Branch, web.Branch)
	}
}

func TestReadBranchFromHEAD(t *testing.T) {
	dir := t.TempDir()
	if got := readBranch(dir); got != "" {
		t.Fatalf("no repo: %q", got)
	}
	git := filepath.Join(dir, ".git")
	if err := os.Mkdir(git, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(git, "HEAD"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("ref: refs/heads/feat/web-55\n")
	if got := readBranch(dir); got != "feat/web-55" {
		t.Fatalf("branch: %q", got)
	}
	write("0123456789abcdef0123456789abcdef01234567\n")
	if got := readBranch(dir); got != "0123456" {
		t.Fatalf("detached: %q", got)
	}
	write("ref: refs/heads/bad\x1b[31mname\n")
	if got := readBranch(dir); got != "" {
		t.Fatalf("control bytes must give no branch: %q", got)
	}
}
