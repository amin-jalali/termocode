//go:build unix

package app

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
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

// GITX-39 (P6): the before-commit checks (git_commit_checks) warn about
// staged files that bring Windows line endings (CRLF) in.
func TestGitxCommitChecksCRLF(t *testing.T) {
	gitxChecksReset(t)
	m, dir := covGitRepo(t)
	covGit(t, dir, "config", "core.autocrlf", "false")
	covWrite(t, dir, "a.txt", "one\r\ntwo\r\n")
	covGit(t, dir, "add", "a.txt")
	res := gitxCheckRun{cwd: dir, cfg: gitxCheckCfg{scan: true, largeMB: 5}}.do()
	if !strings.Contains(strings.Join(res.findings, "\n"), "a.txt has Windows line endings (CRLF)") ||
		!strings.Contains(res.report, "Windows line endings (CRLF)") {
		t.Fatalf("findings %v\nreport:\n%s", res.findings, res.report)
	}
	_ = m
}

// GITX-39 (M10): Git: Show LFS Files lists the files Git LFS tracks and
// says plainly which are only pointers (not downloaded), with the size
// of the real file and how to get it.
func TestGitxLFSPage(t *testing.T) {
	page := gitxLFSPage([]git.LFSFile{{Path: "assets/big.bin", Pointer: true, Size: 5 << 20}, {Path: "here.bin", Size: 12}})
	for _, want := range []string{"2 files", "pointer only", "5.0 MB", "assets/big.bin", "downloaded", "here.bin", "git lfs pull"} {
		if !strings.Contains(page, want) {
			t.Errorf("no %q in:\n%s", want, page)
		}
	}
	if page := gitxLFSPage(nil); !strings.Contains(page, "No file") {
		t.Errorf("empty page:\n%s", page)
	}
	m, _ := covGitRepo(t)
	m.dispatchPaletteAction("git-lfs-files")
	if p := aiActionPolicies["git-lfs-files"]; p.Gate != aiGateAuto {
		t.Error("reading the LFS list is not auto for the AI")
	}
}

// GITX-39 (O26): Git: Create Gist from File… asks secret or public, then
// asks on a Cancel-first card before gh creates the gist; a file that may
// hold secrets (by name or by a secret-looking line) is refused.
func TestGitxCreateGist(t *testing.T) {
	m, dir := covGitRepo(t)
	f := &prFakeGH{answers: map[string]string{"gist create": "https://gist.github.com/me/abc123\n"}}
	m.ciW().run = f.run
	m.dispatchPaletteAction("git-gist")
	if !toastHas(m, "Open the file") {
		t.Fatal("no note without a file")
	}
	covWrite(t, dir, ".env", "TOKEN=x\n")
	m.editor.SetMeta(filepath.Join(dir, ".env"), false, "")
	m.dispatchPaletteAction("git-gist")
	if m.pickerOpen || !toastHas(m, "may hold secrets") {
		t.Fatal(".env offered for a gist")
	}
	covWrite(t, dir, "key.go", "const k = \"sk-"+"test0123456789abcdefghijklmnop\"\n")
	m.editor.SetMeta(filepath.Join(dir, "key.go"), false, "")
	m.dispatchPaletteAction("git-gist")
	if m.pickerOpen {
		t.Fatal("a file with a secret-looking line offered for a gist")
	}
	m.editor.SetMeta(filepath.Join(dir, "a.txt"), false, "")
	m.dispatchPaletteAction("git-gist")
	if !m.pickerOpen || strings.Join(gitxPickerIDs(m), ",") != "gist-secret,gist-public" {
		t.Fatalf("picker %v", gitxPickerIDs(m))
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "gist-public"})
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,gist" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card %q", gitxCardIDList(m))
	}
	m.confirmOpen = false
	if m.handleConfirmSelect("cancel") != nil || f.called("gist") {
		t.Fatal("Cancel made a gist")
	}
	m.dispatchPaletteAction("git-gist")
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "gist-public"})
	m.confirmOpen = false
	covMsg[gitxDoneMsg](t, m.handleConfirmSelect("gist")).apply(m)
	if !f.called("gist create --desc a.txt --public -- a.txt") {
		t.Errorf("gh calls %v", f.calls)
	}
	if !toastHas(m, "https://gist.github.com/me/abc123") {
		t.Error("no gist link")
	}
	if p := aiActionPolicies["git-gist"]; p.Gate != aiGateNever {
		t.Error("the AI may publish a file")
	}
}

