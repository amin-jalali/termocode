package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/prreview"
)

// CMP-36: Git: Set Up PR Review Bot shows the workflow on a Cancel-first
// card; Cancel writes nothing, Add Workflow writes it once, and an
// existing file is never replaced.
func TestGitxReviewBotSetUp(t *testing.T) {
	m, dir := covGitRepo(t)
	covRefresh(m)
	if _, ok := aiActionPolicies["git-pr-review-bot"]; !ok {
		t.Fatal("no AI policy")
	}
	path := filepath.Join(dir, filepath.FromSlash(prreview.WorkflowPath))

	m.dispatchPaletteAction("git-pr-review-bot")
	if !m.confirmOpen || m.confirmKind != confirmKindGitx || m.confirm.SafeID() != "cancel" || gitxCardIDList(m) != "cancel,add" {
		t.Fatalf("card open %v kind %v ids %q", m.confirmOpen, m.confirmKind, gitxCardIDList(m))
	}
	if !strings.Contains(m.confirm.View(), "termocode-review.yml") {
		t.Errorf("the card does not name the file:\n%s", m.confirm.View())
	}
	m.handleGitxConfirm("cancel")
	if _, err := os.Stat(path); err == nil {
		t.Fatal("Cancel wrote the workflow")
	}

	m.dispatchPaletteAction("git-pr-review-bot")
	m.handleGitxConfirm("add")
	b, err := os.ReadFile(path)
	if err != nil || string(b) != prreview.Workflow {
		t.Fatalf("workflow not written: %v", err)
	}
	if !toastHas(m, "Added") {
		t.Error("no toast")
	}

	if err := os.WriteFile(path, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.confirmOpen = false
	m.dispatchPaletteAction("git-pr-review-bot")
	if m.confirmOpen || !toastHas(m, "already set up") {
		t.Error("an existing workflow opened the card")
	}
	if b, _ := os.ReadFile(path); string(b) != "mine\n" {
		t.Error("the existing workflow was replaced")
	}
	// Made between the card and the button: still kept.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	m.dispatchPaletteAction("git-pr-review-bot")
	if err := os.WriteFile(path, []byte("late\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.handleGitxConfirm("add")
	if b, _ := os.ReadFile(path); string(b) != "late\n" {
		t.Error("a file made meanwhile was replaced")
	}
}
