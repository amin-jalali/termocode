package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func committedRepo(t *testing.T) *gitTestRepo {
	t.Helper()
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "one\ntwo\nthree\n")
	r.write(t, "b.txt", "bee\n")
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "init")
	return r
}

// An agent worktree lives in the git dir (not in the project, so search
// and the explorer never see it), on its own branch, and starts from the
// user's working state (uncommitted edits included) without touching the
// user's branch, index or files.
func TestCreateAgentWorktree(t *testing.T) {
	r := committedRepo(t)
	r.write(t, "a.txt", "one\nTWO\nthree\n") // the user's uncommitted edit
	r.write(t, "staged.txt", "s\n")
	r.git(t, "add", "staged.txt")
	statusBefore := r.git(t, "status", "--porcelain")

	w, err := CreateAgentWorktree(r.dir, "r1-left")
	if err != nil {
		t.Fatal(err)
	}
	if w.Branch != "termocode/agent-r1-left" {
		t.Errorf("branch %q", w.Branch)
	}
	if !strings.Contains(filepath.ToSlash(w.Path), "/.git/termocode/worktrees/r1-left") {
		t.Errorf("path %q is not in the git dir", w.Path)
	}
	if got := readFile(t, filepath.Join(w.Path, "a.txt")); got != "one\nTWO\nthree\n" {
		t.Errorf("worktree misses the user's edit: %q", got)
	}
	if got := readFile(t, filepath.Join(w.Path, "staged.txt")); got != "s\n" {
		t.Errorf("worktree misses the staged file: %q", got)
	}
	if b := strings.TrimSpace(r.git(t, "branch", "--show-current")); b != "main" {
		t.Errorf("user's branch changed to %q", b)
	}
	if after := r.git(t, "status", "--porcelain"); after != statusBefore {
		t.Errorf("user's status changed:\n%s\nwant\n%s", after, statusBefore)
	}
	// Create again (a retry) keeps the same tree.
	if err := os.WriteFile(filepath.Join(w.Path, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	w2, err := CreateAgentWorktree(r.dir, "r1-left")
	if err != nil || w2 != w {
		t.Fatalf("second create: %+v %v", w2, err)
	}
	if _, err := os.Stat(filepath.Join(w.Path, "new.txt")); err != nil {
		t.Error("second create lost the agent's file")
	}
}

// From a sub folder of the repository, the agent's workspace is the same
// folder in its worktree.
func TestCreateAgentWorktreeFromSubFolder(t *testing.T) {
	r := committedRepo(t)
	r.write(t, "pkg/x.go", "package x\n")
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "pkg")
	w, err := CreateAgentWorktree(filepath.Join(r.dir, "pkg"), "sub")
	if err != nil {
		t.Fatal(err)
	}
	if w.Sub != "pkg" || w.Dir() != filepath.Join(w.Path, "pkg") {
		t.Errorf("sub %q dir %q", w.Sub, w.Dir())
	}
	if _, err := os.Stat(filepath.Join(w.Dir(), "x.go")); err != nil {
		t.Error(err)
	}
}

func TestCreateAgentWorktreeNeedsACommit(t *testing.T) {
	r := newGitTestRepo(t)
	if _, err := CreateAgentWorktree(r.dir, "x"); err == nil {
		t.Error("a repo with no commit: want an error")
	}
	if _, err := CreateAgentWorktree(t.TempDir(), "x"); err == nil {
		t.Error("not a repo: want an error")
	}
}

// Changes lists what the agent changed (new, edited, deleted files), and
// the patch shows it; a clean merge applies it to the user's tree,
// leaves the user's own other edits alone, and removes the worktree and
// its branch.
func TestAgentWorktreeCleanMerge(t *testing.T) {
	r := committedRepo(t)
	w, err := CreateAgentWorktree(r.dir, "r2-a")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(w.Path, "a.txt"), []byte("one\ntwo\nthree\nfour\n"), 0o644)
	os.WriteFile(filepath.Join(w.Path, "c.txt"), []byte("sea\n"), 0o644)
	os.Remove(filepath.Join(w.Path, "b.txt"))
	r.write(t, "user.txt", "mine\n") // the user works on meanwhile

	ch, err := AgentChanges(r.dir, "r2-a")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, c := range ch {
		got = append(got, c.Status+" "+c.Path)
	}
	if strings.Join(got, ",") != "M a.txt,D b.txt,A c.txt" {
		t.Errorf("changes %v", got)
	}
	patch, err := AgentDiff(r.dir, "r2-a")
	if err != nil || !strings.Contains(patch, "+four") || !strings.Contains(patch, "+sea") {
		t.Errorf("diff %q %v", patch, err)
	}

	res, err := MergeAgentWorktree(r.dir, "r2-a", false)
	if err != nil || len(res.Conflicts) != 0 || len(res.Applied) != 3 {
		t.Fatalf("merge %+v %v", res, err)
	}
	if got := readFile(t, filepath.Join(r.dir, "a.txt")); got != "one\ntwo\nthree\nfour\n" {
		t.Errorf("a.txt %q", got)
	}
	if got := readFile(t, filepath.Join(r.dir, "c.txt")); got != "sea\n" {
		t.Errorf("c.txt %q", got)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "b.txt")); !os.IsNotExist(err) {
		t.Error("b.txt still there")
	}
	if got := readFile(t, filepath.Join(r.dir, "user.txt")); got != "mine\n" {
		t.Error("user's file changed")
	}
	if err := RemoveAgentWorktree(r.dir, "r2-a", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.Path); !os.IsNotExist(err) {
		t.Error("worktree dir still there")
	}
	if b := r.git(t, "branch", "--list", w.Branch); strings.TrimSpace(b) != "" {
		t.Errorf("branch still there: %q", b)
	}
	if left, err := LeftoverAgentWorktrees(r.dir); err != nil || len(left) != 0 {
		t.Errorf("leftovers %+v %v", left, err)
	}
}

