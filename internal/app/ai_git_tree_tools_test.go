package app

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// aiConflictRepo is a repo stopped in a merge of branch other: a.txt
// conflicts (one block), b.txt too.
func aiConflictRepo(t *testing.T) (*Model, string) {
	t.Helper()
	m, dir := covGitRepo(t)
	covWrite(t, dir, "b.txt", "base\n")
	covCommit(t, dir, "add b")
	covGit(t, dir, "checkout", "-q", "-b", "other")
	covWrite(t, dir, "a.txt", "one\nTHEIRS\nthree\n")
	covWrite(t, dir, "b.txt", "theirs b\n")
	covCommit(t, dir, "other side")
	covGit(t, dir, "checkout", "-q", "main")
	covWrite(t, dir, "a.txt", "one\nOURS\nthree\n")
	covWrite(t, dir, "b.txt", "ours b\n")
	covCommit(t, dir, "main side")
	c := exec.Command("git", "merge", "other")
	c.Dir = dir
	_ = c.Run() // stops on the conflicts
	covRefresh(m)
	return m, dir
}

// Spec: git_conflicts resolves a stopped merge, cherry-pick or revert as
// CONFLICTS and the merge view do: list and show (each block with ours,
// base and theirs) run at once; take (a side for one block or all) and
// take_file (a whole-file side) ask every time, as the web's
// git.resolve_side card does; resolve (mark resolved) asks with Always
// allow git (git.resolve has no card); continue and abort ask every time
// (the GUI's merge card).
func TestAIGitConflictsOps(t *testing.T) {
	m, dir := aiConflictRepo(t)
	tl := aiGitToolAt(t, m, dir, "git_conflicts")
	for _, op := range []string{"list", "show"} {
		aiWantAtOnce(t, tl, map[string]any{"op": op})
	}
	aiWantAlways(t, tl, map[string]any{"op": "resolve"}, ai.ClassGit)
	for _, op := range []string{"take", "take_file", "continue", "abort"} {
		aiWantCancelFirst(t, tl, map[string]any{"op": op})
	}
	if out := aiRun(t, tl, map[string]any{"op": "list"}); !strings.Contains(out, "merge") || !strings.Contains(out, "a.txt") || !strings.Contains(out, "b.txt") {
		t.Errorf("list %q", out)
	}
	if out := aiRun(t, tl, map[string]any{"op": "show", "path": "a.txt"}); !strings.Contains(out, "OURS") || !strings.Contains(out, "THEIRS") || !strings.Contains(out, "block 1") {
		t.Errorf("show %q", out)
	}
	if _, err := aiRunErr(tl, map[string]any{"op": "resolve", "path": "a.txt"}); err == nil || !strings.Contains(err.Error(), "markers") {
		t.Errorf("resolve with markers left: %v", err)
	}
	aiRun(t, tl, map[string]any{"op": "take", "path": "a.txt", "side": "theirs", "block": 1})
	if got := aiFileText(t, dir, "a.txt"); got != "one\nTHEIRS\nthree\n" {
		t.Errorf("a.txt %q after take theirs", got)
	}
	aiRun(t, tl, map[string]any{"op": "resolve", "path": "a.txt"})
	aiRun(t, tl, map[string]any{"op": "take_file", "path": "b.txt", "side": "ours"})
	if got := aiFileText(t, dir, "b.txt"); got != "ours b\n" {
		t.Errorf("b.txt %q after take_file ours", got)
	}
	if out := aiRun(t, tl, map[string]any{"op": "continue"}); !strings.Contains(out, "merge") {
		t.Errorf("continue %q", out)
	}
	if s := covGit(t, dir, "rev-list", "--count", "--merges", "HEAD"); s != "1" {
		t.Errorf("%s merge commits after continue, want 1", s)
	}

	m2, dir2 := aiConflictRepo(t)
	before := covGit(t, dir2, "rev-parse", "HEAD")
	aiRun(t, aiGitToolAt(t, m2, dir2, "git_conflicts"), map[string]any{"op": "abort"})
	if after := covGit(t, dir2, "rev-parse", "HEAD"); after != before || aiFileText(t, dir2, "a.txt") != "one\nOURS\nthree\n" {
		t.Errorf("abort left HEAD %s (want %s), a.txt %q", after, before, aiFileText(t, dir2, "a.txt"))
	}
}

