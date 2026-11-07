package git

import (
	"strings"
	"testing"
)

// CMP-33: CommitAll commits every change of the worktree (new, changed,
// deleted and renamed files) in one commit; an empty message and a clean
// tree are errors and commit nothing.
func TestCommitAll(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "a\n")
	r.write(t, "old.txt", "old\n")
	r.write(t, "gone.txt", "gone\n")
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "init")

	r.write(t, "a.txt", "a changed\n")
	r.write(t, "new.txt", "new\n")
	r.git(t, "mv", "old.txt", "renamed.txt")
	r.git(t, "rm", "-q", "gone.txt")

	if err := CommitAll(r.dir, "  "); err == nil {
		t.Fatal("an empty message was committed")
	}
	if err := CommitAll(r.dir, "ship: all of it"); err != nil {
		t.Fatal(err)
	}
	if st, _ := Status(r.dir); len(st) != 0 {
		t.Fatalf("left over after the commit: %+v", st)
	}
	files := r.git(t, "show", "--name-status", "--format=%s", "HEAD")
	for _, want := range []string{"ship: all of it", "a.txt", "new.txt", "renamed.txt", "gone.txt"} {
		if !strings.Contains(files, want) {
			t.Errorf("the commit lacks %q:\n%s", want, files)
		}
	}
	if err := CommitAll(r.dir, "again"); err == nil {
		t.Error("a clean tree made a commit")
	}
}
