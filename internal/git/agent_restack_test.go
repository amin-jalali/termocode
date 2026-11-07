package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// laneWithCommits makes agent worktree id from the user's HEAD and lets
// the agent commit twice (plus an uncommitted edit, which the restack
// snapshots first), then the user commits on their own branch.
func laneWithCommits(t *testing.T) (*gitTestRepo, AgentWorktree) {
	t.Helper()
	r := committedRepo(t)
	w, err := CreateAgentWorktreeFromHead(r.dir, "rs1")
	if err != nil {
		t.Fatal(err)
	}
	lane := &gitTestRepo{dir: w.Path}
	lane.write(t, "new.go", "package x\n")
	lane.git(t, "add", "-A")
	lane.git(t, "commit", "-q", "-m", "wip 1")
	lane.write(t, "a.txt", "one\nTWO\nthree\n")
	lane.git(t, "commit", "-q", "-am", "wip 2")
	lane.write(t, "b.txt", "bee\nbuzz\n") // not committed yet
	r.write(t, "c.txt", "sea\n")
	r.git(t, "add", "c.txt")
	r.git(t, "commit", "-q", "-m", "user moves on")
	return r, w
}

// Spec (CMP-56): restack with squash replays the lane's work on top of
// the user's current commit as ONE clean commit with the given message.
// Only the lane's branch changes: the user's branch, index and files stay;
// the lane's base moves to the user's commit, so its diff (and a later
// merge) is only the agent's work.
func TestRestackAgentWorktreeSquash(t *testing.T) {
	r, w := laneWithCommits(t)
	head := strings.TrimSpace(r.git(t, "rev-parse", "HEAD"))
	status := r.git(t, "status", "--porcelain")

	res, err := RestackAgentWorktree(r.dir, "rs1", AgentRestackOptions{Squash: true, Message: "feat: add x and change a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conflicts) != 0 || res.Before != 3 || res.After != 1 || res.Onto != head {
		t.Fatalf("result %+v (head %s)", res, head)
	}
	lane := &gitTestRepo{dir: w.Path}
	if p := strings.TrimSpace(lane.git(t, "rev-parse", "HEAD^")); p != head {
		t.Errorf("the clean commit's parent is %s, not the user's HEAD %s", p, head)
	}
	if msg := strings.TrimSpace(lane.git(t, "log", "-1", "--format=%B")); msg != "feat: add x and change a" {
		t.Errorf("message %q", msg)
	}
	if files := strings.Fields(lane.git(t, "show", "--name-only", "--format=", "HEAD")); strings.Join(files, " ") != "a.txt b.txt new.go" {
		t.Errorf("clean commit files %v", files)
	}
	if got := readFile(t, filepath.Join(w.Path, "c.txt")); got != "sea\n" {
		t.Errorf("the lane misses the user's new commit: %q", got)
	}
	if h := strings.TrimSpace(r.git(t, "rev-parse", "HEAD")); h != head {
		t.Errorf("the user's HEAD moved to %s", h)
	}
	if after := r.git(t, "status", "--porcelain"); after != status {
		t.Errorf("the user's status changed:\n%s\n→\n%s", status, after)
	}
	ch, err := AgentChanges(r.dir, "rs1")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, c := range ch {
		paths = append(paths, c.Path)
	}
	if strings.Join(paths, " ") != "a.txt b.txt new.go" {
		t.Errorf("lane changes after restack %v (c.txt must not show)", paths)
	}
}

// Spec (CMP-56): without squash the lane's commits are kept, one by one,
// on top of the user's commit.
func TestRestackAgentWorktreeKeepCommits(t *testing.T) {
	r, w := laneWithCommits(t)
	head := strings.TrimSpace(r.git(t, "rev-parse", "HEAD"))
	res, err := RestackAgentWorktree(r.dir, "rs1", AgentRestackOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Before != 3 || res.After != 3 {
		t.Fatalf("result %+v", res)
	}
	lane := &gitTestRepo{dir: w.Path}
	subjects := strings.Split(strings.TrimSpace(lane.git(t, "log", "--format=%s", head+"..HEAD")), "\n")
	if len(subjects) != 3 || subjects[1] != "wip 2" || subjects[2] != "wip 1" {
		t.Errorf("subjects %q", subjects)
	}
}

// Spec (CMP-56): a conflict with the user's commit stops nothing half
// done: the rebase is aborted, the lane's branch and files are as before,
// and the clashing files are listed.
func TestRestackAgentWorktreeConflict(t *testing.T) {
	r, w := laneWithCommits(t)
	r.write(t, "a.txt", "one\nMINE\nthree\n")
	r.git(t, "commit", "-q", "-am", "user edits a")
	lane := &gitTestRepo{dir: w.Path}
	tipBefore := strings.TrimSpace(lane.git(t, "rev-parse", "HEAD"))

	res, err := RestackAgentWorktree(r.dir, "rs1", AgentRestackOptions{Squash: true, Message: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Conflicts, " ") != "a.txt" {
		t.Fatalf("conflicts %v", res.Conflicts)
	}
	if _, err := os.Stat(filepath.Join(w.Path, ".git")); err != nil {
		t.Fatal(err)
	}
	gd := strings.TrimSpace(lane.git(t, "rev-parse", "--absolute-git-dir"))
	for _, d := range []string{"rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(gd, d)); err == nil {
			t.Errorf("a rebase is still in progress (%s)", d)
		}
	}
	// The snapshot commit of the uncommitted edit is the only new thing.
	if p := strings.TrimSpace(lane.git(t, "rev-parse", "HEAD^")); p != tipBefore {
		t.Errorf("the lane's branch moved: HEAD^ %s, tip before %s", p, tipBefore)
	}
	if got := readFile(t, filepath.Join(w.Path, "a.txt")); got != "one\nTWO\nthree\n" {
		t.Errorf("the lane's a.txt %q", got)
	}
}

// Spec (CMP-56): a lane with no commit after its base has nothing to
// restack; an empty squash message is refused.
func TestRestackAgentWorktreeRefuses(t *testing.T) {
	r := committedRepo(t)
	if _, err := CreateAgentWorktreeFromHead(r.dir, "rs2"); err != nil {
		t.Fatal(err)
	}
	if _, err := RestackAgentWorktree(r.dir, "rs2", AgentRestackOptions{}); err != ErrRestackNothing {
		t.Errorf("empty lane: %v", err)
	}
	r2, _ := laneWithCommits(t)
	if _, err := RestackAgentWorktree(r2.dir, "rs1", AgentRestackOptions{Squash: true, Message: "  "}); err == nil {
		t.Error("an empty squash message was taken")
	}
}