// When the user changed the same file in the meantime, a merge first
// reports the conflict and writes nothing; merging with markers then
// writes both sides with conflict markers. The agent's work stays on its
// branch either way.
func TestAgentWorktreeConflict(t *testing.T) {
	r := committedRepo(t)
	w, err := CreateAgentWorktree(r.dir, "r3-a")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(w.Path, "a.txt"), []byte("one\nAGENT\nthree\n"), 0o644)
	os.WriteFile(filepath.Join(w.Path, "c.txt"), []byte("sea\n"), 0o644)
	r.write(t, "a.txt", "one\nUSER\nthree\n")

	res, err := MergeAgentWorktree(r.dir, "r3-a", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Conflicts, ",") != "a.txt" || len(res.Applied) != 0 {
		t.Fatalf("dry merge %+v", res)
	}
	if got := readFile(t, filepath.Join(r.dir, "a.txt")); got != "one\nUSER\nthree\n" {
		t.Errorf("a.txt changed on a conflict: %q", got)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "c.txt")); !os.IsNotExist(err) {
		t.Error("c.txt written although the merge stopped")
	}

	res, err = MergeAgentWorktree(r.dir, "r3-a", true)
	if err != nil || strings.Join(res.Conflicts, ",") != "a.txt" {
		t.Fatalf("forced merge %+v %v", res, err)
	}
	a := readFile(t, filepath.Join(r.dir, "a.txt"))
	if !strings.Contains(a, "<<<<<<<") || !strings.Contains(a, "USER") || !strings.Contains(a, "AGENT") {
		t.Errorf("a.txt has no markers: %q", a)
	}
	if got := readFile(t, filepath.Join(r.dir, "c.txt")); got != "sea\n" {
		t.Errorf("c.txt %q", got)
	}
	// Keep the branch: the worktree goes, the work stays.
	if err := RemoveAgentWorktree(r.dir, "r3-a", false); err != nil {
		t.Fatal(err)
	}
	if b := r.git(t, "branch", "--list", w.Branch); strings.TrimSpace(b) == "" {
		t.Error("branch deleted")
	}
	if got := r.git(t, "show", w.Branch+":a.txt"); got != "one\nAGENT\nthree\n" {
		t.Errorf("branch content %q", got)
	}
}

