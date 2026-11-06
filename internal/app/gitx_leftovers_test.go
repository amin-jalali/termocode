package app

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/git"
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

// GITX-39 (B8): with rows marked in Source Control, c (or Git: Commit
// Marked Files Only…) commits only the marked files, new ones too; the
// rest stays as it was, staged or not.
func TestGitxCommitMarked(t *testing.T) {
	m, dir := gitSelRepo(t)
	covGit(t, dir, "add", "c.txt")
	covRefresh(m)
	m.dispatchPaletteAction("git-commit-marked")
	if m.promptOpen || !toastHas(m, "Mark the files") {
		t.Fatal("no note without marks")
	}
	gitSelCursor(t, m, gitSecChanges, "a.txt")
	covKey(m, tea.KeyMsg{Type: tea.KeySpace})
	gitSelCursor(t, m, gitSecChanges, "b.txt")
	covKey(m, tea.KeyMsg{Type: tea.KeySpace})
	if v := ansi.Strip(m.renderGitSidebar(80, 40)); !strings.Contains(v, "c commit") {
		t.Errorf("the marks bar does not offer c commit:\n%s", v)
	}
	m.gitCommitMsg = "feat: a and b"
	covKey(m, covRunes("c"))
	if !m.promptOpen || m.prompt.Value() != "feat: a and b" {
		t.Fatalf("prompt %v %q", m.promptOpen, m.prompt.Value())
	}
	m.promptOpen = false
	m.handlePromptSubmit("feat: a and b")
	if got := covGit(t, dir, "show", "--name-only", "--format=%s", "HEAD"); got != "feat: a and b\n\na.txt\nb.txt" {
		t.Fatalf("commit:\n%s", got)
	}
	if got := covGit(t, dir, "status", "--porcelain"); got != "A  c.txt" {
		t.Errorf("status %q", got)
	}
	if m.gitSelCount() != 0 || m.gitCommitMsg != "" {
		t.Error("marks or the commit box text stay")
	}
	if p := aiActionPolicies["git-commit-marked"]; p.Gate != aiGateNever {
		t.Error("the AI may commit")
	}
}

// GITX-39 (E28): Git: Show Contributors lists the authors with their
// commit counts and share, most commits first.
func TestGitxContributorsPage(t *testing.T) {
	page := gitxContributorsPage([]git.Contributor{
		{Name: "Ann", Email: "ann@example.com", Commits: 3},
		{Name: "Bo", Email: "bo@example.com", Commits: 1},
	})
	for _, want := range []string{"2 contributors, 4 commits", "3  75%  Ann <ann@example.com>", "1  25%  Bo <bo@example.com>"} {
		if !strings.Contains(page, want) {
			t.Errorf("no %q in:\n%s", want, page)
		}
	}
	m, _ := covGitRepo(t)
	m.dispatchPaletteAction("git-contributors")
	if p := aiActionPolicies["git-contributors"]; p.Gate != aiGateAuto {
		t.Error("reading the contributors is not auto for the AI")
	}
}

// GITX-39 (M11): Git: Clone Repository (Shallow)… is the clone flow with
// only the last commit; Git: Fetch Full History (Unshallow) fetches the
// rest of a shallow repository and says so for a full one.
func TestGitxShallowAndUnshallow(t *testing.T) {
	m, dir := covGitRepo(t)
	covWrite(t, dir, "a.txt", "two\n")
	covCommit(t, dir, "second")
	m.dispatchPaletteAction("git-clone-shallow")
	if !m.promptOpen || m.promptKind != promptKindCloneURL || m.clone.depth != 1 {
		t.Fatalf("shallow clone prompt: open %v depth %d", m.promptOpen, m.clone.depth)
	}
	m.promptOpen = false
	m.dispatchPaletteAction("git-clone")
	if m.clone.depth != 0 {
		t.Error("the plain clone stays shallow")
	}
	m.promptOpen = false

	m.dispatchPaletteAction("git-unshallow")
	if !toastHas(m, "full history") {
		t.Fatal("no note for a full repository")
	}
	dest := filepath.Join(t.TempDir(), "clone")
	covGit(t, dir, "clone", "-q", "--depth", "1", "file://"+filepath.ToSlash(dir), dest)
	m2 := covModelAt(t, dest)
	done := covMsg[gitRemoteDoneMsg](t, m2.dispatchPaletteAction("git-unshallow"))
	m2.gitRemoteDone(done)
	if done.err != nil || covGit(t, dest, "rev-list", "--count", "HEAD") != "2" {
		t.Fatalf("unshallow: %v, %s commits", done.err, covGit(t, dest, "rev-list", "--count", "HEAD"))
	}
}
