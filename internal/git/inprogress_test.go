package git

import (
	"os/exec"
	"testing"
)

// twoSidesRepo has main and side both changing f.txt, on main.
func twoSidesRepo(t *testing.T) *gitTestRepo {
	t.Helper()
	r := newGitTestRepo(t)
	r.write(t, "f.txt", "base\n")
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "base")
	r.git(t, "checkout", "-q", "-b", "side")
	r.write(t, "f.txt", "side\n")
	r.git(t, "commit", "-q", "-am", "side")
	r.git(t, "checkout", "-q", "main")
	r.write(t, "f.txt", "main\n")
	r.git(t, "commit", "-q", "-am", "main")
	return r
}

// gitFails runs a git command that is expected to stop on a conflict.
func gitFails(t *testing.T, r *gitTestRepo, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	if err := cmd.Run(); err == nil {
		t.Fatalf("git %v did not stop", args)
	}
}

// G-UND: a merge, cherry-pick, revert or rebase stopped half-way is named;
// a clean repo has none.
func TestOperationInProgress(t *testing.T) {
	r := twoSidesRepo(t)
	if op := OperationInProgress(r.dir); op != "" {
		t.Fatalf("clean repo: %q", op)
	}
	for _, c := range []struct {
		op    string
		args  []string
		abort []string
	}{
		{"merge", []string{"merge", "side"}, []string{"merge", "--abort"}},
		{"cherry-pick", []string{"cherry-pick", "side"}, []string{"cherry-pick", "--abort"}},
		{"rebase", []string{"rebase", "side"}, []string{"rebase", "--abort"}},
		{"revert", []string{"revert", "--no-edit", "HEAD"}, nil},
	} {
		if c.op == "revert" {
			// main's change reverted on top of side's: conflict.
			r.git(t, "merge", "-q", "-s", "ours", "-m", "keep main", "side")
			r.write(t, "f.txt", "later\n")
			r.git(t, "commit", "-q", "-am", "later")
			c.args = []string{"revert", "--no-edit", "HEAD~2"}
			c.abort = []string{"revert", "--abort"}
		}
		gitFails(t, r, c.args...)
		if op := OperationInProgress(r.dir); op != c.op {
			t.Errorf("after %v: %q, want %q", c.args, op, c.op)
		}
		r.git(t, c.abort...)
		if op := OperationInProgress(r.dir); op != "" {
			t.Errorf("after %v: %q left", c.abort, op)
		}
	}
}
