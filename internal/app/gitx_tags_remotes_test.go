package app

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/picker"
)

func gitxCardIDs(m *Model) string {
	var ids []string
	for _, b := range m.confirm.Buttons() {
		ids = append(ids, b.ID)
	}
	return strings.Join(ids, ",")
}

// gitxTry runs git in dir and returns its error (no test failure).
func gitxTry(dir string, args ...string) (string, error) {
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.Output()
	return strings.TrimSpace(string(out)), err
}

// GITX-11 (G2): Create Tag… asks the name, then the message (empty: a
// lightweight tag); the GRAPH menu tags the row's commit.
func TestGitxCreateTag(t *testing.T) {
	m, dir := covGitRepo(t)
	m.dispatchPaletteAction("git-tag-create")
	if !m.promptOpen || m.promptKind != promptKindGitTagName {
		t.Fatal("no name prompt")
	}
	m.promptOpen = false
	m.handlePromptSubmit("v1")
	if !m.promptOpen || m.promptKind != promptKindGitTagMessage {
		t.Fatal("no message prompt")
	}
	m.promptOpen = false
	m.handlePromptSubmit("")
	if got := covGit(t, dir, "cat-file", "-t", "v1"); got != "commit" || !toastHas(m, "Created tag v1") {
		t.Errorf("v1 is %q", got)
	}
	m.dispatchPaletteAction("git-tag-create")
	m.promptOpen = false
	m.handlePromptSubmit("v2")
	m.promptOpen = false
	m.handlePromptSubmit("Release two")
	if got := covGit(t, dir, "cat-file", "-t", "v2"); got != "tag" {
		t.Errorf("v2 is %q (want an annotated tag)", got)
	}
	var ids []string
	for _, it := range gitCommitMenuItems() {
		ids = append(ids, it.ID)
	}
	if !strings.Contains(strings.Join(ids, ","), gitMenuTagHere) {
		t.Errorf("no Create Tag Here… in the GRAPH menu: %v", ids)
	}
}

// GITX-11 (G3): Delete Tag… asks on a Cancel-first card; Cancel keeps
// the tag, Delete names the hash that brings it back.
func TestGitxDeleteTag(t *testing.T) {
	m, dir := covGitRepo(t)
	covGit(t, dir, "tag", "v1")
	tip := covGit(t, dir, "rev-parse", "--short", "v1")
	m.dispatchPaletteAction("git-tag-delete")
	if !m.pickerOpen || m.pickerKind != pickerKindGitTagDelete {
		t.Fatal("no picker")
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "tag-v1"})
	if !m.confirmOpen || m.confirmKind != confirmKindGitTagDelete || gitxCardIDs(m) != "cancel,delete" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card %q", gitxCardIDs(m))
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if covGit(t, dir, "tag") != "v1" {
		t.Fatal("Cancel deleted the tag")
	}
	m.pickerKind = pickerKindGitTagDelete
	m.handlePickerSelect(picker.SelectMsg{ID: "tag-v1"})
	m.confirmOpen = false
	m.handleConfirmSelect("delete")
	if covGit(t, dir, "tag") != "" || !toastHas(m, tip) {
		t.Error("not deleted, or the toast does not name the tip")
	}
}

// GITX-11 (G4, H12): Push Tag… pushes one tag or all of them.
func TestGitxPushTags(t *testing.T) {
	m, dir, bare := gitxRemoteRepo(t)
	covGit(t, dir, "tag", "v1")
	covGit(t, dir, "tag", "v2")
	m.dispatchPaletteAction("git-tag-push")
	if !m.pickerOpen || m.pickerKind != pickerKindGitTagPush {
		t.Fatal("no picker")
	}
	m.pickerOpen = false
	m.gitRemoteDone(covMsg[gitRemoteDoneMsg](t, m.handlePickerSelect(picker.SelectMsg{ID: "tag-v1"})))
	if got := covGit(t, bare, "tag"); got != "v1" {
		t.Fatalf("remote tags %q", got)
	}
	m.gitRemoteDone(covMsg[gitRemoteDoneMsg](t, m.handlePickerSelect(picker.SelectMsg{ID: "tags-all"})))
	if got := covGit(t, bare, "tag"); got != "v1\nv2" {
		t.Errorf("remote tags %q", got)
	}
}

// GITX-11 (G5): Delete Remote Tag… asks on a Cancel-first card, then for
// the typed name; a wrong name deletes nothing.
func TestGitxDeleteRemoteTag(t *testing.T) {
	m, dir, bare := gitxRemoteRepo(t)
	covGit(t, dir, "tag", "v1")
	covGit(t, dir, "push", "-q", "origin", "v1")
	m.dispatchPaletteAction("git-tag-delete-remote")
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "tag-v1"})
	if !m.confirmOpen || m.confirmKind != confirmKindGitTagDeleteRemote || gitxCardIDs(m) != "cancel,delete-remote" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card %q", gitxCardIDs(m))
	}
	m.confirmOpen = false
	m.handleConfirmSelect("delete-remote")
	if !m.promptOpen || m.promptKind != promptKindGitTagDeleteTyped || m.prompt.Value() != "" {
		t.Fatal("no typed prompt")
	}
	m.promptOpen = false
	m.handlePromptSubmit("v2")
	if covGit(t, bare, "tag") != "v1" || !toastHas(m, "did not match") {
		t.Fatal("deleted on a wrong name")
	}
	m.confirmOpen = false
	m.handleConfirmSelect("delete-remote")
	m.promptOpen = false
	m.gitRemoteDone(covMsg[gitRemoteDoneMsg](t, m.handlePromptSubmit("v1")))
	if covGit(t, bare, "tag") != "" || covGit(t, dir, "tag") != "v1" {
		t.Error("remote tag not deleted, or the local one went too")
	}
}