// A crash leaves worktrees behind: they are listed (never removed by
// the listing itself).
func TestLeftoverAgentWorktrees(t *testing.T) {
	r := committedRepo(t)
	for _, id := range []string{"r4-a", "r4-b"} {
		if _, err := CreateAgentWorktree(r.dir, id); err != nil {
			t.Fatal(err)
		}
	}
	r.git(t, "worktree", "add", "-q", "-b", "other", filepath.Join(t.TempDir(), "o"))
	left, err := LeftoverAgentWorktrees(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, l := range left {
		ids = append(ids, l.ID)
		if _, err := os.Stat(l.Path); err != nil {
			t.Errorf("%s: listing removed it", l.ID)
		}
	}
	if strings.Join(ids, ",") != "r4-a,r4-b" {
		t.Errorf("leftovers %v", ids)
	}
}

func TestAgentWorktreeID(t *testing.T) {
	if got := AgentWorktreeID("R1", "Left Step/x"); got != "r1-left-step-x" {
		t.Errorf("id %q", got)
	}
}

// AgentFiles gives each changed file's text before and after the agent,
// for the review card.
func TestAgentFiles(t *testing.T) {
	r := committedRepo(t)
	w, err := CreateAgentWorktree(r.dir, "r5-a")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(w.Path, "a.txt"), []byte("one\n"), 0o644)
	os.WriteFile(filepath.Join(w.Path, "c.txt"), []byte("sea\n"), 0o644)
	os.Remove(filepath.Join(w.Path, "b.txt"))
	files, err := AgentFiles(r.dir, "r5-a")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, f := range files {
		got = append(got, f.Status+" "+f.Path+" "+strings.TrimSpace(f.Before)+">"+strings.TrimSpace(f.After))
	}
	if strings.Join(got, "|") != "M a.txt one\ntwo\nthree>one|D b.txt bee>|A c.txt >sea" {
		t.Errorf("files %q", got)
	}
}

