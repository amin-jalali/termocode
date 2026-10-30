package git

import (
	"strings"
	"testing"
)

// FileCommits lists the commits that changed one file, newest first,
// across a rename.
func TestFileCommitsFollowRenames(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "one\n")
	r.write(t, "other.txt", "x\n")
	r.git(t, "add", "a.txt", "other.txt")
	r.git(t, "commit", "-q", "-m", "first")
	r.write(t, "other.txt", "y\n")
	r.git(t, "commit", "-q", "-am", "only other")
	r.git(t, "mv", "a.txt", "b.txt")
	r.git(t, "commit", "-q", "-m", "rename")
	r.write(t, "b.txt", "one\ntwo\n")
	r.write(t, "other.txt", "z\n")
	r.git(t, "commit", "-q", "-am", "both")

	cs, err := FileCommits(r.dir, "b.txt", 10)
	if err != nil {
		t.Fatal(err)
	}
	var subj []string
	for _, c := range cs {
		subj = append(subj, c.Subject)
		if c.Hash == "" || c.Author != "Test" || c.When == 0 {
			t.Errorf("commit %+v", c)
		}
	}
	if got := strings.Join(subj, ","); got != "both,rename,first" {
		t.Errorf("commits %q, want both,rename,first", got)
	}
	if cs, _ := FileCommits(r.dir, "b.txt", 1); len(cs) != 1 {
		t.Errorf("limit 1 gave %d", len(cs))
	}
	if cs, err := FileCommits(r.dir, "nothere.txt", 10); err != nil || len(cs) != 0 {
		t.Errorf("unknown file: %v %v", cs, err)
	}

}

// ShowCapped with a path shows only that file of the commit.
func TestShowCappedOneFile(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "one\n")
	r.write(t, "other.txt", "x\n")
	r.git(t, "add", "a.txt", "other.txt")
	r.git(t, "commit", "-q", "-m", "first")
	r.write(t, "a.txt", "one\ntwo\n")
	r.write(t, "other.txt", "z\n")
	r.git(t, "commit", "-q", "-am", "both")
	head, diff, _, err := ShowCapped(r.dir, "HEAD", 1<<20, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if head.Subject != "both" || !strings.Contains(diff, "+two") || strings.Contains(diff, "other.txt") {
		t.Errorf("one file's show %+v %q", head, diff)
	}
}