// Spec: git_lines stages, unstages or discards single lines (start..end)
// as Stage / Unstage / Discard Line do; stage and unstage ask with
// Always allow git, discard every time (it cannot be undone).
// git_discard throws changes away as Discard Changes / Discard All /
// Delete Untracked do, each on a Cancel-first card: whole files, or all
// changes and the untracked files into a safety stash.
func TestAIGitLinesAndDiscard(t *testing.T) {
	m, dir := covGitRepo(t)
	lines := aiGitToolAt(t, m, dir, "git_lines")
	for _, op := range []string{"stage", "unstage"} {
		aiWantAlways(t, lines, map[string]any{"op": op}, ai.ClassGit)
	}
	aiWantCancelFirst(t, lines, map[string]any{"op": "discard"})
	covWrite(t, dir, "a.txt", "ONE\ntwo\nTHREE\n")
	aiRun(t, lines, map[string]any{"op": "stage", "path": "a.txt", "start": 1, "end": 1})
	if got := covGit(t, dir, "show", ":a.txt"); got != "ONE\ntwo\nthree" {
		t.Errorf("index %q after staging line 1", got)
	}
	aiRun(t, lines, map[string]any{"op": "unstage", "path": "a.txt", "start": 1, "end": 1})
	if got := covGit(t, dir, "diff", "--cached", "--name-only"); got != "" {
		t.Errorf("staged %q after unstage", got)
	}
	aiRun(t, lines, map[string]any{"op": "discard", "path": "a.txt", "start": 3, "end": 3})
	if got := aiFileText(t, dir, "a.txt"); got != "ONE\ntwo\nthree\n" {
		t.Errorf("a.txt %q after discarding line 3", got)
	}

	disc := aiGitToolAt(t, m, dir, "git_discard")
	for _, op := range []string{"files", "all", "untracked"} {
		aiWantCancelFirst(t, disc, map[string]any{"op": op})
	}
	aiRun(t, disc, map[string]any{"op": "files", "paths": []any{"a.txt"}})
	if got := aiFileText(t, dir, "a.txt"); got != "one\ntwo\nthree\n" {
		t.Errorf("a.txt %q after discard files", got)
	}
	covWrite(t, dir, "new.txt", "new\n")
	out := aiRun(t, disc, map[string]any{"op": "untracked"})
	if aiFileText(t, dir, "new.txt") != "<missing>" || !strings.Contains(out, "stash@{0}") {
		t.Errorf("untracked %q; new.txt %q", out, aiFileText(t, dir, "new.txt"))
	}
	covWrite(t, dir, "a.txt", "changed\n")
	covWrite(t, dir, "n2.txt", "n2\n")
	aiRun(t, disc, map[string]any{"op": "all", "untracked": true})
	if st := covGit(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("status %q after discard all", st)
	}
}

