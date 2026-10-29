package git

import (
	"errors"
	"os/exec"
	"testing"
)

// try runs git and returns its error (a conflict is an error here).
func (r *gitTestRepo) try(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	return cmd.Run()
}

// forkRepo commits base on main, then ours on main and theirs on a
// "feature" branch, and leaves main checked out.
func forkRepo(t *testing.T, base, ours, theirs string) *gitTestRepo {
	t.Helper()
	r := newGitTestRepo(t)
	r.write(t, "f.txt", base)
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "base")
	r.git(t, "checkout", "-q", "-b", "feature")
	r.write(t, "f.txt", theirs)
	r.git(t, "commit", "-q", "-am", "theirs")
	r.git(t, "checkout", "-q", "main")
	r.write(t, "f.txt", ours)
	r.git(t, "commit", "-q", "-am", "ours")
	return r
}

func wantStages(t *testing.T, r *gitTestRepo, base, ours, theirs string) {
	t.Helper()
	st, err := ReadConflictStages(r.dir, "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !st.TextMerge() || st.DeletedBy() != "" {
		t.Fatalf("not a text merge: %+v", st)
	}
	if st.Base.Text != base || st.Ours.Text != ours || st.Theirs.Text != theirs {
		t.Errorf("stages = %q | %q | %q, want %q | %q | %q", st.Base.Text, st.Ours.Text, st.Theirs.Text, base, ours, theirs)
	}
}

func TestConflictStagesMerge(t *testing.T) {
	r := forkRepo(t, "a\nx\nb\n", "a\nours\nb\n", "a\ntheirs\nb\n")
	if r.try("merge", "--no-edit", "feature") == nil {
		t.Fatal("merge did not conflict")
	}
	wantStages(t, r, "a\nx\nb\n", "a\nours\nb\n", "a\ntheirs\nb\n")
}

func TestConflictStagesRebase(t *testing.T) {
	r := forkRepo(t, "a\nx\nb\n", "a\nours\nb\n", "a\ntheirs\nb\n")
	r.git(t, "checkout", "-q", "feature")
	if r.try("rebase", "main") == nil {
		t.Fatal("rebase did not conflict")
	}
	// A rebase replays feature onto main: ours is main's text.
	wantStages(t, r, "a\nx\nb\n", "a\nours\nb\n", "a\ntheirs\nb\n")
}

func TestConflictStagesCherryPick(t *testing.T) {
	r := forkRepo(t, "a\nx\nb\n", "a\nours\nb\n", "a\ntheirs\nb\n")
	if r.try("cherry-pick", "feature") == nil {
		t.Fatal("cherry-pick did not conflict")
	}
	wantStages(t, r, "a\nx\nb\n", "a\nours\nb\n", "a\ntheirs\nb\n")
}

func TestConflictStagesStashPop(t *testing.T) {
	r := forkRepo(t, "a\nx\nb\n", "a\nours\nb\n", "a\ntheirs\nb\n")
	r.write(t, "f.txt", "a\nstashed\nb\n")
	r.git(t, "stash", "-q")
	r.write(t, "f.txt", "a\nnew\nb\n")
	r.git(t, "commit", "-q", "-am", "new")
	if r.try("stash", "pop") == nil {
		t.Fatal("stash pop did not conflict")
	}
	// The stash's base is the commit it was made on ("ours" then).
	wantStages(t, r, "a\nours\nb\n", "a\nnew\nb\n", "a\nstashed\nb\n")
}

func TestConflictStagesDeletedAndAddAdd(t *testing.T) {
	r := forkRepo(t, "a\n", "a\nours\n", "a\ntheirs\n")
	r.git(t, "checkout", "-q", "feature")
	r.git(t, "rm", "-q", "f.txt")
	r.git(t, "commit", "-q", "-m", "delete")
	r.git(t, "checkout", "-q", "main")
	if r.try("merge", "--no-edit", "feature") == nil {
		t.Fatal("merge did not conflict")
	}
	st, err := ReadConflictStages(r.dir, "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if st.DeletedBy() != "theirs" || st.TextMerge() || st.Ours.Text != "a\nours\n" {
		t.Errorf("modify/delete = %+v", st)
	}

	r2 := newGitTestRepo(t)
	r2.write(t, "seed", "s\n")
	r2.git(t, "add", ".")
	r2.git(t, "commit", "-q", "-m", "seed")
	r2.git(t, "checkout", "-q", "-b", "feature")
	r2.write(t, "f.txt", "theirs\n")
	r2.git(t, "add", ".")
	r2.git(t, "commit", "-q", "-m", "add theirs")
	r2.git(t, "checkout", "-q", "main")
	r2.write(t, "f.txt", "ours\n")
	r2.git(t, "add", ".")
	r2.git(t, "commit", "-q", "-m", "add ours")
	if r2.try("merge", "--no-edit", "feature") == nil {
		t.Fatal("add/add did not conflict")
	}
	st, err = ReadConflictStages(r2.dir, "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if st.Base.Present || !st.TextMerge() || st.Ours.Text != "ours\n" || st.Theirs.Text != "theirs\n" {
		t.Errorf("add/add = %+v", st)
	}
}

func TestConflictStagesBinaryAndClean(t *testing.T) {
	r := forkRepo(t, "a\x00b", "a\x00ours", "a\x00theirs")
	if r.try("merge", "--no-edit", "feature") == nil {
		t.Fatal("merge did not conflict")
	}
	st, err := ReadConflictStages(r.dir, "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Binary() || st.TextMerge() || st.Ours.Text != "" {
		t.Errorf("binary = %+v", st)
	}
	r.write(t, "clean.txt", "c\n")
	if _, err := ReadConflictStages(r.dir, "clean.txt"); !errors.Is(err, ErrNotConflicted) {
		t.Errorf("clean file err = %v", err)
	}
}
