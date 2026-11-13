package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSigLabelsAndSignReason(t *testing.T) {
	for st, want := range map[byte]string{
		'G': "✓ Verified · me",
		'U': "✓ Signed, key not trusted · me",
		'B': "✗ Bad signature · me",
		'X': "Signed, expired · me",
		'Y': "Signed, expired · me",
		'R': "✗ Signed, key revoked · me",
	} {
		if got := (Sig{Signed: true, Status: st, Signer: "me"}).Label(); got != want {
			t.Errorf("Label(%c) = %q, want %q", st, got, want)
		}
	}
	if got := (Sig{Signed: true, Status: 'B'}).Label(); got != "✗ Bad signature" {
		t.Errorf("Label without a signer = %q", got)
	}
	got := signReason("hint\ngpg: skipped \"x\": No secret key\nmore")
	if !strings.Contains(got, "(gpg: skipped") || !strings.HasPrefix(got, "Signing the commit failed") {
		t.Errorf("signReason = %q", got)
	}
	if got := signReason("nothing useful"); strings.Contains(got, "(") && strings.Contains(got, "failed (") {
		t.Errorf("signReason without a hint = %q", got)
	}
	if got := signReason("error: Load key \"k\": bad"); !strings.Contains(got, "Load key") {
		t.Errorf("signReason key = %q", got)
	}
	if Signature(t.TempDir(), "HEAD").Signed {
		t.Error("no repo: not signed")
	}
}

func TestReflogSummaryForms(t *testing.T) {
	for _, c := range []struct {
		e    ReflogEntry
		want string
	}{
		{ReflogEntry{Kind: ReflogMerge, Message: "commit (merge): Merge x", Subject: "Merge x"}, "merge commit 'Merge x'"},
		{ReflogEntry{Kind: ReflogMerge, Message: "merge side: Fast-forward"}, "merge side"},
		{ReflogEntry{Kind: ReflogPull, Message: "pull: Fast-forward"}, "pull"},
		{ReflogEntry{Kind: ReflogRebase, Message: "rebase (finish): returning to refs/heads/x"}, "rebase finished"},
		{ReflogEntry{Kind: ReflogRebase, Message: "rebase (abort): returning to refs/heads/x"}, "rebase aborted"},
		{ReflogEntry{Kind: ReflogRebase, Message: "rebase (pick): c1"}, "rebase (pick): c1"},
		{ReflogEntry{Kind: ReflogOther, Message: "whatever"}, "whatever"},
	} {
		if got := c.e.Summary(); got != c.want {
			t.Errorf("Summary(%+v) = %q, want %q", c.e, got, c.want)
		}
	}
}

func TestUnquotePathAndHunkStart(t *testing.T) {
	if got := unquotePath(`"a\tb.txt"`); got != "a\tb.txt" {
		t.Errorf("unquotePath = %q", got)
	}
	if got := unquotePath(`"bad\q"`); got != `"bad\q"` {
		t.Errorf("an invalid quoted path stays as it is, got %q", got)
	}
	if got := unquotePath("plain.txt"); got != "plain.txt" {
		t.Errorf("unquotePath(plain) = %q", got)
	}
	if hunkNewStart("no hunk header") != 1 {
		t.Error("a line without a hunk header starts at 1")
	}
	if hunkNewStart("@@ -3,2 +7,4 @@") != 7 || hunkNewStart("@@ -3 +9 @@") != 9 {
		t.Error("hunkNewStart")
	}
}

