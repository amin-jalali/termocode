package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/picker"
)

const gitxIssuesJSON = `[{"number":12,"title":"Login crashes on empty password!","labels":[{"name":"bug"}]},
 {"number":9,"title":"Dark mode","labels":[]}]`

// GITX-36: Issues… lists the open issues; Start Work makes a branch
// named after the issue (the name can be changed) and switches to it.
func TestGitxIssuesStartWork(t *testing.T) {
	m, dir := covGitRepo(t)
	f := &prFakeGH{answers: map[string]string{"issue list": gitxIssuesJSON}}
	m.ciW().run = f.run
	done := covMsg[gitxDoneMsg](t, m.dispatchPaletteAction("git-issues"))
	done.apply(m)
	if !m.pickerOpen || strings.Join(gitxPickerIDs(m), ",") != "issue-12,issue-9" {
		t.Fatalf("issues %v", gitxPickerIDs(m))
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "issue-12"})
	if !m.pickerOpen || strings.Join(gitxPickerIDs(m), ",") != "start,copy" {
		t.Fatalf("actions %v", gitxPickerIDs(m))
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "start"})
	if !m.promptOpen || m.prompt.Value() != "12-login-crashes-on-empty-password" {
		t.Fatalf("branch prompt %q", m.prompt.Value())
	}
	m.promptOpen = false
	m.handlePromptSubmit("12-login-crash")
	if got := covGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "12-login-crash" {
		t.Errorf("on %s", got)
	}
	if !toastHas(m, "#12") {
		t.Error("the toast does not name the issue")
	}
	// gh missing: a quiet hint, no picker
	m2, _ := covGitRepo(t)
	m2.ciW().run = ciNoGH
	covMsg[gitxDoneMsg](t, m2.dispatchPaletteAction("git-issues")).apply(m2)
	if m2.pickerOpen {
		t.Error("a picker without gh")
	}
}

// GITX-37: Initialize Repository runs git init in a folder that is no
// repository; Publish to GitHub… asks the name and private / public,
// then asks on a Cancel-first card before gh creates and pushes.
func TestGitxInitAndPublish(t *testing.T) {
	covGitEnv(t)
	dir := t.TempDir()
	covWrite(t, dir, "a.txt", "a\n")
	m := covModelAt(t, dir)
	m.dispatchPaletteAction("git-init")
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatal("no repository made")
	}
	if got := covGit(t, dir, "symbolic-ref", "--short", "HEAD"); got != "main" {
		t.Errorf("branch %s", got)
	}
	covRefresh(m)
	m.dispatchPaletteAction("git-init")
	if !toastHas(m, "already a git repository") {
		t.Error("no note for a second init")
	}
	covCommit(t, dir, "first")
	covRefresh(m)
	f := &prFakeGH{answers: map[string]string{"repo create": "https://github.com/me/" + filepath.Base(dir)}}
	m.ciW().run = f.run
	m.dispatchPaletteAction("git-publish")
	if !m.promptOpen || m.prompt.Value() != filepath.Base(dir) {
		t.Fatalf("name prompt %q", m.prompt.Value())
	}
	m.promptOpen = false
	m.handlePromptSubmit("my-tool")
	if strings.Join(gitxPickerIDs(m), ",") != "private,public" {
		t.Fatalf("visibility %v", gitxPickerIDs(m))
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "private"})
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,publish" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card %q", gitxCardIDList(m))
	}
	m.confirmOpen = false
	if m.handleConfirmSelect("cancel") != nil || f.called("repo") {
		t.Fatal("Cancel published")
	}
	m.dispatchPaletteAction("git-publish")
	m.promptOpen = false
	m.handlePromptSubmit("my-tool")
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "private"})
	m.confirmOpen = false
	covMsg[gitxDoneMsg](t, m.handleConfirmSelect("publish")).apply(m)
	if !f.called("repo create my-tool --private --source . --remote origin --push") {
		t.Errorf("gh calls %v", f.calls)
	}
	// with an origin already: no publish
	covGit(t, dir, "remote", "add", "origin", "https://example.com/x.git")
	m.dispatchPaletteAction("git-publish")
	if m.promptOpen || !toastHas(m, "has a remote") {
		t.Error("publish offered with a remote")
	}
}
