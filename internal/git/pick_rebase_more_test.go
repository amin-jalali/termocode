package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (r *gitTestRepo) commitAll(t *testing.T, msg string) string {
	t.Helper()
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", msg)
	return strings.TrimSpace(r.git(t, "rev-parse", "HEAD"))
}

func (r *gitTestRepo) readOr(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.dir, name))
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

// sideRepo: main has c1; branch side adds s1, s2 (and a merge-free
// history); main then gets c2.
func sideRepo(t *testing.T) *gitTestRepo {
	t.Helper()
	r, _ := rebaseRepo(t, 1)
	r.git(t, "checkout", "-q", "-b", "side")
	r.write(t, "s1.txt", "s1\n")
	r.commitAll(t, "s1")
	r.write(t, "s2.txt", "s2\n")
	r.commitAll(t, "s2")
	r.git(t, "checkout", "-q", "main")
	r.write(t, "m.txt", "m\n")
	r.commitAll(t, "c2")
	return r
}

// GITX-16 (K2): BranchOnlyCommits lists, oldest first, the commits of a
// branch that HEAD lacks (a commit already picked is left out);
// CherryPickMany applies them in that order.
func TestCherryPickMany(t *testing.T) {
	r := sideRepo(t)
	es, err := BranchOnlyCommits(r.dir, "side", 50)
	if err != nil || logSubjectsOf(es) != "s1|s2" {
		t.Fatalf("branch commits %q %v", logSubjectsOf(es), err)
	}
	if _, err := CherryPickMany(r.dir, []string{es[0].Hash}); err != nil {
		t.Fatal(err)
	}
	if es, _ := BranchOnlyCommits(r.dir, "side", 50); logSubjectsOf(es) != "s2" {
		t.Errorf("after one pick %q", logSubjectsOf(es))
	}
	es, _ = BranchOnlyCommits(r.dir, "side", 50)
	if _, err := CherryPickMany(r.dir, []string{es[0].Hash}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(subjects(t, r), " "); got != "c1 c2 s1 s2" {
		t.Errorf("history %s", got)
	}
	if _, err := CherryPickMany(r.dir, nil); err == nil {
		t.Error("nothing picked without an error")
	}
	if _, err := BranchOnlyCommits(r.dir, "--all", 5); err == nil {
		t.Error("an option as branch")
	}
}

// GITX-16 (K5): a merge is reverted against the parent the user keeps;
// MergeParents names both.
func TestRevertMergeParent(t *testing.T) {
	r := sideRepo(t)
	r.git(t, "merge", "-q", "--no-ff", "-m", "merge side", "side")
	merge := strings.TrimSpace(r.git(t, "rev-parse", "HEAD"))
	ps, err := MergeParents(r.dir, merge)
	if err != nil || logSubjectsOf(ps) != "c2|s2" {
		t.Fatalf("parents %q %v", logSubjectsOf(ps), err)
	}
	if _, err := RevertMerge(r.dir, merge, 1); err != nil {
		t.Fatal(err)
	}
	if r.readOr(t, "s1.txt") != "<missing>" || r.readOr(t, "m.txt") != "m\n" {
		t.Error("revert -m 1 did not drop the side's files")
	}
	if _, err := RevertMerge(r.dir, merge, 3); err == nil {
		t.Error("parent 3 accepted")
	}
	if _, err := RevertMerge(r.dir, "HEAD", 1); err == nil {
		t.Error("a non-merge reverted as a merge")
	}
}

// GITX-16 (K3): ApplyFileFromCommit puts one file's changes of a commit
// into the working tree (and the index); nothing is committed.
func TestApplyFileFromCommit(t *testing.T) {
	r := sideRepo(t)
	r.git(t, "checkout", "-q", "side")
	r.write(t, "f1.txt", "file 1\nfrom side\n")
	r.write(t, "s3.txt", "s3\n")
	both := r.commitAll(t, "two files")
	r.git(t, "checkout", "-q", "main")
	head := strings.TrimSpace(r.git(t, "rev-parse", "HEAD"))
	if err := ApplyFileFromCommit(r.dir, both, "f1.txt"); err != nil {
		t.Fatal(err)
	}
	if r.readOr(t, "f1.txt") != "file 1\nfrom side\n" || r.readOr(t, "s3.txt") != "<missing>" {
		t.Errorf("f1 %q s3 %q", r.readOr(t, "f1.txt"), r.readOr(t, "s3.txt"))
	}
	if strings.TrimSpace(r.git(t, "rev-parse", "HEAD")) != head {
		t.Error("a commit was made")
	}
	if err := ApplyFileFromCommit(r.dir, both, "nope.txt"); err == nil {
		t.Error("a file the commit does not change")
	}
}

// GITX-16 (K12): RestoreFileFrom puts a file back as it was in a commit;
// uncommitted changes are saved in the stash list first.
func TestRestoreFileFrom(t *testing.T) {
	r, h := rebaseRepo(t, 1)
	r.write(t, "f1.txt", "v2\n")
	r.commitAll(t, "v2")
	r.write(t, "f1.txt", "dirty\n")
	backup, err := RestoreFileFrom(r.dir, h[0], "f1.txt")
	if err != nil {
		t.Fatal(err)
	}
	if r.readOr(t, "f1.txt") != "file 1\n" {
		t.Errorf("f1 %q", r.readOr(t, "f1.txt"))
	}
	if backup == "" || !strings.Contains(r.git(t, "stash", "list"), "before restoring f1.txt") {
		t.Errorf("no safety copy: %q\n%s", backup, r.git(t, "stash", "list"))
	}
	if got := r.git(t, "show", backup+":f1.txt"); got != "dirty\n" {
		t.Errorf("safety copy holds %q", got)
	}
	// clean file: no safety copy needed
	r.git(t, "checkout", "-q", "HEAD", "--", "f1.txt")
	if b, err := RestoreFileFrom(r.dir, h[0], "f1.txt"); err != nil || b != "" {
		t.Errorf("clean restore %q %v", b, err)
	}
	if _, err := RestoreFileFrom(r.dir, h[0], "nope.txt"); err == nil {
		t.Error("a file not in the commit")
	}
}

// GITX-17 (F9, J11, J10): RebaseOnto replays the branch onto another;
// with keepMerges a merge on the branch stays a merge.
func TestRebaseOnto(t *testing.T) {
	r := sideRepo(t)
	r.git(t, "checkout", "-q", "side")
	if es, err := CommitsNotOn(r.dir, "main", 10); err != nil || logSubjectsOf(es) != "s1|s2" {
		t.Errorf("commits not on main %q %v", logSubjectsOf(es), err)
	}
	if _, err := CommitsNotOn(r.dir, "--all", 10); err == nil {
		t.Error("an option as branch")
	}
	if _, err := RebaseOnto(r.dir, "main", false, RebaseOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(subjects(t, r), " "); got != "c1 c2 s1 s2" {
		t.Errorf("history %s", got)
	}
}

// The branch with a merge, rebased with keepMerges, still has it.
func TestRebaseOntoKeepMerges(t *testing.T) {
	r := sideRepo(t)
	r.git(t, "checkout", "-q", "-b", "topic", "side~2")
	r.write(t, "t.txt", "t\n")
	r.commitAll(t, "t1")
	r.git(t, "merge", "-q", "--no-ff", "-m", "merge side into topic", "side")
	if _, err := RebaseOnto(r.dir, "main", true, RebaseOptions{}); err != nil {
		t.Fatal(err)
	}
	if n := strings.TrimSpace(r.git(t, "rev-list", "--merges", "--count", "main..HEAD")); n != "1" {
		t.Errorf("%s merges after a rebase that keeps them", n)
	}
	if err := spawnQuiet(r.dir, "merge-base", "--is-ancestor", "main", "HEAD"); err != nil {
		t.Error("not on main")
	}
	r.write(t, "t.txt", "dirty\n")
	if _, err := RebaseOnto(r.dir, "side", false, RebaseOptions{}); err != ErrRebaseDirty {
		t.Errorf("dirty tree: %v", err)
	}
	if _, err := RebaseOnto(r.dir, "--root", false, RebaseOptions{Autostash: true}); err == nil {
		t.Error("an option as branch")
	}
}

// GITX-17 (J7, J9, J8): one-step reword (the full message), drop and
// squash-into-previous of one commit.
func TestRewordDropSquashCommit(t *testing.T) {
	r, h := rebaseRepo(t, 4)
	if _, err := RewordCommit(r.dir, h[1], "c2 new\n\nnew body\nline two", RebaseOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(r.git(t, "log", "-1", "--format=%B", "HEAD~2")); got != "c2 new\n\nnew body\nline two" {
		t.Errorf("message %q", got)
	}
	h2 := strings.TrimSpace(r.git(t, "rev-parse", "HEAD~1"))
	if _, err := DropCommit(r.dir, h2, RebaseOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(subjects(t, r), " "); got != "c1 c2 new c4" {
		t.Errorf("after drop %s", got)
	}
	if _, err := SquashIntoPrevious(r.dir, strings.TrimSpace(r.git(t, "rev-parse", "HEAD")), RebaseOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(subjects(t, r), " "); got != "c1 c2 new" {
		t.Errorf("after squash %s", got)
	}
	if r.readOr(t, "f4.txt") != "file 4\n" {
		t.Error("the squashed change is gone")
	}
	root := strings.TrimSpace(r.git(t, "rev-list", "--max-parents=0", "HEAD"))
	if _, err := SquashIntoPrevious(r.dir, root, RebaseOptions{}); err == nil {
		t.Error("the root squashed into nothing")
	}
}

// GITX-17 (J16): Autosquash folds existing fixup! commits into their
// targets.
func TestAutosquash(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	if n, _, err := AutosquashPlan(r.dir); err != nil || n != 0 {
		t.Fatalf("plan with no fixups %d %v", n, err)
	}
	r.write(t, "f1.txt", "file 1 fixed\n")
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "--fixup", h[0])
	n, base, err := AutosquashPlan(r.dir)
	if err != nil || n != 1 || base != "" {
		t.Fatalf("plan %d %q %v", n, base, err)
	}
	if _, err := Autosquash(r.dir, RebaseOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(subjects(t, r), " "); got != "c1 c2 c3" {
		t.Errorf("history %s", got)
	}
	if got := r.git(t, "show", "HEAD~2:f1.txt"); got != "file 1 fixed\n" {
		t.Errorf("c1 has %q", got)
	}
}

// GITX-25: a commit as a patch, the changes as a patch, and a checked
// apply.
func TestPatches(t *testing.T) {
	r, h := rebaseRepo(t, 2)
	p, err := CommitPatch(r.dir, h[1])
	if err != nil || !strings.Contains(p, "Subject: [PATCH] c2") || !strings.Contains(p, "+file 2") {
		t.Fatalf("commit patch %v\n%s", err, p)
	}
	r.write(t, "f1.txt", "changed\n")
	ch, err := ChangesPatch(r.dir)
	if err != nil || !strings.Contains(ch, "+changed") {
		t.Fatalf("changes patch %v\n%s", err, ch)
	}
	r.git(t, "checkout", "-q", "--", "f1.txt")
	stat, err := CheckPatch(r.dir, ch)
	if err != nil || !strings.Contains(stat, "f1.txt") {
		t.Fatalf("check %q %v", stat, err)
	}
	if err := ApplyPatch(r.dir, ch); err != nil {
		t.Fatal(err)
	}
	if r.readOr(t, "f1.txt") != "changed\n" {
		t.Error("not applied")
	}
	if _, err := CheckPatch(r.dir, ch); err == nil {
		t.Error("a patch that is already in is not refused")
	}
	if _, err := CheckPatch(r.dir, "not a patch"); err == nil {
		t.Error("text that is no patch")
	}
}

func logSubjectsOf(es []LogEntry) string {
	var s []string
	for _, e := range es {
		s = append(s, e.Subject)
	}
	return strings.Join(s, "|")
}