// Steps that start together create their worktrees at the same time.
func TestCreateAgentWorktreesAtOnce(t *testing.T) {
	r := committedRepo(t)
	r.write(t, "a.txt", "dirty\n")
	errs := make(chan error, 4)
	for _, id := range []string{"p-a", "p-b", "p-c", "p-d"} {
		go func(id string) {
			_, err := CreateAgentWorktree(r.dir, id)
			errs <- err
		}(id)
	}
	for i := 0; i < 4; i++ {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	if left, err := LeftoverAgentWorktrees(r.dir); err != nil || len(left) != 4 {
		t.Errorf("worktrees %d %v", len(left), err)
	}
}

// Worktrees made at the same time (the steps of one wave) each start from
// the user's uncommitted edits: `git stash create` takes the index lock,
// so a create that loses the race must wait for it, not start from HEAD.
func TestCreateAgentWorktreesAtOnceKeepEdits(t *testing.T) {
	r := committedRepo(t)
	r.write(t, "a.txt", "dirty\n")
	const n = 8
	type res struct {
		w   AgentWorktree
		err error
	}
	out := make(chan res, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		go func(id string) {
			<-start
			w, err := CreateAgentWorktree(r.dir, id)
			out <- res{w, err}
		}(AgentWorktreeID("r", string(rune('a'+i))))
	}
	close(start)
	for i := 0; i < n; i++ {
		x := <-out
		if x.err != nil {
			t.Errorf("create: %v", x.err)
			continue
		}
		if got := readFile(t, filepath.Join(x.w.Path, "a.txt")); got != "dirty\n" {
			t.Errorf("%s misses the user's edit: %q", x.w.ID, got)
		}
	}
}

// A lock another git process keeps is waited for a bounded time; then the
// create fails with the reason and leaves nothing (no worktree from HEAD
// that misses the user's edits).
func TestCreateAgentWorktreeLockedIndexFails(t *testing.T) {
	r := committedRepo(t)
	r.write(t, "a.txt", "dirty\n")
	tries, wait := agentLockTries, agentLockWait
	agentLockTries, agentLockWait = 3, time.Millisecond
	t.Cleanup(func() { agentLockTries, agentLockWait = tries, wait })
	lock := filepath.Join(r.dir, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := CreateAgentWorktree(r.dir, "r-locked")
	if err == nil || !strings.Contains(err.Error(), "uncommitted edits") {
		t.Fatalf("err %v, want the uncommitted edits reason", err)
	}
	if left, _ := LeftoverAgentWorktrees(r.dir); len(left) != 0 {
		t.Errorf("left %v", left)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	w, err := CreateAgentWorktree(r.dir, "r-locked")
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(w.Path, "a.txt")); got != "dirty\n" {
		t.Errorf("worktree misses the user's edit: %q", got)
	}
}

// A merge keeps every kind of file the agent touched: a script stays
// executable, a symlink stays a link, binary bytes and an empty file are
// written as they are, and a name with spaces works; a nested new folder
// is made.
func TestAgentWorktreeMergeFileKinds(t *testing.T) {
	r := committedRepo(t)
	os.Chmod(r.write(t, "run.sh", "#!/bin/sh\necho hi\n"), 0o755)
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "script")
	w, err := CreateAgentWorktree(r.dir, "r6-kinds")
	if err != nil {
		t.Fatal(err)
	}
	bin := []byte{0, 1, 2, 0xff, '\n', 0}
	os.WriteFile(filepath.Join(w.Path, "run.sh"), []byte("#!/bin/sh\necho agent\n"), 0o755)
	os.WriteFile(filepath.Join(w.Path, "new.sh"), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(w.Path, "data.bin"), bin, 0o644)
	os.WriteFile(filepath.Join(w.Path, "empty.txt"), nil, 0o644)
	os.MkdirAll(filepath.Join(w.Path, "new dir", "deep"), 0o755)
	os.WriteFile(filepath.Join(w.Path, "new dir", "deep", "my file.txt"), []byte("spaced\n"), 0o644)
	if err := os.Symlink("a.txt", filepath.Join(w.Path, "link")); err != nil {
		t.Skip("no symlinks here")
	}
	files, err := AgentFiles(r.dir, "r6-kinds")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, f := range files {
		got = append(got, f.Status+" "+f.Path)
	}
	if strings.Join(got, "|") != "A data.bin|A empty.txt|A link|A new dir/deep/my file.txt|A new.sh|M run.sh" {
		t.Errorf("files %q", got)
	}
	res, err := MergeAgentWorktree(r.dir, "r6-kinds", false)
	if err != nil || len(res.Conflicts) != 0 || len(res.Applied) != 6 {
		t.Fatalf("merge %+v %v", res, err)
	}
	if fi, err := os.Stat(filepath.Join(r.dir, "new.sh")); err != nil || fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("new.sh %v %v", fi, err)
	}
	if b, _ := os.ReadFile(filepath.Join(r.dir, "data.bin")); string(b) != string(bin) {
		t.Errorf("data.bin %q", b)
	}
	if fi, err := os.Stat(filepath.Join(r.dir, "empty.txt")); err != nil || fi.Size() != 0 {
		t.Errorf("empty.txt %v %v", fi, err)
	}
	if got := readFile(t, filepath.Join(r.dir, "new dir", "deep", "my file.txt")); got != "spaced\n" {
		t.Errorf("spaced file %q", got)
	}
	if l, err := os.Readlink(filepath.Join(r.dir, "link")); err != nil || l != "a.txt" {
		t.Errorf("link %q %v", l, err)
	}
	fi, err := os.Stat(filepath.Join(r.dir, "run.sh"))
	if err != nil || fi.Mode().Perm()&0o100 == 0 || readFile(t, filepath.Join(r.dir, "run.sh")) != "#!/bin/sh\necho agent\n" {
		t.Errorf("run.sh %v %v", fi, err)
	}
}
