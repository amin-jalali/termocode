package app

import (
	"strings"
	"testing"
)

// GITX-39 (B10): Commit Empty… asks the message (the commit box text
// first) and makes a commit without changes.
func TestGitxCommitEmpty(t *testing.T) {
	m, dir := covGitRepo(t)
	m.gitCommitMsg = "ci: run the pipeline again"
	m.dispatchPaletteAction("git-commit-empty")
	if !m.promptOpen || m.prompt.Value() != "ci: run the pipeline again" {
		t.Fatalf("prompt %v %q", m.promptOpen, m.prompt.Value())
	}
	m.promptOpen = false
	m.handlePromptSubmit("ci: run the pipeline again")
	if got := covGit(t, dir, "log", "-1", "--format=%s"); got != "ci: run the pipeline again" {
		t.Fatalf("subject %q", got)
	}
	if covGit(t, dir, "rev-list", "--count", "HEAD") != "2" || covGit(t, dir, "diff", "HEAD~1", "--stat") != "" {
		t.Error("not one empty commit")
	}
	if m.gitCommitMsg != "" {
		t.Error("the commit box keeps the used message")
	}
	m.dispatchPaletteAction("git-commit-empty")
	m.promptOpen = false
	m.handlePromptSubmit("  ")
	if covGit(t, dir, "rev-list", "--count", "HEAD") != "2" {
		t.Error("an empty message made a commit")
	}
	if p, ok := aiActionPolicies["git-commit-empty"]; !ok || p.Gate == aiGateAuto {
		t.Error("the AI may make commits")
	}
}

// GITX-39 (B19): Commit Staged as Another Author… asks the author (the
// own identity first), refuses a bad one, then asks the message and
// commits what is staged with that author; the committer stays the user.
func TestGitxCommitAsAuthor(t *testing.T) {
	m, dir := covGitRepo(t)
	m.dispatchPaletteAction("git-commit-author")
	if !toastHas(m, "Nothing is staged") || m.promptOpen {
		t.Fatal("no note with nothing staged")
	}
	covWrite(t, dir, "a.txt", "changed\n")
	covGit(t, dir, "add", "a.txt")
	covRefresh(m)
	m.dispatchPaletteAction("git-commit-author")
	if !m.promptOpen || m.prompt.Value() != "Tess <tess@example.com>" {
		t.Fatalf("author prompt %q", m.prompt.Value())
	}
	m.promptOpen = false
	m.handlePromptSubmit("Ann")
	if m.promptOpen || !toastHas(m, "Name <e-mail>") {
		t.Fatal("a bad author was taken")
	}
	m.dispatchPaletteAction("git-commit-author")
	m.promptOpen = false
	m.handlePromptSubmit("Ann Lee <ann@example.com>")
	if !m.promptOpen {
		t.Fatal("no message prompt")
	}
	m.promptOpen = false
	m.handlePromptSubmit("fix: from Ann's patch")
	if got := covGit(t, dir, "log", "-1", "--format=%an <%ae>|%cn|%s"); got != "Ann Lee <ann@example.com>|Tess|fix: from Ann's patch" {
		t.Fatalf("commit %q", got)
	}
	if !strings.Contains(covGit(t, dir, "show", "HEAD:a.txt"), "changed") {
		t.Error("the staged change is not in the commit")
	}
}