// GITX-11 (G6): Show Tag Details… names tagger, date, message and
// commit.
func TestGitxTagDetails(t *testing.T) {
	m, dir := covGitRepo(t)
	covGit(t, dir, "tag", "-a", "-m", "Release one", "v1")
	m.dispatchPaletteAction("git-tag-details")
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "tag-v1"})
	page := m.gitRef.page
	for _, want := range []string{"v1", "annotated", "Tess", "Release one", "first"} {
		if !strings.Contains(page, want) {
			t.Errorf("page has no %q:\n%s", want, page)
		}
	}
}

// GITX-13: Manage Remotes… lists the remotes with their URL and an Add
// row; a remote's actions rename it, change its URL, or remove it after
// a Cancel-first card.
func TestGitxManageRemotes(t *testing.T) {
	m, dir, bare := gitxRemoteRepo(t)
	m.dispatchPaletteAction("git-remotes")
	if !m.pickerOpen || m.pickerKind != pickerKindGitRemotes {
		t.Fatal("no picker")
	}
	if v := ansi.Strip(m.picker.View()); !strings.Contains(v, "origin") || !strings.Contains(v, "Add Remote") {
		t.Errorf("picker:\n%s", v)
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "remote-add"})
	m.promptOpen = false
	m.handlePromptSubmit("fork")
	if !m.promptOpen || m.promptKind != promptKindGitRemoteAddURL {
		t.Fatal("no URL prompt")
	}
	m.promptOpen = false
	m.handlePromptSubmit(bare)
	if got := covGit(t, dir, "remote"); got != "fork\norigin" {
		t.Fatalf("remotes %q", got)
	}
	m.pickerKind = pickerKindGitRemotes
	m.handlePickerSelect(picker.SelectMsg{ID: "remote-fork"})
	if !m.pickerOpen || m.pickerKind != pickerKindGitRemoteAction {
		t.Fatal("no action picker")
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "remote-rename"})
	if m.prompt.Value() != "fork" {
		t.Errorf("rename prompt %q", m.prompt.Value())
	}
	m.promptOpen = false
	m.handlePromptSubmit("mine")
	m.pickerKind = pickerKindGitRemoteAction
	m.handlePickerSelect(picker.SelectMsg{ID: "remote-url"})
	m.promptOpen = false
	m.handlePromptSubmit("https://example.com/x.git")
	if got := covGit(t, dir, "remote", "get-url", "mine"); got != "https://example.com/x.git" {
		t.Errorf("url %q", got)
	}
	m.pickerKind = pickerKindGitRemoteAction
	m.handlePickerSelect(picker.SelectMsg{ID: "remote-remove"})
	if !m.confirmOpen || m.confirmKind != confirmKindGitRemoteRemove || gitxCardIDs(m) != "cancel,remove" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card %q", gitxCardIDs(m))
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if !strings.Contains(covGit(t, dir, "remote"), "mine") {
		t.Fatal("Cancel removed the remote")
	}
	m.pickerKind = pickerKindGitRemoteAction
	m.handlePickerSelect(picker.SelectMsg{ID: "remote-remove"})
	m.confirmOpen = false
	m.handleConfirmSelect("remove")
	if got := covGit(t, dir, "remote"); got != "origin" {
		t.Errorf("remotes %q", got)
	}
}

// GITX-13: Set Upstream Branch… picks a remote branch; Unset Upstream
// stops tracking it.
func TestGitxUpstream(t *testing.T) {
	m, dir, _ := gitxRemoteRepo(t)
	m.dispatchPaletteAction("git-upstream-unset")
	if _, err := gitxTry(dir, "rev-parse", "--abbrev-ref", "main@{upstream}"); err == nil {
		t.Fatal("upstream still set")
	}
	m.dispatchPaletteAction("git-upstream-set")
	if !m.pickerOpen || m.pickerKind != pickerKindGitUpstream {
		t.Fatal("no picker")
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "up-origin/main"})
	if got := covGit(t, dir, "rev-parse", "--abbrev-ref", "main@{upstream}"); got != "origin/main" {
		t.Errorf("upstream %q", got)
	}
}

// GITX-14: Force Push (with Lease)… asks on a Cancel-first card, then for
// the typed branch name, and replaces the remote branch.
func TestGitxForcePushWithLease(t *testing.T) {
	m, dir, bare := gitxRemoteRepo(t)
	covGit(t, dir, "commit", "-q", "--amend", "-m", "first, reworded")
	covRefresh(m)
	m.dispatchPaletteAction("git-push-force")
	if !m.confirmOpen || m.confirmKind != confirmKindGitForcePush || gitxCardIDs(m) != "cancel,force" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card %q", gitxCardIDs(m))
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if covGit(t, bare, "log", "-1", "--format=%s", "main") != "first" {
		t.Fatal("Cancel pushed")
	}
	m.dispatchPaletteAction("git-push-force")
	m.confirmOpen = false
	m.handleConfirmSelect("force")
	if !m.promptOpen || m.promptKind != promptKindGitForcePushTyped {
		t.Fatal("no typed prompt")
	}
	m.promptOpen = false
	m.handlePromptSubmit("mian")
	if covGit(t, bare, "log", "-1", "--format=%s", "main") != "first" || !toastHas(m, "did not match") {
		t.Fatal("pushed on a wrong name")
	}
	m.dispatchPaletteAction("git-push-force")
	m.confirmOpen = false
	m.handleConfirmSelect("force")
	m.promptOpen = false
	m.gitRemoteDone(covMsg[gitRemoteDoneMsg](t, m.handlePromptSubmit("main")))
	if got := covGit(t, bare, "log", "-1", "--format=%s", "main"); got != "first, reworded" {
		t.Errorf("remote main %q", got)
	}
}
