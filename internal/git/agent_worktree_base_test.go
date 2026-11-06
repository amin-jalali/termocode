package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// WFX-10: a workflow item's worktree starts from the each step's base
// branch (not the user's HEAD), from its last commit only; the user's
// branch, index and files stay as they were.
func TestCreateAgentWorktreeFromBase(t *testing.T) {
	r := committedRepo(t)
	r.git(t, "checkout", "-q", "-b", "release")
	r.write(t, "a.txt", "release\n")
	r.git(t, "commit", "-q", "-am", "release change")
	r.git(t, "checkout", "-q", "main")
	r.write(t, "b.txt", "uncommitted\n")
	before := r.git(t, "status", "--porcelain")

	w, err := CreateAgentWorktreeFrom(r.dir, "wf-1-a", "release")
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(w.Dir(), "a.txt")); got != "release\n" {
		t.Errorf("a.txt %q, want the release branch's", got)
	}
	if got := readFile(t, filepath.Join(w.Dir(), "b.txt")); got != "bee\n" {
		t.Errorf("b.txt %q: the user's uncommitted edit leaked in", got)
	}
	if b := strings.TrimSpace(r.git(t, "branch", "--show-current")); b != "main" {
		t.Errorf("user's branch is %q", b)
	}
	if after := r.git(t, "status", "--porcelain"); after != before {
		t.Errorf("user's status changed:\n%s\nwas\n%s", after, before)
	}
	// The changes are against the base, not against main.
	if err := os.WriteFile(filepath.Join(w.Dir(), "c.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ch, err := AgentChanges(r.dir, "wf-1-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(ch) != 1 || ch[0].Path != "c.txt" {
		t.Errorf("changes %v", ch)
	}
	// Again: the same worktree, the agent's work kept.
	w2, err := CreateAgentWorktreeFrom(r.dir, "wf-1-a", "release")
	if err != nil || w2.Path != w.Path {
		t.Fatalf("again: %v %q", err, w2.Path)
	}
}

func TestCreateAgentWorktreeFromEmptyBaseIsHead(t *testing.T) {
	r := committedRepo(t)
	w, err := CreateAgentWorktreeFrom(r.dir, "wf-2-a", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(w.Dir(), "a.txt")); got != "one\ntwo\nthree\n" {
		t.Errorf("a.txt %q", got)
	}
}

func TestCreateAgentWorktreeFromRefusesBadBase(t *testing.T) {
	r := committedRepo(t)
	for _, base := range []string{"no-such-branch", "-x", "--upload-pack=evil"} {
		if _, err := CreateAgentWorktreeFrom(r.dir, "wf-3-a", base); err == nil {
			t.Errorf("base %q: no error", base)
		}
	}
	if left, _ := LeftoverAgentWorktrees(r.dir); len(left) != 0 {
		t.Errorf("a refused base left %v", left)
	}
}