// Spec: git_repo does the rest of the Git: palette on the repository:
// identity, submodules, contributors and lfs_files and bisect_status run
// at once; ignore, untrack, submodule_update and unshallow ask with
// Always allow git; init and the bisect steps ask every time.
func TestAIGitRepoOps(t *testing.T) {
	m, dir := covGitRepo(t)
	tl := aiGitToolAt(t, m, dir, "git_repo")
	for _, op := range []string{"identity", "submodules", "contributors", "lfs_files", "bisect_status"} {
		aiWantAtOnce(t, tl, map[string]any{"op": op})
	}
	for _, op := range []string{"ignore", "untrack", "submodule_update", "unshallow"} {
		aiWantAlways(t, tl, map[string]any{"op": op}, ai.ClassGit)
	}
	for _, op := range []string{"init", "bisect_start", "bisect_mark", "bisect_reset"} {
		aiWantCancelFirst(t, tl, map[string]any{"op": op})
	}
	if out := aiRun(t, tl, map[string]any{"op": "identity"}); !strings.Contains(out, "Tess") {
		t.Errorf("identity %q", out)
	}
	if out := aiRun(t, tl, map[string]any{"op": "contributors"}); !strings.Contains(out, "Tess") {
		t.Errorf("contributors %q", out)
	}
	aiRun(t, tl, map[string]any{"op": "ignore", "pattern": "*.log"})
	if got := aiFileText(t, dir, ".gitignore"); !strings.Contains(got, "*.log") {
		t.Errorf(".gitignore %q", got)
	}
	aiRun(t, tl, map[string]any{"op": "untrack", "path": "a.txt"})
	if got := covGit(t, dir, "ls-files", "a.txt"); got != "" {
		t.Errorf("a.txt still tracked")
	}
	if aiFileText(t, dir, "a.txt") == "<missing>" {
		t.Error("untrack deleted the file")
	}
	covGit(t, dir, "add", "a.txt", ".gitignore")
	covGit(t, dir, "commit", "-q", "-m", "ignore logs")
	for i := 0; i < 3; i++ {
		covWrite(t, dir, "a.txt", strings.Repeat("x\n", i+4))
		covCommit(t, dir, "step")
	}
	good := covGit(t, dir, "rev-list", "--max-parents=0", "HEAD")
	if out := aiRun(t, tl, map[string]any{"op": "bisect_start", "bad": "HEAD", "good": good}); !strings.Contains(out, "test") {
		t.Errorf("bisect_start %q", out)
	}
	if out := aiRun(t, tl, map[string]any{"op": "bisect_status"}); !strings.Contains(out, "bisect") {
		t.Errorf("bisect_status %q", out)
	}
	aiRun(t, tl, map[string]any{"op": "bisect_mark", "verdict": "good"})
	aiRun(t, tl, map[string]any{"op": "bisect_reset"})
	if out := aiRun(t, tl, map[string]any{"op": "bisect_status"}); !strings.Contains(out, "no bisect") {
		t.Errorf("bisect_status after reset %q", out)
	}
	fresh := t.TempDir()
	aiRun(t, aiGitToolAt(t, m, fresh, "git_repo"), map[string]any{"op": "init"})
	if got := covGit(t, fresh, "rev-parse", "--is-inside-work-tree"); got != "true" {
		t.Errorf("init: %q", got)
	}
}

// Spec: git_stash also shows an entry's diff (show, at once) and makes a
// branch from it (branch, with Always allow git), as Git: Stash Actions…
// does.
func TestAIGitStashShowAndBranch(t *testing.T) {
	m, dir := covGitRepo(t)
	tl := aiGitToolAt(t, m, dir, "git_stash")
	aiWantAtOnce(t, tl, map[string]any{"op": "show"})
	aiWantAlways(t, tl, map[string]any{"op": "branch"}, ai.ClassGit)
	covWrite(t, dir, "a.txt", "stashed\n")
	covGit(t, dir, "stash", "push", "-q", "-m", "wip")
	if out := aiRun(t, tl, map[string]any{"op": "show", "index": 0}); !strings.Contains(out, "+stashed") {
		t.Errorf("show %q", out)
	}
	aiRun(t, tl, map[string]any{"op": "branch", "name": "from-stash", "index": 0})
	if b := covGit(t, dir, "branch", "--show-current"); b != "from-stash" || aiFileText(t, dir, "a.txt") != "stashed\n" {
		t.Errorf("on %q with a.txt %q after branch", b, aiFileText(t, dir, "a.txt"))
	}
}
