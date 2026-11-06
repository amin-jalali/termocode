package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// gitxChecksReset clears the check settings after a test.
func gitxChecksReset(t *testing.T) {
	t.Helper()
	path, err := settingsConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })
}

// gitxCommitRun presses Commit and runs the checks it starts, if any.
func gitxCommitRun(t *testing.T, m *Model, msg string) {
	t.Helper()
	m.gitCommitMsg = msg
	cmd := m.gitInlineCommit(false, false)
	if m.gitx.checks.running {
		covMsg[gitxDoneMsg](t, cmd).apply(m)
	}
}

func gitxCommitCount(t *testing.T, dir string) int {
	t.Helper()
	return len(strings.Split(covGit(t, dir, "log", "--format=%s"), "\n"))
}

// GITX-27 (B20–B22, P7, P8): the before-commit checks are off by
// default. Turned on, a commit with a large file or a secret-looking line
// stops at a Cancel-first card that names them (never the secret); Show
// Report opens the whole report, Commit Anyway commits. A clean commit
// goes through without a card. The task named in git_commit_task runs
// first; a failing task stops the commit and its output is in the report.
func TestGitxCommitChecks(t *testing.T) {
	gitxChecksReset(t)
	m, dir := covGitRepo(t)
	for _, k := range []string{"git_commit_checks", "git_commit_format", "git_commit_task", "git_commit_large_mb"} {
		if _, ok := aiSettingPolicies[k]; !ok {
			t.Fatalf("no AI policy for %s", k)
		}
	}
	for _, id := range []string{"setting-git-commit-checks", "setting-git-commit-format", "setting-git-commit-task", "setting-git-commit-large-mb"} {
		if c := settingsCategories()[id]; c != "Git" {
			t.Fatalf("%s category %q", id, c)
		}
	}
	if v, _ := gitxSettingValue("setting-git-commit-checks"); v != "false" {
		t.Fatalf("default %q", v)
	}

	big := strings.Repeat("0123456789abcdef\n", 70000) // 1.1 MB
	covWrite(t, dir, "big.txt", big)
	covGit(t, dir, "add", "big.txt")
	covRefresh(m)
	gitxCommitRun(t, m, "off")
	if m.confirmOpen || gitxCommitCount(t, dir) != 2 {
		t.Fatalf("checks off: card %v, commits %d", m.confirmOpen, gitxCommitCount(t, dir))
	}

	m.applySetting("setting-git-commit-checks", "true")
	m.applySetting("setting-git-commit-large-mb", "1")
	if v, _ := gitxSettingValue("setting-git-commit-large-mb"); v != "1" {
		t.Fatalf("large mb %q", v)
	}
	token := "ghp_" + "abcdefghijklmnopqrst1234"
	covWrite(t, dir, "big.txt", big+"more\n")
	covWrite(t, dir, "conf.txt", "name = demo\ntoken: "+token+"\n")
	covGit(t, dir, "add", "big.txt", "conf.txt")
	covRefresh(m)
	gitxCommitRun(t, m, "with problems")
	if !m.confirmOpen || m.confirmKind != confirmKindGitx || m.confirm.SafeID() != "cancel" || gitxCardIDList(m) != "cancel,report,commit" {
		t.Fatalf("card %v kind %v ids %q", m.confirmOpen, m.confirmKind, gitxCardIDList(m))
	}
	text := m.gitx.checks.card
	for _, want := range []string{"Before-commit checks", "big.txt", "1.1 MB", "conf.txt:2", "secret"} {
		if !strings.Contains(text, want) {
			t.Fatalf("card lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, token) || strings.Contains(m.gitx.checks.report, token) {
		t.Fatal("the secret is shown")
	}
	if gitxCommitCount(t, dir) != 2 {
		t.Fatal("committed before the answer")
	}
	m.confirmOpen = false
	m.handleGitxConfirm("report")
	if !strings.Contains(m.gitx.checks.report, "conf.txt:2") || !strings.Contains(m.gitx.checks.report, "big.txt") {
		t.Fatalf("report:\n%s", m.gitx.checks.report)
	}
	if gitxCommitCount(t, dir) != 2 {
		t.Fatal("Show Report committed")
	}

	// Commit Anyway.
	gitxCommitRun(t, m, "anyway")
	m.confirmOpen = false
	m.handleGitxConfirm("commit")
	if got := covGit(t, dir, "log", "-1", "--format=%s"); got != "anyway" {
		t.Fatalf("Commit Anyway: HEAD is %q", got)
	}

	// A clean commit with a passing task: no card.
	covWrite(t, dir, ".termocode/tasks.json", `{"tasks": [
		{"label": "check ok", "command": "echo fine"},
		{"label": "check bad", "command": "echo boom-from-task; exit 3"}]}`)
	m.applySetting("setting-git-commit-task", "check ok")
	if v, _ := gitxSettingValue("setting-git-commit-task"); v != "check ok" {
		t.Fatalf("task %q", v)
	}
	covWrite(t, dir, "a.txt", "clean\n")
	covGit(t, dir, "add", "a.txt")
	covRefresh(m)
	gitxCommitRun(t, m, "clean")
	if m.confirmOpen {
		t.Fatalf("a clean commit shows a card: %s", m.gitx.checks.card)
	}
	if got := covGit(t, dir, "log", "-1", "--format=%s"); got != "clean" {
		t.Fatalf("clean commit: HEAD is %q", got)
	}

	// A failing task stops the commit.
	m.applySetting("setting-git-commit-task", "check bad")
	covWrite(t, dir, "a.txt", "clean again\n")
	covGit(t, dir, "add", "a.txt")
	covRefresh(m)
	gitxCommitRun(t, m, "bad task")
	if !m.confirmOpen || !strings.Contains(m.gitx.checks.card, "check bad") || !strings.Contains(m.gitx.checks.card, "exit 3") {
		t.Fatalf("task card %v:\n%s", m.confirmOpen, m.gitx.checks.card)
	}
	if !strings.Contains(m.gitx.checks.report, "boom-from-task") {
		t.Fatalf("report lacks the task output:\n%s", m.gitx.checks.report)
	}
	m.confirmOpen = false
	m.handleGitxConfirm("cancel")
	if got := covGit(t, dir, "log", "-1", "--format=%s"); got != "clean" {
		t.Fatalf("Cancel committed: HEAD is %q", got)
	}

	// An unknown task is a finding, not a silent pass.
	m.applySetting("setting-git-commit-task", "no such task")
	gitxCommitRun(t, m, "unknown task")
	if !m.confirmOpen || !strings.Contains(m.gitx.checks.card, "no such task") {
		t.Fatalf("unknown task card %v:\n%s", m.confirmOpen, m.gitx.checks.card)
	}

	// The palette row runs the checks without a commit.
	m.confirmOpen = false
	m.applySetting("setting-git-commit-task", "")
	if _, ok := aiActionPolicies["git-commit-checks"]; !ok {
		t.Fatal("no AI policy for git-commit-checks")
	}
	cmd := m.dispatchPaletteAction("git-commit-checks")
	covMsg[gitxDoneMsg](t, cmd).apply(m)
	if !strings.Contains(m.gitx.checks.report, "No problems found") {
		t.Fatalf("palette report:\n%s", m.gitx.checks.report)
	}
	if got := covGit(t, dir, "log", "-1", "--format=%s"); got != "clean" {
		t.Fatalf("the palette row committed: HEAD is %q", got)
	}
}

// git_commit_format formats the fully staged files and stages the
// result; a file that also has unstaged edits is left as it is.
func TestGitxCommitChecksFormat(t *testing.T) {
	gitxChecksReset(t)
	m, dir := covGitRepo(t)
	var called []string
	old := gitxFormatFile
	gitxFormatFile = func(_ *nvim.Client, abs string) (string, error) {
		called = append(called, filepath.Base(abs))
		b, _ := os.ReadFile(abs)
		return gitxFormatted, os.WriteFile(abs, []byte(strings.ToUpper(string(b))), 0o644)
	}
	t.Cleanup(func() { gitxFormatFile = old })
	m.applySetting("setting-git-commit-format", "true")

	covWrite(t, dir, "a.txt", "fmt me\n")
	covWrite(t, dir, "b.txt", "staged\n")
	covGit(t, dir, "add", "a.txt", "b.txt")
	covWrite(t, dir, "b.txt", "staged\nand not\n")
	covRefresh(m)
	gitxCommitRun(t, m, "formatted")
	if strings.Join(called, ",") != "a.txt" {
		t.Fatalf("formatter ran on %v", called)
	}
	if got := covGit(t, dir, "show", "HEAD:a.txt"); got != "FMT ME" {
		t.Fatalf("committed a.txt %q", got)
	}
	if got := covGit(t, dir, "show", "HEAD:b.txt"); got != "staged" {
		t.Fatalf("committed b.txt %q", got)
	}
	if !strings.Contains(m.gitx.checks.report, "b.txt") {
		t.Fatalf("the skipped file is not in the report:\n%s", m.gitx.checks.report)
	}
}

// The errors of the Problems list in staged files are a finding.
func TestGitxCommitChecksProblems(t *testing.T) {
	gitxChecksReset(t)
	m, dir := covGitRepo(t)
	c := aiTestNvim(t, m)
	m.applySetting("setting-git-commit-checks", "true")
	p := covWrite(t, dir, "a.txt", "broken\n")
	covGit(t, dir, "add", "a.txt")
	covRefresh(m)
	if err := c.Command("edit " + p); err != nil {
		t.Fatal(err)
	}
	if err := c.ExecLua(`vim.diagnostic.set(vim.api.nvim_create_namespace('t'), 0, {{lnum = 0, col = 0, severity = 1, message = 'undefined: broken'}})`); err != nil {
		t.Fatal(err)
	}
	gitxCommitRun(t, m, "with an error")
	if !m.confirmOpen || !strings.Contains(m.gitx.checks.card, "1 error") {
		t.Fatalf("card %v:\n%s", m.confirmOpen, m.gitx.checks.card)
	}
	if !strings.Contains(m.gitx.checks.report, "a.txt:1") || !strings.Contains(m.gitx.checks.report, "undefined: broken") {
		t.Fatalf("report:\n%s", m.gitx.checks.report)
	}
}

// gitxFailingHook adds a pre-commit hook that prints a line and fails.
func gitxFailingHook(t *testing.T, dir string) {
	t.Helper()
	hook := filepath.Join(dir, ".git", "hooks", "pre-commit")
	_ = os.MkdirAll(filepath.Dir(hook), 0o755)
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho 'lint: 2 problems in a.txt'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// GITX-27 (B23, P5): a commit a git hook stops opens a Cancel-first card
// with the hook's output (Show Hook Output opens it as a page) and Commit
// Without Hooks (--no-verify). Git: Toggle Skip Hooks on Commit skips
// them for this session; Git: Show Last Hook Output shows the output
// again.
func TestGitxHookFailure(t *testing.T) {
	gitxChecksReset(t)
	m, dir := covGitRepo(t)
	for _, id := range []string{"git-skip-hooks", "git-hook-output"} {
		if _, ok := aiActionPolicies[id]; !ok {
			t.Fatalf("no AI policy for %s", id)
		}
	}
	gitxFailingHook(t, dir)
	covWrite(t, dir, "a.txt", "changed\n")
	covGit(t, dir, "add", "a.txt")
	covRefresh(m)

	gitxCommitRun(t, m, "hooked")
	if !m.confirmOpen || m.confirmKind != confirmKindGitx || m.confirm.SafeID() != "cancel" || gitxCardIDList(m) != "cancel,output,noverify" {
		t.Fatalf("card %v ids %q", m.confirmOpen, gitxCardIDList(m))
	}
	if !strings.Contains(m.gitx.hookOutput, "lint: 2 problems in a.txt") || !strings.Contains(m.gitx.checks.card, "lint: 2 problems") {
		t.Fatalf("hook output %q card %q", m.gitx.hookOutput, m.gitx.checks.card)
	}
	if gitxCommitCount(t, dir) != 1 {
		t.Fatal("the hook did not stop the commit")
	}
	m.confirmOpen = false
	m.handleGitxConfirm("output")
	if gitxCommitCount(t, dir) != 1 {
		t.Fatal("Show Hook Output committed")
	}
	if m.dispatchPaletteAction("git-hook-output"); m.gitx.hookOutput == "" {
		t.Fatal("the last hook output is gone")
	}

	gitxCommitRun(t, m, "hooked")
	m.confirmOpen = false
	m.handleGitxConfirm("noverify")
	if got := covGit(t, dir, "log", "-1", "--format=%s"); got != "hooked" {
		t.Fatalf("Commit Without Hooks: HEAD is %q", got)
	}

	// The session toggle.
	m.dispatchPaletteAction("git-skip-hooks")
	if !m.gitx.skipHooks {
		t.Fatal("the toggle did not turn skipping on")
	}
	covWrite(t, dir, "a.txt", "changed twice\n")
	covGit(t, dir, "add", "a.txt")
	covRefresh(m)
	gitxCommitRun(t, m, "skipped")
	if m.confirmOpen {
		t.Fatal("the hooks ran while skipped")
	}
	if got := covGit(t, dir, "log", "-1", "--format=%s"); got != "skipped" {
		t.Fatalf("skip hooks: HEAD is %q", got)
	}
	m.dispatchPaletteAction("git-skip-hooks")
	if m.gitx.skipHooks {
		t.Fatal("the toggle did not turn skipping off")
	}

	// Without hooks a failure stays a toast.
	_ = os.Remove(filepath.Join(dir, ".git", "hooks", "pre-commit"))
	covRefresh(m)
	gitxCommitRun(t, m, "nothing staged")
	if m.confirmOpen {
		t.Fatal("a failure without hooks opened the hook card")
	}
}
