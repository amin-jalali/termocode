package git

import (
	"strings"
	"testing"
)

// conflictRepo: c1 adds f.txt "x", c2 makes it "a", c3 makes it "b".
// Swapping c2 and c3 conflicts on c3. Hashes oldest first.
func conflictRepo(t *testing.T) (*gitTestRepo, []string) {
	t.Helper()
	r := newGitTestRepo(t)
	var h []string
	for i, body := range []string{"x\n", "a\n", "b\n"} {
		r.write(t, "f.txt", body)
		r.git(t, "add", "-A")
		r.git(t, "commit", "-q", "-m", "c"+string(rune('1'+i)))
		h = append(h, strings.TrimSpace(r.git(t, "rev-parse", "HEAD")))
	}
	return r, h
}

// G-RBS: when a rebase stops on a conflict, RebaseStatus says so: the
// step, the commit it stopped on, the branch and the conflicted paths.
// Without a rebase it says nothing is running.
func TestRebaseStatusOnConflict(t *testing.T) {
	r, h := conflictRepo(t)
	if st := RebaseStatus(r.dir); st.InProgress {
		t.Fatalf("idle repo: %+v", st)
	}
	todo, err := LoadRebaseTodo(r.dir, h[0])
	if err != nil {
		t.Fatal(err)
	}
	todo.Move(0, 1) // c3 then c2
	out, err := RunRebase(r.dir, todo, testRebaseOpts(t))
	if err != nil || !out.Stopped || len(out.Conflicts) != 1 || out.Conflicts[0] != "f.txt" {
		t.Fatalf("outcome %+v %v", out, err)
	}
	st := RebaseStatus(r.dir)
	if !st.InProgress || st.Step != 1 || st.Total != 2 || st.Branch != "main" {
		t.Fatalf("status %+v", st)
	}
	if !strings.HasPrefix(h[2], st.Hash) || st.Subject != "c3" || len(st.Conflicts) != 1 {
		t.Errorf("stopped on %+v", st)
	}
}
