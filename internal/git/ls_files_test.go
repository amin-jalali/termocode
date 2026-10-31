package git

import (
	"slices"
	"testing"
)

// TrackedFiles lists the index paths raw (a space, a non-ASCII name),
// relative to the top, and none in an empty repo.
func TestTrackedFiles(t *testing.T) {
	r := newGitTestRepo(t)
	if got, err := TrackedFiles(r.dir); err != nil || len(got) != 0 {
		t.Fatalf("empty repo: %q %v", got, err)
	}
	r.write(t, "a b.txt", "x\n")
	r.write(t, "日.txt", "y\n")
	r.git(t, "add", ".")
	got, err := TrackedFiles(r.dir)
	if err != nil || !slices.Equal(got, []string{"a b.txt", "日.txt"}) {
		t.Errorf("TrackedFiles = %q %v", got, err)
	}
}
