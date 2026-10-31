package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentWorktreeOutsideARepo(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateAgentWorktree(dir, ""); err == nil || !strings.Contains(err.Error(), "no worktree id") {
		t.Errorf("create with no id = %v", err)
	}
	if _, err := CreateAgentWorktree(dir, "x"); err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("create outside a repo = %v", err)
	}
	if _, err := AgentWorktreePath(dir, "x"); err == nil {
		t.Error("AgentWorktreePath outside a repo: want error")
	}
	if _, err := AgentChanges(dir, "x"); err == nil {
		t.Error("AgentChanges outside a repo: want error")
	}
	if _, err := AgentDiff(dir, "x"); err == nil {
		t.Error("AgentDiff outside a repo: want error")
	}
	if _, err := AgentFiles(dir, "x"); err == nil {
		t.Error("AgentFiles outside a repo: want error")
	}
	if _, err := MergeAgentWorktree(dir, "x", true); err == nil {
		t.Error("MergeAgentWorktree outside a repo: want error")
	}
	if err := RemoveAgentWorktree(dir, "x", true); err == nil {
		t.Error("RemoveAgentWorktree outside a repo: want error")
	}
	if _, err := LeftoverAgentWorktrees(dir); err == nil {
		t.Error("LeftoverAgentWorktrees outside a repo: want error")
	}
}

