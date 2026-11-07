//go:build unix

package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// SCN-23: Source Control in the hard git cases a real user meets: a
// folder with no git, a held .git/index.lock, submodules, a rebase that
// is still in progress after a restart, and a branch switch with open
// tabs (one unsaved). The large-repo case is TestScaleSCM50kChanges and
// TestSCMGraphBigHistoryFast. The language server refresh after a switch
// needs a fake language server (SCN-15) and is not covered here.

// Spec: Given a folder that is not a git repository, when the user opens
// Source Control, then it says "Not a git repository" with how to start
// one, and Git: Stage All Changes does not crash or make a repository.
func TestSCN23NoGitFolder(t *testing.T) {
	d := New(t, Options{W: 140, H: 44, Files: scmFiles})
	d.Action("view:git")
	if !until(d, func() bool { return strings.Contains(text(d), "Not a git repository") }) {
		t.Fatalf("no empty state outside git:%s", dump(d))
	}
	if !strings.Contains(text(d), "git init") {
		t.Errorf("the empty state does not say how to start:%s", dump(d))
	}
	d.Palette("git-stage-all")
	d.Settle(0)
	if _, err := os.Stat(filepath.Join(d.Dir, ".git")); err == nil {
		t.Errorf("a palette action made a git repository")
	}
	if !strings.Contains(text(d), "Not a git repository") {
		t.Errorf("Source Control changed after Stage All:%s", dump(d))
	}
}

// Spec: Given a modified file and a .git/index.lock held by another git
// command, when the user stages all, then a toast says another git
// command is using the repository, CHANGES still lists the file (status
// reads take no lock) and nothing is staged; when the lock is gone,
// staging again works.
func TestSCN23IndexLockHeld(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("main.go", "package main\n// edit\n")
	})
	d.WaitForText("main.go", 3*time.Second)
	lock := filepath.Join(r.dir, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	d.Palette("git-stage-all")
	if !until(d, func() bool { return strings.Contains(text(d), "Another git command") }) {
		t.Fatalf("no toast for the held index.lock:%s", dump(d))
	}
	if got := r.porcelain(); got != " M main.go" {
		t.Errorf("git status %q, want the file still unstaged", got)
	}
	if !inSection(d, "CHANGES", "main.go") {
		t.Errorf("CHANGES lost main.go while the lock was held:%s", dump(d))
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	d.Palette("git-stage-all")
	if !until(d, func() bool { return inSection(d, "CHANGES", "No changes") }) {
		t.Fatalf("stage all did not work after the lock was gone:%s", dump(d))
	}
	if got := r.porcelain(); got != "M  main.go" {
		t.Errorf("git status %q, want main.go staged", got)
	}
}

// Spec: Given a repository with a submodule, when the user opens Source
// Control, then SUBMODULES lists it; Git: Update Submodules… asks on a
// Cancel-first card and Enter (Cancel) changes nothing.
func TestSCN23Submodules(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		lib := filepath.Join(looseTempDir(r.t), "lib")
		l := newRepo(r.t, lib)
		l.write("lib.txt", "1\n")
		l.commit("lib one")
		r.git("-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "libs/lib")
		r.commit("add lib")
	})
	if !until(d, func() bool { return rowWith(d, "SUBMODULES") >= 0 && rowWith(d, "libs/lib") >= 0 }) {
		t.Fatalf("no SUBMODULES section with libs/lib:%s", dump(d))
	}
	head := r.git("rev-parse", "HEAD")
	d.Palette("git-submodule-update")
	if !until(d, func() bool { return strings.Contains(text(d), "Update Submodules?") }) {
		t.Fatalf("no Update Submodules card:%s", dump(d))
	}
	d.Key("enter") // Cancel has the focus
	if !until(d, func() bool { return !strings.Contains(text(d), "Update Submodules?") }) {
		t.Fatalf("Enter did not close the card:%s", dump(d))
	}
	if got := r.git("rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD moved: %s, want %s", got, head)
	}
	if got := r.porcelain(); got != "" {
		t.Errorf("Cancel changed the tree: %q", got)
	}
}

// Spec: Given a rebase stopped on a conflict, when the user quits and
// starts termocode again, then Source Control still shows the rebase
// banner and the file in CONFLICTS, and the banner's Abort finishes it
// (the branch is back where it was).
func TestSCN23RebaseInProgressAfterRestart(t *testing.T) {
	d, r := conflictRebase(t)
	head := r.git("rev-parse", "ORIG_HEAD")
	d.Restart()
	d.Action("view:git")
	if !until(d, func() bool { return rowWith(d, "Rebasing 1/2") >= 0 && inSection(d, "CONFLICTS", "f.txt") }) {
		t.Fatalf("no rebase banner or CONFLICTS after restart:%s", dump(d))
	}
	openBanner(t, d)
	clickTarget(d, "confirm:rebase-abort")
	if !until(d, func() bool { return strings.Contains(text(d), "Rebase aborted") }) {
		t.Fatalf("Abort did not abort after restart:%s", dump(d))
	}
	if got := r.git("rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD %s, want %s", got, head)
	}
	if r.exists(".git/rebase-merge") {
		t.Errorf("the rebase is still in progress")
	}
}

// Spec: Given main.go open (clean) and README.md open with unsaved text,
// when the user switches to a branch where main.go differs, then the
// main.go tab shows the new branch's text, and README.md keeps its
// unsaved text (nothing on disk was overwritten).
func TestSCN23BranchSwitchWithDirtyTabs(t *testing.T) {
	d, r := scmNvimDriver(t, func(r *repo) {
		r.git("checkout", "-q", "-b", "dev")
		r.write("main.go", "package main\n\n// dev\nfunc main() {}\n")
		r.commit("feat: dev")
		r.git("checkout", "-q", "main")
	})
	open(d, "main.go")
	open(d, "README.md")
	d.Type("X")
	if !waitTabs(d, "main.go", "README.md*") {
		got, _ := tabs(d)
		t.Fatalf("README.md not modified: %v", got)
	}
	d.Palette("git-branch")
	d.Type("dev")
	d.Key("enter")
	if !until(d, func() bool { return strings.Contains(statusBar(d), "dev") }) {
		t.Fatalf("branch did not switch:%s", dump(d))
	}
	if got := r.git("branch", "--show-current"); got != "dev" {
		t.Fatalf("current branch %q, want dev", got)
	}
	if !eventually(d, func() bool { return strings.Contains(bufText(d, "main.go"), "// dev") }) {
		t.Errorf("main.go tab did not reload the dev text: %q", bufText(d, "main.go"))
	}
	if got := bufText(d, "README.md"); !strings.HasPrefix(got, "X# Demo") {
		t.Errorf("unsaved text lost: %q", got)
	}
	if got := r.read("README.md"); got != "# Demo\n" {
		t.Errorf("README.md on disk changed: %q", got)
	}
	if got, _ := tabs(d); len(got) != 2 || got[1] != "README.md*" {
		t.Errorf("tabs after switch %v, want README.md still unsaved", got)
	}
}
