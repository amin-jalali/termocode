package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reword the very first commit: the todo starts at the root.
func TestRunRebaseFromRoot(t *testing.T) {
	r, _ := rebaseRepo(t, 2)
	todo, err := LoadRebaseTodo(r.dir, "")
	if err != nil {
		t.Fatal(err)
	}
	todo.SetAction(0, RebaseReword)
	todo.Items[0].Message = "first"
	if out, err := RunRebase(r.dir, todo, testRebaseOpts(t)); err != nil || out.Stopped {
		t.Fatalf("root rebase: %+v %v", out, err)
	}
	if got := strings.Join(subjects(t, r), " "); got != "first c2" {
		t.Errorf("history %q", got)
	}
}

// With no Exe set, termocode runs itself as git's editor (here: the test
// binary, whose TestMain answers as the helper).
func TestRunRebaseUsesOwnBinary(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	todo, err := LoadRebaseTodo(r.dir, h[0])
	if err != nil {
		t.Fatal(err)
	}
	todo.Move(0, 1)
	if out, err := RunRebase(r.dir, todo, RebaseOptions{}); err != nil || out.Stopped {
		t.Fatalf("rebase: %+v %v", out, err)
	}
	if got := strings.Join(subjects(t, r), " "); got != "c1 c3 c2" {
		t.Errorf("history %q", got)
	}
}

// The termocode binary is gone (moved by an update): git cannot start the
// editor, the rebase fails with a reason and the branch is unchanged.
func TestRunRebaseHelperMissing(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	todo, err := LoadRebaseTodo(r.dir, h[0])
	if err != nil {
		t.Fatal(err)
	}
	todo.Move(0, 1)
	_, err = RunRebase(r.dir, todo, RebaseOptions{Exe: filepath.Join(t.TempDir(), "gone")})
	if err == nil {
		t.Fatal("rebase with a missing helper: want error")
	}
	if RebaseStatus(r.dir).InProgress {
		t.Error("a failed start left a rebase in progress")
	}
	if head := strings.TrimSpace(r.git(t, "rev-parse", "HEAD")); head != h[2] {
		t.Errorf("HEAD moved to %s", head)
	}
}

