package git

import (
	"reflect"
	"strings"
	"testing"
)

// BaseDiffFiles is what a lane changed since it left its base: commits
// on the branch, uncommitted edits and new files git does not ignore.
// The base side is the merge base, so commits made on main later do not
// show up as the lane's work.
func TestBaseDiffFilesSinceTheMergeBase(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "one\n")
	r.write(t, "b.txt", "keep\n")
	r.write(t, ".gitignore", "*.log\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "first")
	r.git(t, "checkout", "-q", "-b", "lane")
	r.write(t, "a.txt", "two\n")
	r.git(t, "commit", "-q", "-am", "lane edit")
	r.write(t, "b.txt", "changed, not committed\n")
	r.write(t, "new/c.txt", "new\n")
	r.write(t, "noise.log", "ignored\n")
	// main moves on after the lane left it
	r.git(t, "checkout", "-q", "main")
	r.write(t, "main-only.txt", "later\n")
	r.git(t, "add", "main-only.txt")
	r.git(t, "commit", "-q", "-m", "main later")
	r.git(t, "checkout", "-q", "lane")

	mb, files, err := BaseDiffFiles(r.dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	first := strings.TrimSpace(r.git(t, "rev-parse", "main~1"))
	if mb != first {
		t.Errorf("merge base %s, want %s", mb, first)
	}
	if want := []string{"a.txt", "b.txt", "new/c.txt"}; !reflect.DeepEqual(files, want) {
		t.Errorf("files %q, want %q", files, want)
	}
	if _, _, err := BaseDiffFiles(r.dir, "--output=x"); err == nil {
		t.Error("a base that looks like an option is used")
	}
}
