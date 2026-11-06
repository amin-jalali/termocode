package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/git"
)

// GITX-38 (B17, B18): the commit view's header has the signature badge of
// a signed commit (none for a plain one); Toggle Commit Signing turns
// commit.gpgsign on and off for this repository.
func TestGitxSigning(t *testing.T) {
	m, dir := covGitRepo(t)
	head := covGit(t, dir, "rev-parse", "HEAD")
	m.onCommitViewReady(covMsg[commitViewReadyMsg](t, m.gitOpenCommitViewCmd(head)))
	if h, _ := m.commitView.card.Header(); h.Badge != "" {
		t.Fatalf("a plain commit has a badge %q", h.Badge)
	}
	m.dispatchPaletteAction("git-signing-toggle")
	if covGit(t, dir, "config", "--local", "commit.gpgsign") != "true" || !toastHas(m, "Commit signing on") {
		t.Fatal("not on")
	}
	m.dispatchPaletteAction("git-signing-toggle")
	if covGit(t, dir, "config", "--local", "commit.gpgsign") != "false" || !toastHas(m, "Commit signing off") {
		t.Fatal("not off")
	}
	if _, ok := aiActionPolicies["git-signing-toggle"]; !ok {
		t.Error("no AI policy")
	}
}

// GITX-38: commitHeader shows the signature label as the badge.
func TestGitxCommitHeaderBadge(t *testing.T) {
	info := git.CommitInfo{Signature: "✓ Verified · ann@example.com"}
	if h := commitHeader(info); h.Badge != "✓ Verified · ann@example.com" {
		t.Fatalf("badge %q", h.Badge)
	}
}
