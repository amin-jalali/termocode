package git

import (
	"strings"
	"testing"
)

// pickerRepo: main (2 commits), feat/a tracking main and one commit ahead
// and one behind it, feat/b, fix-c; a remote "origin" with main and
// remote-only.
func pickerRepo(t *testing.T) *gitTestRepo {
	t.Helper()
	up := newGitTestRepo(t)
	up.write(t, "a.txt", "a\n")
	up.git(t, "add", ".")
	up.git(t, "commit", "-q", "-m", "first")
	up.git(t, "branch", "remote-only")
	r := newGitTestRepo(t)
	r.git(t, "remote", "add", "origin", up.dir)
	r.git(t, "fetch", "-q", "origin")
	r.git(t, "reset", "-q", "--hard", "origin/main")
	r.git(t, "checkout", "-q", "-b", "feat/a")
	r.git(t, "branch", "-q", "--set-upstream-to=main")
	r.write(t, "f.txt", "f\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "feat a")
	r.git(t, "checkout", "-q", "main")
	r.write(t, "m.txt", "m\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "main 2")
	r.git(t, "branch", "feat/b")
	r.git(t, "branch", "fix-c")
	return r
}

// GITX-32: every local branch with its upstream and how far ahead /
// behind it is; the current one is marked.
func TestBranchList(t *testing.T) {
	r := pickerRepo(t)
	list, err := BranchList(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]BranchInfo{}
	for _, b := range list {
		got[b.Name] = b
	}
	a := got["feat/a"]
	if a.Upstream != "main" || a.Ahead != 1 || a.Behind != 1 || a.Current {
		t.Errorf("feat/a = %+v", a)
	}
	if m := got["main"]; !m.Current {
		t.Errorf("main = %+v", m)
	}
	if len(got) != 4 {
		t.Errorf("branches = %v", list)
	}
}

// Recent branches come from the checkouts in the reflog, newest first,
// each once, only branches that still exist.
func TestRecentBranches(t *testing.T) {
	r := pickerRepo(t)
	r.git(t, "checkout", "-q", "fix-c")
	r.git(t, "checkout", "-q", "-b", "gone")
	r.git(t, "checkout", "-q", "feat/b")
	r.git(t, "branch", "-q", "-D", "gone")
	r.git(t, "checkout", "-q", "main")
	got := strings.Join(RecentBranches(r.dir, 10), ",")
	if !strings.HasPrefix(got, "main,feat/b,fix-c") || strings.Contains(got, "gone") {
		t.Errorf("recent = %s", got)
	}
	if n := len(RecentBranches(r.dir, 2)); n != 2 {
		t.Errorf("limit 2 gave %d", n)
	}
}

// A remote branch is checked out as a new local branch that tracks it.
func TestCheckoutTracking(t *testing.T) {
	r := pickerRepo(t)
	if err := CheckoutTracking(r.dir, "origin/remote-only"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(r.git(t, "rev-parse", "--abbrev-ref", "HEAD")); got != "remote-only" {
		t.Fatalf("HEAD = %s", got)
	}
	if got := strings.TrimSpace(r.git(t, "rev-parse", "--abbrev-ref", "@{u}")); got != "origin/remote-only" {
		t.Fatalf("upstream = %s", got)
	}
}

// When local changes block a switch, Stash and Switch saves them in the
// stash (untracked files too) and switches; IsBlockedByChanges knows the
// error.
func TestStashAndSwitch(t *testing.T) {
	r := pickerRepo(t)
	r.git(t, "checkout", "-q", "feat/a")
	r.write(t, "m.txt", "mine\n") // m.txt is only on main: untracked here, overwritten there
	r.write(t, "a.txt", "changed\n")
	err := Checkout(r.dir, "main")
	if err == nil || !IsBlockedByChanges(err) {
		t.Fatalf("checkout = %v, want blocked by changes", err)
	}
	if err := StashAndSwitch(r.dir, "main"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(r.git(t, "rev-parse", "--abbrev-ref", "HEAD")); got != "main" {
		t.Fatalf("HEAD = %s", got)
	}
	if s := r.git(t, "stash", "list"); !strings.Contains(s, "before switching to main") {
		t.Fatalf("stash = %q", s)
	}
	if IsBlockedByChanges(errString("fatal: invalid reference: nope")) {
		t.Error("an unknown branch is not blocked by changes")
	}
	// A switch that fails after the stash gives the changes back.
	r.write(t, "a.txt", "again\n")
	if err := StashAndSwitch(r.dir, "nope"); err == nil {
		t.Fatal("switch to a missing branch worked")
	}
	if got := strings.TrimSpace(r.git(t, "status", "--porcelain")); !strings.Contains(got, "a.txt") {
		t.Fatalf("changes not given back: %q", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
