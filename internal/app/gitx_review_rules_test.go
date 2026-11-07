package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CMP-37: AI Review puts the project's review rules (the "Code Review
// Rules" section of AGENTS.md) into the request, above the diff; the
// chat's system prompt does not get them.
func TestGitxAIReviewUsesReviewRules(t *testing.T) {
	m, dir := covGitRepo(t)
	m.aiS().provider = &recordingAIProvider{}
	covWrite(t, dir, "AGENTS.md", "Use tabs.\n\n## Code Review Rules\nReport only P0 and P1 findings.\n")
	covCommit(t, dir, "docs: rules")
	covWrite(t, dir, "a.txt", "one\nTWO\nthree\n")
	covRefresh(m)
	m.dispatchPaletteAction("git-ai-review-changes")
	in := string(m.aiS().input)
	if !strings.Contains(in, "Report only P0 and P1 findings.") || !strings.Contains(in, "+TWO") {
		t.Fatalf("chat box %q", in)
	}
	if strings.Index(in, "P0 and P1") > strings.Index(in, "```diff") {
		t.Error("the rules are not above the diff")
	}
	if strings.Contains(in, "Use tabs.") {
		t.Error("the chat rules went into the review request")
	}
}

// CMP-37: Git: Add Review Rules to AGENTS.md asks on a Cancel-first card,
// then adds the section; a second run opens the file instead.
func TestGitxAddReviewRules(t *testing.T) {
	m, dir := covGitRepo(t)
	if _, ok := aiActionPolicies["git-ai-review-rules"]; !ok {
		t.Fatal("no AI policy")
	}
	m.dispatchPaletteAction("git-ai-review-rules")
	if !m.confirmOpen || m.confirm.SafeID() != "cancel" || gitxCardIDList(m) != "cancel,add" {
		t.Fatalf("card %q", gitxCardIDList(m))
	}
	if !strings.Contains(m.confirm.View(), "Code Review Rules") {
		t.Errorf("the card does not show the section:\n%s", m.confirm.View())
	}
	m.confirmOpen = false
	m.gitx.onConfirm(m, "cancel")
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err == nil {
		t.Fatal("Cancel wrote the file")
	}
	m.dispatchPaletteAction("git-ai-review-rules")
	m.confirmOpen = false
	m.gitx.onConfirm(m, "add")
	b, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil || !strings.Contains(string(b), "## Code Review Rules") {
		t.Fatalf("AGENTS.md %q %v", b, err)
	}
	// Existing text is kept; the section goes at the end.
	covWrite(t, dir, "AGENTS.md", "Use tabs.\n")
	m.dispatchPaletteAction("git-ai-review-rules")
	m.confirmOpen = false
	m.gitx.onConfirm(m, "add")
	b, _ = os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if !strings.HasPrefix(string(b), "Use tabs.\n\n## Code Review Rules\n") {
		t.Errorf("AGENTS.md %q", b)
	}
	// It has a section now: no card.
	m.dispatchPaletteAction("git-ai-review-rules")
	if m.confirmOpen {
		t.Error("a card for a file that has the section")
	}
}
