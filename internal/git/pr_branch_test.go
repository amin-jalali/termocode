package git

import (
	"os/exec"
	"reflect"
	"testing"
)

func (r *gitTestRepo) commitFile(t *testing.T, name, subject string) {
	t.Helper()
	r.write(t, name, subject+"\n")
	r.git(t, "add", name)
	r.git(t, "commit", "-q", "-m", subject)
}

// BranchSubjects lists the branch's own commits oldest first, measured
// from origin/<base> when it exists.
func TestBranchSubjects(t *testing.T) {
	r := newGitTestRepo(t)
	r.commitFile(t, "a.txt", "base one")
	r.git(t, "update-ref", "refs/remotes/origin/main", "HEAD")
	r.git(t, "checkout", "-q", "-b", "feat/x")
	r.commitFile(t, "b.txt", "feat: first")
	r.commitFile(t, "c.txt", "fix: second")
	got, err := BranchSubjects(r.dir, "main")
	if err != nil || !reflect.DeepEqual(got, []string{"feat: first", "fix: second"}) {
		t.Errorf("subjects %v err %v", got, err)
	}
	if got, _ := BranchSubjects(r.dir, "-evil"); got != nil {
		t.Errorf("option-like base gave %v", got)
	}
}

func TestUnpushedAndDirty(t *testing.T) {
	r := newGitTestRepo(t)
	r.commitFile(t, "a.txt", "one")
	if _, ok := Unpushed(r.dir); ok {
		t.Error("a branch with no upstream reported as pushed")
	}
	remote := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	r.git(t, "remote", "add", "origin", remote)
	r.git(t, "push", "-q", "-u", "origin", "main")
	if n, ok := Unpushed(r.dir); !ok || n != 0 {
		t.Errorf("pushed: n=%d ok=%v", n, ok)
	}
	r.commitFile(t, "b.txt", "two")
	if n, ok := Unpushed(r.dir); !ok || n != 1 {
		t.Errorf("one ahead: n=%d ok=%v", n, ok)
	}

	if d, err := Dirty(r.dir); err != nil || d {
		t.Errorf("clean tree: dirty=%v err=%v", d, err)
	}
	r.write(t, "untracked.txt", "x")
	if d, _ := Dirty(r.dir); d {
		t.Error("an untracked file counts as dirty")
	}
	r.write(t, "a.txt", "changed\n")
	if d, _ := Dirty(r.dir); !d {
		t.Error("a changed tracked file is not dirty")
	}
}
