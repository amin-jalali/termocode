package git

import (
	"strings"
	"testing"
)

// stashRepo has a.txt and b.txt committed, then a.txt changed (staged),
// b.txt changed (not staged) and new.txt untracked.
func stashRepo(t *testing.T) *gitTestRepo {
	t.Helper()
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "a\n")
	r.write(t, "b.txt", "b\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "first")
	r.write(t, "a.txt", "a2\n")
	r.git(t, "add", "a.txt")
	r.write(t, "b.txt", "b2\n")
	r.write(t, "new.txt", "n\n")
	return r
}

func stashPorcelain(t *testing.T, r *gitTestRepo) string {
	t.Helper()
	return strings.TrimRight(r.git(t, "status", "--porcelain"), "\n")
}

// GITX-10: stash with a message and the untracked files.
func TestStashPushUntracked(t *testing.T) {
	r := stashRepo(t)
	if err := StashPush(r.dir, StashOptions{Message: "all of it", Untracked: true}); err != nil {
		t.Fatal(err)
	}
	if got := stashPorcelain(t, r); got != "" {
		t.Errorf("left %q", got)
	}
	if got := strings.TrimSpace(r.git(t, "stash", "list", "--format=%s")); !strings.HasSuffix(got, "all of it") {
		t.Errorf("stash %q", got)
	}
	d, err := StashDiff(r.dir, "stash@{0}")
	if err != nil || !strings.Contains(d, "+a2") || !strings.Contains(d, "+b2") || !strings.Contains(d, "new.txt") {
		t.Errorf("diff %q %v", d, err)
	}
}

// GITX-10: stash only the staged changes, or only some files.
func TestStashPushStagedAndPaths(t *testing.T) {
	r := stashRepo(t)
	if err := StashPush(r.dir, StashOptions{StagedOnly: true}); err != nil {
		t.Fatal(err)
	}
	if got := stashPorcelain(t, r); got != " M b.txt\n?? new.txt" {
		t.Errorf("after staged-only %q", got)
	}
	if err := StashPush(r.dir, StashOptions{Paths: []string{"b.txt"}}); err != nil {
		t.Fatal(err)
	}
	if got := stashPorcelain(t, r); got != "?? new.txt" {
		t.Errorf("after one file %q", got)
	}
	if err := StashPush(r.dir, StashOptions{}); err == nil {
		t.Error("no error with nothing to stash")
	}
}

// GITX-10: a dropped stash comes back from its hash; a branch can start
// from a stash.
func TestStashDropRestoreAndBranch(t *testing.T) {
	r := stashRepo(t)
	if err := StashPush(r.dir, StashOptions{Message: "keep me"}); err != nil {
		t.Fatal(err)
	}
	hash := StashHash(r.dir, "stash@{0}")
	if len(hash) != 40 {
		t.Fatalf("hash %q", hash)
	}
	if err := StashDrop(r.dir, "stash@{0}"); err != nil {
		t.Fatal(err)
	}
	if err := StashStore(r.dir, hash, "keep me"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(r.git(t, "stash", "list", "--format=%gs")); got != "keep me" {
		t.Fatalf("restored %q", got)
	}
	if err := StashBranch(r.dir, "from-stash", "stash@{0}"); err != nil {
		t.Fatal(err)
	}
	if cur := strings.TrimSpace(r.git(t, "branch", "--show-current")); cur != "from-stash" {
		t.Errorf("branch %q", cur)
	}
	if got := r.read(t, "b.txt"); got != "b2\n" {
		t.Errorf("b.txt %q", got)
	}
}

// resetRepo is main with commits one, two, three.
func resetRepo(t *testing.T) *gitTestRepo {
	t.Helper()
	r := newGitTestRepo(t)
	for _, m := range []string{"one", "two", "three"} {
		r.write(t, "f.txt", m+"\n")
		r.git(t, "add", ".")
		r.git(t, "commit", "-q", "-m", m)
	}
	return r
}

// GITX-15: soft keeps the changes staged, mixed unstaged, keep keeps
// local changes.
func TestResetModes(t *testing.T) {
	r := resetRepo(t)
	if err := Reset(r.dir, ResetSoft, "HEAD~1"); err != nil {
		t.Fatal(err)
	}
	if got := stashPorcelain(t, r); got != "M  f.txt" {
		t.Errorf("soft %q", got)
	}
	r.git(t, "commit", "-q", "-m", "three again")
	if err := Reset(r.dir, ResetMixed, "HEAD~1"); err != nil {
		t.Fatal(err)
	}
	if got := stashPorcelain(t, r); got != " M f.txt" {
		t.Errorf("mixed %q", got)
	}
	if err := Reset(r.dir, ResetKeep, "HEAD~1"); err == nil {
		t.Error("keep reset over a changed file that differs")
	}
	if err := Reset(r.dir, "bogus", "HEAD"); err == nil {
		t.Error("accepted a bad mode")
	}
}

// GITX-15: a hard reset first saves the uncommitted changes as a stash
// and HEAD as a backup ref, so both come back.
func TestResetHardSafe(t *testing.T) {
	r := resetRepo(t)
	old := strings.TrimSpace(r.git(t, "rev-parse", "HEAD"))
	r.write(t, "f.txt", "unsaved work\n")
	b, err := ResetHardSafe(r.dir, "HEAD~2")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.read(t, "f.txt"); got != "one\n" {
		t.Errorf("f.txt %q", got)
	}
	if got := strings.TrimSpace(r.git(t, "rev-parse", b.Ref)); got != old {
		t.Errorf("backup ref %s at %s, want %s", b.Ref, got, old)
	}
	if !strings.HasPrefix(b.Ref, "refs/termocode/backup/") || !b.Stashed {
		t.Errorf("backup %+v", b)
	}
	r.git(t, "reset", "-q", "--hard", b.Ref)
	r.git(t, "stash", "pop", "-q")
	if got := r.read(t, "f.txt"); got != "unsaved work\n" {
		t.Errorf("work not back: %q", got)
	}
	// A clean tree has nothing to stash.
	r.git(t, "checkout", "-q", "--", "f.txt")
	if b, err := ResetHardSafe(r.dir, "HEAD~1"); err == nil && b.Stashed {
		t.Errorf("stashed a clean tree: %+v", b)
	}
}

// GITX-15: Uncommit undoes the last commit and keeps its changes staged;
// the first commit has no parent to go back to.
func TestUncommit(t *testing.T) {
	r := resetRepo(t)
	if err := Uncommit(r.dir); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(r.git(t, "log", "-1", "--format=%s")); got != "two" {
		t.Errorf("head %q", got)
	}
	if got := stashPorcelain(t, r); got != "M  f.txt" {
		t.Errorf("status %q", got)
	}
	r2 := newGitTestRepo(t)
	r2.write(t, "x", "x")
	r2.git(t, "add", ".")
	r2.git(t, "commit", "-q", "-m", "only")
	if err := Uncommit(r2.dir); err == nil {
		t.Error("uncommitted the first commit")
	}
}
