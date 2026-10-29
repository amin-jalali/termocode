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

// stopOnConflict starts conflictRepo's swap (c3 first) and checks it
// stopped; reword sets a message on c2.
func stopOnConflict(t *testing.T, reword string) (*gitTestRepo, []string) {
	t.Helper()
	r, h := conflictRepo(t)
	todo, err := LoadRebaseTodo(r.dir, h[0])
	if err != nil {
		t.Fatal(err)
	}
	if reword != "" {
		todo.SetAction(0, RebaseReword)
		todo.Items[0].Message = reword
	}
	todo.Move(0, 1)
	if out, err := RunRebase(r.dir, todo, testRebaseOpts(t)); err != nil || !out.Stopped {
		t.Fatalf("outcome %+v %v", out, err)
	}
	return r, h
}

// G-RBS: Abort puts the branch back where it was.
func TestRebaseAbort(t *testing.T) {
	r, h := stopOnConflict(t, "")
	if err := RebaseAbort(r.dir); err != nil {
		t.Fatal(err)
	}
	if RebaseStatus(r.dir).InProgress {
		t.Fatal("still rebasing")
	}
	if head := strings.TrimSpace(r.git(t, "rev-parse", "HEAD")); head != h[2] {
		t.Errorf("HEAD %s, want %s", head, h[2])
	}
}

// G-RBS: Continue refuses while a conflicted file still has markers; once
// fixed it stages the file itself and goes on (stopping again on the next
// conflict). Skip drops the commit it stopped on.
func TestRebaseContinueAndSkip(t *testing.T) {
	r, _ := stopOnConflict(t, "")
	opt := testRebaseOpts(t)
	if _, err := RebaseContinue(r.dir, opt); err == nil || !strings.Contains(err.Error(), "f.txt") {
		t.Fatalf("continue with markers: %v", err)
	}
	r.write(t, "f.txt", "b\n")
	out, err := RebaseContinue(r.dir, opt)
	if err != nil || !out.Stopped {
		t.Fatalf("continue: %+v %v", out, err)
	}
	if st := RebaseStatus(r.dir); st.Step != 2 || st.Subject != "c2" {
		t.Fatalf("second stop %+v", st)
	}
	out, err = RebaseSkip(r.dir, opt)
	if err != nil || out.Stopped {
		t.Fatalf("skip: %+v %v", out, err)
	}
	if got := strings.Join(subjects(t, r), " "); got != "c1 c3" {
		t.Errorf("history %q", got)
	}
}

// G-RBS: a reword row that stopped on a conflict still gets its new
// message after Continue (the plan lives in .git/rebase-merge).
func TestRebaseContinueKeepsReword(t *testing.T) {
	r, _ := stopOnConflict(t, "c2 reworded")
	opt := testRebaseOpts(t)
	r.write(t, "f.txt", "b\n")
	if out, err := RebaseContinue(r.dir, opt); err != nil || !out.Stopped {
		t.Fatalf("continue: %+v %v", out, err)
	}
	r.write(t, "f.txt", "a\n")
	if out, err := RebaseContinue(r.dir, opt); err != nil || out.Stopped {
		t.Fatalf("continue 2: %+v %v", out, err)
	}
	if got := strings.TrimSpace(r.git(t, "log", "-1", "--format=%B")); got != "c2 reworded" {
		t.Errorf("message %q", got)
	}
}