func TestAgentWorktreePathIsInTheGitDir(t *testing.T) {
	r := committedRepo(t)
	w, err := AgentWorktreePath(r.dir, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(filepath.ToSlash(w.Path), "/.git/termocode/worktrees/p1") || w.Branch != AgentBranchPrefix+"p1" {
		t.Errorf("AgentWorktreePath = %+v", w)
	}
	if _, err := os.Stat(w.Path); !os.IsNotExist(err) {
		t.Error("AgentWorktreePath must not create the folder")
	}
}

func TestAgentUnknownAndGoneBranch(t *testing.T) {
	r := committedRepo(t)
	if _, err := AgentChanges(r.dir, "nope"); err == nil || !strings.Contains(err.Error(), "not known") {
		t.Errorf("AgentChanges of an unknown id = %v", err)
	}
	w, err := CreateAgentWorktree(r.dir, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveAgentWorktree(r.dir, "g1", false); err != nil {
		t.Fatal(err)
	}
	r.git(t, "branch", "-D", w.Branch)
	if _, err := AgentDiff(r.dir, "g1"); err == nil || !strings.Contains(err.Error(), "is gone") {
		t.Errorf("AgentDiff with the branch deleted = %v, want branch is gone", err)
	}
}

// The user deleted the worktree folder by hand: the next create makes it
// again at once (git still lists the missing folder until a prune).
func TestCreateAgentWorktreeAfterFolderDeletedByHand(t *testing.T) {
	r := committedRepo(t)
	w, err := CreateAgentWorktree(r.dir, "h1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(w.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateAgentWorktree(r.dir, "h1"); err != nil {
		t.Fatalf("create after the folder was deleted: %v", err)
	}
	if got := readFile(t, filepath.Join(w.Path, "a.txt")); got != "one\ntwo\nthree\n" {
		t.Errorf("worktree a.txt = %q", got)
	}
}

// An agent that makes a script executable, turns a file into a symlink and
// deletes another: the merge writes the same kinds into the user's tree.
func TestMergeAgentModesSymlinksAndDeletes(t *testing.T) {
	r := committedRepo(t)
	w, err := CreateAgentWorktree(r.dir, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Path, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(w.Path, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("b.txt", filepath.Join(w.Path, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(w.Path, "b.txt")); err != nil {
		t.Fatal(err)
	}

	ch, err := AgentChanges(r.dir, "m1")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range ch {
		got[c.Path] = c.Status
	}
	if got["a.txt"] != "M" || got["b.txt"] != "D" || got["run.sh"] != "A" {
		t.Fatalf("AgentChanges = %v, want a.txt M (type change), b.txt D, run.sh A", got)
	}

	res, err := MergeAgentWorktree(r.dir, "m1", false)
	if err != nil || len(res.Conflicts) != 0 || len(res.Applied) != 3 {
		t.Fatalf("merge = %+v, %v", res, err)
	}
	fi, err := os.Stat(filepath.Join(r.dir, "run.sh"))
	if err != nil || fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("run.sh mode = %v, %v; want executable", fi, err)
	}
	if l, err := os.Readlink(filepath.Join(r.dir, "a.txt")); err != nil || l != "b.txt" {
		t.Errorf("a.txt link = %q, %v; want a symlink to b.txt", l, err)
	}
	if _, err := os.Lstat(filepath.Join(r.dir, "b.txt")); !os.IsNotExist(err) {
		t.Error("b.txt still there after the agent deleted it")
	}
}

// The user already made the same change as the agent: no conflict.
func TestMergeAgentSameChangeAsUser(t *testing.T) {
	r := committedRepo(t)
	w, err := CreateAgentWorktree(r.dir, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Path, "a.txt"), []byte("same\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.write(t, "a.txt", "same\n")
	res, err := MergeAgentWorktree(r.dir, "s1", false)
	if err != nil || len(res.Conflicts) != 0 || strings.Join(res.Applied, ",") != "a.txt" {
		t.Errorf("merge of an equal change = %+v, %v", res, err)
	}
}

// A conflict on a binary file or a symlink cannot get markers: the user's
// version is kept and listed.
func TestMergeAgentForcedBinaryAndSymlinkConflictKeepsYours(t *testing.T) {
	r := committedRepo(t)
	r.write(t, "bin.dat", "base\x00data")
	r.git(t, "add", "bin.dat")
	r.git(t, "commit", "-qm", "bin")
	w, err := CreateAgentWorktree(r.dir, "k1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Path, "bin.dat"), []byte("agent\x00data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", filepath.Join(w.Path, "link")); err != nil {
		t.Fatal(err)
	}
	r.write(t, "bin.dat", "user\x00data")
	if err := os.Symlink("b.txt", filepath.Join(r.dir, "link")); err != nil {
		t.Fatal(err)
	}

	res, err := MergeAgentWorktree(r.dir, "k1", true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Kept, ",") != "bin.dat,link" || len(res.Conflicts) != 2 {
		t.Errorf("forced merge = %+v, want bin.dat and link kept", res)
	}
	if got := readFile(t, filepath.Join(r.dir, "bin.dat")); got != "user\x00data" {
		t.Errorf("bin.dat = %q, want the user's", got)
	}
	if l, _ := os.Readlink(filepath.Join(r.dir, "link")); l != "b.txt" {
		t.Errorf("link -> %q, want the user's b.txt", l)
	}
}

// With no git identity at all, the agent's snapshot commit still works
// under a termocode identity.
func TestAgentSnapshotWithoutIdentity(t *testing.T) {
	r := committedRepo(t)
	r.git(t, "config", "--unset", "user.email")
	r.git(t, "config", "--unset", "user.name")
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"} {
		v, had := os.LookupEnv(k)
		os.Unsetenv(k)
		if had {
			t.Cleanup(func() { os.Setenv(k, v) })
		}
	}
	t.Setenv("EMAIL", "")
	w, err := CreateAgentWorktree(r.dir, "i1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Path, "new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AgentChanges(r.dir, "i1"); err != nil {
		t.Fatalf("snapshot without identity: %v", err)
	}
	if got := strings.TrimSpace(r.git(t, "log", "-1", "--format=%ae", w.Branch)); got != "termocode@localhost" {
		t.Errorf("snapshot author = %q, want termocode@localhost", got)
	}
}

// FIX-27: the agent makes existing files executable, one with the same
// text and one with new text; the merge sets the exec bit on both.
func TestMergeAgentMakesAnExistingFileExecutable(t *testing.T) {
	r := committedRepo(t)
	w, err := CreateAgentWorktree(r.dir, "x1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(w.Path, "a.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Path, "b.txt"), []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(w.Path, "b.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := MergeAgentWorktree(r.dir, "x1", false)
	if err != nil || len(res.Conflicts) != 0 || strings.Join(res.Applied, ",") != "a.txt,b.txt" {
		t.Fatalf("merge = %+v, %v", res, err)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		fi, err := os.Stat(filepath.Join(r.dir, name))
		if err != nil || fi.Mode().Perm()&0o100 == 0 {
			t.Errorf("%s mode = %v, %v; want executable", name, fi.Mode(), err)
		}
	}
}