// GITX-39 (Q4): Git: Write Changelog with AI… asks where to start (the
// latest tag first) and puts the commit subjects since then into the
// chat box; nothing is sent before Enter.
func TestGitxAIChangelog(t *testing.T) {
	m, dir := covGitRepo(t)
	gitxWithAI(m, "")
	covGit(t, dir, "tag", "v1.0")
	covWrite(t, dir, "a.txt", "x\n")
	covCommit(t, dir, "feat: add the parser")
	covWrite(t, dir, "a.txt", "y\n")
	covCommit(t, dir, "fix: empty lines")
	covRefresh(m)
	m.dispatchPaletteAction("git-ai-changelog")
	if !m.promptOpen || m.prompt.Value() != "v1.0" {
		t.Fatalf("prompt %v %q", m.promptOpen, m.prompt.Value())
	}
	m.promptOpen = false
	m.handlePromptSubmit("v1.0")
	in := string(m.aiS().input)
	if !strings.Contains(in, "changelog") || !strings.Contains(in, "v1.0") || !strings.Contains(in, "- feat: add the parser") || !strings.Contains(in, "- fix: empty lines") || strings.Contains(in, "- first") {
		t.Errorf("changelog prompt %q", in)
	}
	if m.aiS().streaming {
		t.Error("sent without Enter")
	}
	m.aiS().input = nil
	m.dispatchPaletteAction("git-ai-changelog")
	m.promptOpen = false
	m.handlePromptSubmit("no-such-ref")
	if len(m.aiS().input) != 0 || !toastHas(m, "no-such-ref") {
		t.Error("a bad start was taken")
	}
}

// GITX-39 (Q8, J14): Git: Plan Commits for My Changes with AI puts the
// changed and new files with their diff into the chat box and asks for
// small commits with messages; nothing runs or is sent before Enter.
func TestGitxAIComposeCommits(t *testing.T) {
	m, dir := covGitRepo(t)
	gitxWithAI(m, "")
	m.dispatchPaletteAction("git-ai-compose")
	if !toastHas(m, "No changes") {
		t.Fatal("no note without changes")
	}
	covWrite(t, dir, "a.txt", "ONE\ntwo\nthree\n")
	covWrite(t, dir, "b.txt", "new\n")
	covRefresh(m)
	m.dispatchPaletteAction("git-ai-compose")
	in := string(m.aiS().input)
	if !strings.Contains(in, "commits") || !strings.Contains(in, "a.txt") || !strings.Contains(in, "+ONE") || !strings.Contains(in, "b.txt") {
		t.Errorf("compose prompt %q", in)
	}
	if m.aiS().streaming {
		t.Error("sent without Enter")
	}
}

// GITX-39 (J12): Git: How to Split a Commit opens a guide that walks
// through the app's own commands: the last commit with Uncommit, an older
// one with an edit stop in Interactive Rebase, then staging the parts and
// Continue.
func TestGitxSplitCommitGuide(t *testing.T) {
	page := gitxSplitGuidePage("feat/x")
	for _, want := range []string{"Git: Uncommit Last Commit (Keep Changes)", "Git: Interactive Rebase…", "edit", "Git: Rebase in Progress (Continue / Skip / Abort)…", "Git: Commit Marked Files Only…", "feat/x"} {
		if !strings.Contains(page, want) {
			t.Errorf("no %q in:\n%s", want, page)
		}
	}
	m, _ := covGitRepo(t)
	m.dispatchPaletteAction("git-split-guide")
	if p := aiActionPolicies["git-split-guide"]; p.Gate != aiGateAuto {
		t.Error("the guide is not auto for the AI")
	}
}
