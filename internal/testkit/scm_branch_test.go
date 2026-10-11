package testkit

import (
	"strings"
	"testing"
)

// Source Control: branches and stashes.

// Spec: Given main, when the user picks "Create new branch…" and types a
// name, then git is on that new branch (same commit) and the panel and the
// status bar show its name.
func TestSCMBranchCreate(t *testing.T) {
	d, r := scmDriver(t, nil)
	head := r.git("rev-parse", "HEAD")
	d.Palette("git-branch")
	if !strings.Contains(text(d), "Create new branch") {
		t.Fatalf("branch picker not shown:%s", dump(d))
	}
	d.Key("enter") // the first item
	if !strings.Contains(text(d), "Branch name") {
		t.Fatalf("no branch name prompt:%s", dump(d))
	}
	d.Type("feature/login")
	d.Key("enter")
	until(d, func() bool { return strings.Contains(statusBar(d), "feature/login") })
	if got := r.git("branch", "--show-current"); got != "feature/login" {
		t.Errorf("current branch %q, want feature/login", got)
	}
	if got := r.git("rev-parse", "HEAD"); got != head {
		t.Errorf("new branch is not at the old HEAD")
	}
	if !strings.Contains(sidebar(d)[2], "feature/login") {
		t.Errorf("panel branch row: %q", sidebar(d)[2])
	}
	if !strings.Contains(statusBar(d), "feature/login") {
		t.Errorf("status bar: %q", statusBar(d))
	}
}

// Spec: Given a branch name that already exists, when the user creates it,
// then git stays on the current branch and an error toast says it exists.
func TestSCMBranchCreateExistingFails(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) { r.git("branch", "dev") })
	d.Palette("git-branch")
	d.Key("enter")
	d.Type("dev")
	d.Key("enter")
	until(d, func() bool { return strings.Contains(text(d), "Branch failed") })
	if !strings.Contains(text(d), "already exists") {
		t.Errorf("toast does not say the branch exists:%s", dump(d))
	}
	if got := r.git("branch", "--show-current"); got != "main" {
		t.Errorf("current branch %q, want main", got)
	}
}

// Spec: Given branches main (current) and dev with its own commit, when the
// user picks dev in the branch picker, then git is on dev, dev's file is on
// disk, and the graph shows dev's commit on top.
func TestSCMBranchSwitch(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.git("checkout", "-q", "-b", "dev")
		r.write("dev.txt", "d\n")
		r.commit("feat: dev work")
		r.git("checkout", "-q", "main")
	})
	d.Palette("git-branch")
	if !strings.Contains(text(d), "current") {
		t.Errorf("picker does not mark the current branch:%s", dump(d))
	}
	d.Type("dev")
	d.Key("enter")
	until(d, func() bool { return strings.Contains(statusBar(d), "dev") })
	if got := r.git("branch", "--show-current"); got != "dev" {
		t.Errorf("current branch %q, want dev", got)
	}
	if !r.exists("dev.txt") {
		t.Error("dev.txt not checked out")
	}
	if g := section(d, "GRAPH"); len(g) == 0 || !strings.Contains(g[0], "dev work") {
		t.Errorf("graph top row: %q", g)
	}
}

// Spec: Given a local edit that the other branch would overwrite, when the
// user switches, then git refuses, the edit is kept, the branch does not
// change and the toast says why.
func TestSCMBranchSwitchBlockedByLocalChanges(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.git("checkout", "-q", "-b", "dev")
		r.write("main.go", "package main\n// dev\n")
		r.commit("feat: dev")
		r.git("checkout", "-q", "main")
		r.write("main.go", "package main\n// local\n")
	})
	d.Palette("git-branch")
	d.Type("dev")
	d.Key("enter")
	until(d, func() bool { return strings.Contains(text(d), "Checkout failed") })
	if !strings.Contains(text(d), "Your local changes") {
		t.Errorf("toast does not say why:%s", dump(d))
	}
	if got := r.git("branch", "--show-current"); got != "main" {
		t.Errorf("branch %q, want main", got)
	}
	if got := r.read("main.go"); got != "package main\n// local\n" {
		t.Errorf("local edit lost: %q", got)
	}
}

// Spec: Given a modified tracked file, when the user stashes, then the
// edit is gone from the tree and saved in stash@{0}; when the user pops,
// then the edit is back and the stash list is empty.
func TestSCMStashPushAndPop(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("main.go", "package main\n// wip\n")
	})
	d.Palette("git-stash")
	until(d, func() bool { return inSection(d, "CHANGES", "No changes") })
	if got := r.read("main.go"); got != scmFiles["main.go"] {
		t.Errorf("after stash main.go = %q", got)
	}
	if got := r.git("stash", "list"); !strings.Contains(got, "stash@{0}") {
		t.Errorf("no stash entry: %q", got)
	}
	if !strings.Contains(text(d), "Stashed") {
		t.Errorf("no Stashed toast:%s", dump(d))
	}

	d.Palette("git-stash-pop")
	until(d, func() bool { return inSection(d, "CHANGES", "main.go") })
	if got := r.read("main.go"); got != "package main\n// wip\n" {
		t.Errorf("after pop main.go = %q", got)
	}
	if got := r.git("stash", "list"); got != "" {
		t.Errorf("stash not dropped by pop: %q", got)
	}
}

// Spec: Given a clean tree, when the user stashes, then nothing is stashed
// and a warning says there is nothing to save; when the user pops with no
// stash, then an error toast shows and nothing changes.
func TestSCMStashNothing(t *testing.T) {
	d, r := scmDriver(t, nil)
	d.Palette("git-stash")
	if !until(d, func() bool { return strings.Contains(text(d), "no local changes") }) {
		t.Errorf("no 'no local changes' warning:%s", dump(d))
	}
	if got := r.git("stash", "list"); got != "" {
		t.Errorf("stash list %q, want empty", got)
	}
	d.Palette("git-stash-pop")
	if !until(d, func() bool { return strings.Contains(text(d), "Stash pop failed") }) {
		t.Errorf("no error for pop without a stash:%s", dump(d))
	}
}

// Spec: Given two stashes, when the user opens the stash list and picks the
// older one, then its change is applied and both stashes are kept (apply
// does not drop).
func TestSCMStashListApply(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("main.go", "package main\n// first\n")
		r.git("stash", "push", "-q", "-m", "first idea")
		r.write("README.md", "# second\n")
		r.git("stash", "push", "-q", "-m", "second idea")
	})
	d.Palette("git-stash-list")
	if !strings.Contains(text(d), "first idea") || !strings.Contains(text(d), "second idea") {
		t.Fatalf("stash list does not show both:%s", dump(d))
	}
	d.Type("first")
	d.Key("enter")
	until(d, func() bool { return inSection(d, "CHANGES", "main.go") })
	if got := r.read("main.go"); got != "package main\n// first\n" {
		t.Errorf("main.go = %q, want the first stash", got)
	}
	if got := r.read("README.md"); got != scmFiles["README.md"] {
		t.Errorf("README.md changed: %q (the other stash was applied)", got)
	}
	if n := len(strings.Split(r.git("stash", "list"), "\n")); n != 2 {
		t.Errorf("%d stashes left, want 2", n)
	}
}