func TestHasCommitHooksFindsOnlyExecutableHooks(t *testing.T) {
	r := committedRepo(t)
	if HasCommitHooks(r.dir) {
		t.Error("a fresh repo has only .sample hooks")
	}
	if HasCommitHooks(t.TempDir()) {
		t.Error("no repo: no hooks")
	}
	hooks := filepath.Join(r.dir, ".git", "hooks")
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if HasCommitHooks(r.dir) {
		t.Error("a hook that is not executable does not run")
	}
	if err := os.Chmod(filepath.Join(hooks, "pre-commit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !HasCommitHooks(r.dir) {
		t.Error("an executable pre-commit hook runs")
	}
	// core.hooksPath, relative to the repo, is honoured.
	r.git(t, "config", "core.hooksPath", "myhooks")
	if HasCommitHooks(r.dir) {
		t.Error("the hooks folder moved: the old hook no longer counts")
	}
}

func TestRestackAgentWorktreeRefusals(t *testing.T) {
	r := committedRepo(t)
	w, err := CreateAgentWorktree(r.dir, "rs1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RestackAgentWorktree(r.dir, "rs1", AgentRestackOptions{Squash: true}); err == nil {
		t.Error("squash without a message: want error")
	}
	if _, err := RestackAgentWorktree(r.dir, "rs1", AgentRestackOptions{Onto: "-x"}); err == nil || !strings.Contains(err.Error(), "not a commit") {
		t.Errorf("an option as onto = %v", err)
	}
	if _, err := RestackAgentWorktree(r.dir, "rs1", AgentRestackOptions{Onto: "nope"}); err == nil || !strings.Contains(err.Error(), "not a commit of this repository") {
		t.Errorf("unknown onto = %v", err)
	}
	if err := os.RemoveAll(w.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := RestackAgentWorktree(r.dir, "rs1", AgentRestackOptions{}); err == nil || !strings.Contains(err.Error(), "is gone") {
		t.Errorf("a removed worktree = %v", err)
	}
}

func TestViaResetAndCounts(t *testing.T) {
	x := &scriptRunner{out: map[string]string{
		"git rev-parse --verify -q HEAD~1": "1111111111\n",
		"git reset":                        "",
		"git rev-list --count":             "x\n",
	}}
	s := StashAt("/c", x)
	if err := s.Reset("bogus", "HEAD"); err == nil {
		t.Error("an unknown reset mode is refused")
	}
	if err := s.Uncommit(); err != nil {
		t.Errorf("Uncommit = %v", err)
	}
	if s.CountCommits("a..b") != -1 {
		t.Error("an unreadable count is -1")
	}
	if StashAt("/c", &scriptRunner{}).CountCommits("a..b") != -1 {
		t.Error("a failing count is -1")
	}
	if StashAt("/c", &scriptRunner{}).Subject() != "" {
		t.Error("a failing subject is empty")
	}
	if err := StashAt("/c", &scriptRunner{}).Uncommit(); err == nil {
		t.Error("no HEAD~1: the first commit cannot be undone")
	}
	if _, err := StashAt("/c", &scriptRunner{}).ResetHardSafe("HEAD"); err == nil {
		t.Error("no commit to reset: want error")
	}
	// A failing update-ref stops before anything is reset.
	y := &scriptRunner{out: map[string]string{"git rev-parse --verify -q HEAD": "abcdef1234\n"}}
	if _, err := StashAt("/c", y).ResetHardSafe("HEAD"); err == nil {
		t.Error("update-ref fails: want error")
	}
	// A status that cannot be read stops before the reset too.
	z := &scriptRunner{out: map[string]string{"git rev-parse --verify -q HEAD": "abcdef1234\n", "git update-ref": ""}}
	if _, err := StashAt("/c", z).ResetHardSafe("HEAD"); err == nil || !strings.Contains(err.Error(), "nothing was reset") {
		t.Errorf("status fails = %v", err)
	}
	for _, l := range z.seen {
		if strings.Contains(l, "reset") {
			t.Errorf("nothing may be reset, ran %q", l)
		}
	}
	// The local forms keep the package functions.
	r := rebaseRepoNoT(t)
	if err := StashAt(r.dir, nil).Uncommit(); err != nil {
		t.Errorf("local Uncommit = %v", err)
	}
	if err := StashAt(r.dir, nil).Reset(ResetSoft, "HEAD"); err != nil {
		t.Errorf("local Reset = %v", err)
	}
	if _, err := StashAt(r.dir, nil).ResetHardSafe("HEAD"); err != nil {
		t.Errorf("local ResetHardSafe = %v", err)
	}
}

func rebaseRepoNoT(t *testing.T) *gitTestRepo {
	r, _ := rebaseRepo(t, 2)
	return r
}