// No temp space: the rebase is refused before git runs.
func TestRunRebaseNoTempDir(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	todo, err := LoadRebaseTodo(r.dir, h[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	if _, err := RunRebase(r.dir, todo, testRebaseOpts(t)); err == nil {
		t.Error("RunRebase without a temp dir: want error")
	}
	if head := strings.TrimSpace(r.git(t, "rev-parse", "HEAD")); head != h[2] {
		t.Errorf("HEAD moved to %s", head)
	}
}

func TestRunRebaseOutsideRepo(t *testing.T) {
	todo := RebaseTodo{Base: "abc", Items: []RebaseItem{{Action: RebasePick, Hash: "abc1234", FullHash: "abc1234"}}}
	if _, err := RunRebase(t.TempDir(), todo, RebaseOptions{Autostash: true}); err == nil {
		t.Error("RunRebase outside a repo: want error")
	}
}

func TestFixupIntoRefusals(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	opt := testRebaseOpts(t)
	r.git(t, "checkout", "-q", "-b", "side", h[0])
	r.write(t, "s.txt", "s\n")
	r.git(t, "add", "s.txt")
	r.git(t, "commit", "-qm", "side")
	r.git(t, "checkout", "-q", "main")
	r.write(t, "f1.txt", "fix\n")
	r.git(t, "add", "f1.txt")
	if _, err := FixupInto(r.dir, "side", opt); err == nil || !strings.Contains(err.Error(), "not on the current branch") {
		t.Errorf("fixup into another branch = %v", err)
	}

	// index.lock: the fixup commit fails and nothing moves.
	lock := filepath.Join(r.dir, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := FixupInto(r.dir, h[1], opt); err == nil || !strings.Contains(err.Error(), "index.lock") {
		t.Errorf("fixup with index.lock = %v", err)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if head := strings.TrimSpace(r.git(t, "rev-parse", "HEAD")); head != h[2] {
		t.Errorf("HEAD moved to %s", head)
	}
}

// Fixup into the root commit (it has no parent) rebases from the root.
func TestFixupIntoRootCommit(t *testing.T) {
	r, h := rebaseRepo(t, 2)
	r.write(t, "f1.txt", "fixed 1\n")
	r.git(t, "add", "f1.txt")
	if out, err := FixupInto(r.dir, h[0], testRebaseOpts(t)); err != nil || out.Stopped {
		t.Fatalf("fixup into root: %+v %v", out, err)
	}
	if got := strings.Join(subjects(t, r), " "); got != "c1 c2" {
		t.Errorf("history %q", got)
	}
	if got := r.git(t, "show", "HEAD~1:f1.txt"); got != "fixed 1\n" {
		t.Errorf("root f1.txt = %q", got)
	}
}

// A merge commit after the target: the fixup is refused before committing.
func TestFixupIntoRefusesMergeAfterTarget(t *testing.T) {
	r, h := rebaseRepo(t, 2)
	r.git(t, "checkout", "-q", "-b", "side", h[0])
	r.write(t, "s.txt", "s\n")
	r.git(t, "add", "s.txt")
	r.git(t, "commit", "-qm", "side")
	r.git(t, "checkout", "-q", "main")
	r.git(t, "merge", "-q", "--no-ff", "-m", "merge side", "side")
	head := strings.TrimSpace(r.git(t, "rev-parse", "HEAD"))
	r.write(t, "f2.txt", "fix\n")
	r.git(t, "add", "f2.txt")
	if _, err := FixupInto(r.dir, h[1], testRebaseOpts(t)); err == nil || !strings.Contains(err.Error(), "merge") {
		t.Errorf("fixup across a merge = %v", err)
	}
	if got := strings.TrimSpace(r.git(t, "rev-parse", "HEAD")); got != head {
		t.Error("fixup committed before refusing")
	}
}

// Skip / Abort with no rebase running fail with git's reason.
func TestRebaseCommandsWithoutRebase(t *testing.T) {
	r, _ := rebaseRepo(t, 1)
	if err := RebaseAbort(r.dir); err == nil {
		t.Error("abort with no rebase: want error")
	}
	if _, err := RebaseSkip(r.dir, testRebaseOpts(t)); err == nil {
		t.Error("skip with no rebase: want error")
	}
	if st := RebaseStatus(t.TempDir()); st.InProgress {
		t.Error("RebaseStatus outside a repo says in progress")
	}
}

// A rebase started outside termocode (plain git, on a detached HEAD):
// status shows no branch, and Continue works with git's own messages.
func TestRebaseStartedOutsideTermocode(t *testing.T) {
	r, h := conflictRepo(t)
	r.git(t, "checkout", "-q", "--detach")
	swap := `f() { awk 'NR==1{a=$0;next} NR==2{print;print a;next} {print}' "$1" >"$1.t" && mv "$1.t" "$1"; }; f`
	cmd := []string{"-c", "sequence.editor=" + swap, "rebase", "-i", h[0]}
	if out, err := runOut(r.dir, "git", cmd...); err == nil {
		t.Fatalf("the swap should stop on a conflict: %s", out)
	}
	st := RebaseStatus(r.dir)
	if !st.InProgress || st.Branch != "" {
		t.Fatalf("status = %+v, want in progress with no branch", st)
	}
	r.write(t, "f.txt", "b\n")
	out, err := RebaseContinue(r.dir, testRebaseOpts(t))
	if err != nil || !out.Stopped || strings.Join(out.Conflicts, ",") != "f.txt" {
		t.Fatalf("continue after c3 = %+v %v, want a stop on c2's conflict", out, err)
	}
	r.write(t, "f.txt", "a\n")
	if out, err = RebaseContinue(r.dir, testRebaseOpts(t)); err != nil || out.Stopped {
		t.Fatalf("last continue = %+v %v", out, err)
	}
	if got := strings.Join(subjects(t, r), " "); got != "c1 c3 c2" {
		t.Errorf("history %q", got)
	}
}

func TestRebaseHelperBadInput(t *testing.T) {
	dir := t.TempDir()
	plan := filepath.Join(dir, "plan.json")
	if code := runRebaseHelper([]string{"seq", filepath.Join(dir, "none.json"), filepath.Join(dir, "todo")}); code != 1 {
		t.Errorf("missing plan: exit %d, want 1", code)
	}
	if err := os.WriteFile(plan, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runRebaseHelper([]string{"seq", plan, filepath.Join(dir, "todo")}); code != 1 {
		t.Errorf("bad plan: exit %d, want 1", code)
	}
	if err := os.WriteFile(plan, []byte(`{"Todo":"pick a x\n"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runRebaseHelper([]string{"what", plan, filepath.Join(dir, "todo")}); code != 2 {
		t.Errorf("unknown mode: exit %d, want 2", code)
	}
	if code := runRebaseHelper([]string{"seq", plan, filepath.Join(dir, "no", "such", "todo")}); code != 1 {
		t.Errorf("todo in a missing folder: exit %d, want 1", code)
	}
}
