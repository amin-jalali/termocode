package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/picker"
)

// GITX-30: Add to .gitignore offers the file, its extension and its
// folder; a tracked file gets a Cancel-first card that can stop
// tracking it (the file stays).
func TestGitxAddToGitignore(t *testing.T) {
	m, dir := covGitRepo(t)
	covWrite(t, dir, "logs/run.log", "x\n")
	m.editor.SetMeta(filepath.Join(dir, "logs/run.log"), false, "")
	m.dispatchPaletteAction("git-ignore-file")
	if !m.pickerOpen || strings.Join(gitxPickerIDs(m), ",") != "ignore-file,ignore-ext,ignore-dir" {
		t.Fatalf("picker %v", gitxPickerIDs(m))
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "ignore-ext"})
	if b, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); string(b) != "*.log\n" {
		t.Errorf(".gitignore %q", b)
	}
	// a tracked file
	m.editor.SetMeta(filepath.Join(dir, "a.txt"), false, "")
	m.dispatchPaletteAction("git-ignore-file")
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "ignore-file"})
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,ignore,untrack" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card %q", gitxCardIDList(m))
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if b, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); string(b) != "*.log\n" {
		t.Fatalf("Cancel changed .gitignore: %q", b)
	}
	m.dispatchPaletteAction("git-ignore-file")
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "ignore-file"})
	m.confirmOpen = false
	m.handleConfirmSelect("untrack")
	if b, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); string(b) != "*.log\n/a.txt\n" {
		t.Errorf(".gitignore %q", b)
	}
	if covGit(t, dir, "ls-files", "a.txt") != "" {
		t.Error("a.txt still tracked")
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Error("a.txt deleted")
	}
}

// GITX-30: Set Identity… asks the name, the e-mail, then where (this
// repository first).
func TestGitxSetIdentity(t *testing.T) {
	m, dir := covGitRepo(t)
	m.dispatchPaletteAction("git-identity")
	if !m.promptOpen || m.prompt.Value() != "Tess" {
		t.Fatalf("name prompt %q", m.prompt.Value())
	}
	m.promptOpen = false
	m.handlePromptSubmit("Ann Lee")
	if !m.promptOpen || m.prompt.Value() != "tess@example.com" {
		t.Fatalf("email prompt %q", m.prompt.Value())
	}
	m.promptOpen = false
	m.handlePromptSubmit("ann@example.com")
	if !m.pickerOpen || strings.Join(gitxPickerIDs(m), ",") != "id-repo,id-global" {
		t.Fatalf("where %v", gitxPickerIDs(m))
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "id-repo"})
	if covGit(t, dir, "config", "--local", "user.name") != "Ann Lee" || covGit(t, dir, "config", "--local", "user.email") != "ann@example.com" {
		t.Error("identity not set")
	}
}
