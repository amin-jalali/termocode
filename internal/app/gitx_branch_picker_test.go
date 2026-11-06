package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/picker"
)

// branchPickerModel: main and feat/a (tracks main, one ahead), feat/b,
// fix-c, and a remote "origin" with only-there; fix-c was checked out
// last before main.
func branchPickerModel(t *testing.T) (*Model, string) {
	t.Helper()
	m, dir := covGitRepo(t)
	up := t.TempDir()
	covGit(t, up, "init", "-q", "-b", "main")
	covWrite(t, up, "u.txt", "u\n")
	covCommit(t, up, "up")
	covGit(t, up, "branch", "only-there")
	covGit(t, dir, "remote", "add", "origin", up)
	covGit(t, dir, "fetch", "-q", "origin")
	covGit(t, dir, "checkout", "-q", "-b", "feat/a")
	covGit(t, dir, "branch", "-q", "--set-upstream-to=main")
	covWrite(t, dir, "f.txt", "f\n")
	covCommit(t, dir, "feat a")
	covGit(t, dir, "branch", "feat/b", "main")
	covGit(t, dir, "branch", "fix-c", "main")
	covGit(t, dir, "checkout", "-q", "fix-c")
	covGit(t, dir, "checkout", "-q", "main")
	covRefresh(m)
	return m, dir
}

func branchPickerRows(m *Model) []picker.Item { return m.gitx.items }

// GITX-32: the picker lists recent branches first, then the rest grouped
// by prefix (feat/), then remote branches that have no local twin; each
// row says current and how far it is ahead / behind its upstream.
func TestGitxBranchPickerLayout(t *testing.T) {
	m, _ := branchPickerModel(t)
	m.dispatchPaletteAction("git-branch")
	if !m.pickerOpen || m.pickerKind != pickerKindBranches {
		t.Fatal("no branch picker")
	}
	var order []string
	hint := map[string]string{}
	for _, it := range branchPickerRows(m) {
		if it.Header {
			order = append(order, "#"+it.Title)
			continue
		}
		order = append(order, it.ID)
		hint[it.ID] = it.Hint
	}
	got := strings.Join(order, " ")
	for _, want := range []string{
		"new #Recent co-main co-fix-c co-feat/a",
		"#feat/ co-feat/b",
		"#Remote tr-origin/only-there",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rows %q miss %q", got, want)
		}
	}
	if strings.Contains(got, "tr-origin/main") {
		t.Error("origin/main has a local twin and should not be listed")
	}
	if !strings.Contains(hint["co-main"], "current") || !strings.Contains(hint["co-feat/a"], "↑1") {
		t.Errorf("hints %v", hint)
	}
	if !strings.Contains(hint["tr-origin/only-there"], "track") {
		t.Errorf("remote hint %q", hint["tr-origin/only-there"])
	}
}

// Picking a remote branch makes a local branch that tracks it.
func TestGitxBranchPickerTracking(t *testing.T) {
	m, dir := branchPickerModel(t)
	m.dispatchPaletteAction("git-branch")
	m.handlePickerSelect(picker.SelectMsg{ID: "tr-origin/only-there"})
	if covGit(t, dir, "branch", "--show-current") != "only-there" || !toastHas(m, "Switched to") {
		t.Fatal("no tracking branch")
	}
	if covGit(t, dir, "rev-parse", "--abbrev-ref", "@{u}") != "origin/only-there" {
		t.Fatal("not tracking origin/only-there")
	}
}

// A switch blocked by local changes asks on a Cancel-first card; Cancel
// changes nothing, Stash and Switch saves the changes and switches.
func TestGitxBranchPickerStashAndSwitch(t *testing.T) {
	m, dir := branchPickerModel(t)
	covWrite(t, dir, "f.txt", "mine\n") // f.txt is only on feat/a
	m.dispatchPaletteAction("git-branch")
	m.handlePickerSelect(picker.SelectMsg{ID: "co-feat/a"})
	if !m.confirmOpen || m.confirm.SafeID() != "cancel" || !strings.Contains(stripANSI(m.confirm.View()), "Stash and Switch") {
		t.Fatalf("no stash card (open %v)", m.confirmOpen)
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if covGit(t, dir, "branch", "--show-current") != "main" || covGit(t, dir, "stash", "list") != "" {
		t.Fatal("Cancel changed something")
	}
	m.handlePickerSelect(picker.SelectMsg{ID: "co-feat/a"})
	m.confirmOpen = false
	m.handleConfirmSelect("stash-switch")
	if covGit(t, dir, "branch", "--show-current") != "feat/a" || !toastHas(m, "stash") {
		t.Fatal("Stash and Switch did not switch")
	}
	if !strings.Contains(covGit(t, dir, "stash", "list"), "before switching to feat/a") {
		t.Fatal("the changes are not in the stash")
	}
}
