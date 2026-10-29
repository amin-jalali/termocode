package git

import (
	"strings"
	"testing"
)

// A conflicted file whose name has a space or non-ASCII letters is
// reported by its real name (one path, not split at the space).
func TestCherryPickConflictKeepsSpacedNames(t *testing.T) {
	r := newGitTestRepo(t)
	for _, name := range []string{"my notes.txt", "日本.txt"} {
		r.write(t, name, "base\n")
	}
	r.git(t, "add", "-A")
	r.git(t, "commit", "-qm", "base")
	r.git(t, "checkout", "-q", "-b", "topic")
	r.write(t, "my notes.txt", "topic\n")
	r.write(t, "日本.txt", "topic\n")
	r.git(t, "commit", "-qam", "topic")
	r.git(t, "checkout", "-q", "main")
	r.write(t, "my notes.txt", "main\n")
	r.write(t, "日本.txt", "main\n")
	r.git(t, "commit", "-qam", "main")

	paths, err := CherryPick(r.dir, "topic")
	if err == nil || !strings.Contains(err.Error(), "2 conflict(s)") {
		t.Fatalf("cherry-pick err = %v, want 2 conflicts", err)
	}
	if strings.Join(paths, "|") != "my notes.txt|日本.txt" {
		t.Errorf("conflicts = %q, want the two real names", paths)
	}
}
