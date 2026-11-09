package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// aiHistoryRepo is covGitRepo with a second commit on main and a commit
// "side c" (c.txt) on branch side.
func aiHistoryRepo(t *testing.T) (*Model, string) {
	t.Helper()
	m, dir := covGitRepo(t)
	covWrite(t, dir, "a.txt", "one\ntwo\nthree\nfour\n")
	covCommit(t, dir, "second")
	covGit(t, dir, "checkout", "-q", "-b", "side")
	covWrite(t, dir, "c.txt", "c\n")
	covCommit(t, dir, "side c")
	covGit(t, dir, "checkout", "-q", "main")
	covRefresh(m)
	return m, dir
}

func aiFileText(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

// Spec: git_history reads and rewrites history as GRAPH, the commit view
// and the Git: palette do: show (a commit with its diff), file_at, search
// (the Search Log words), line_history, reflog and patch run at once;
// uncommit asks with Always allow git; cherry_pick, revert, reset, undo,
// redo, move_to, restore_file, move_commit and apply_patch ask every time
// (Cancel first), as the GUI does. reset hard keeps a backup ref and the
// changes in a stash; undo can be redone.
func TestAIGitHistoryOps(t *testing.T) {
	m, dir := aiHistoryRepo(t)
	tl := aiGitToolAt(t, m, dir, "git_history")
	for _, op := range []string{"show", "file_at", "search", "line_history", "reflog", "patch"} {
		aiWantAtOnce(t, tl, map[string]any{"op": op})
	}
	aiWantAlways(t, tl, map[string]any{"op": "uncommit"}, ai.ClassGit)
	for _, op := range []string{"cherry_pick", "revert", "reset", "undo", "redo", "move_to", "restore_file", "move_commit", "apply_patch"} {
		aiWantCancelFirst(t, tl, map[string]any{"op": op})
	}

	if out := aiRun(t, tl, map[string]any{"op": "show", "rev": "HEAD"}); !strings.Contains(out, "second") || !strings.Contains(out, "+four") {
		t.Errorf("show %q", out)
	}
	if out := aiRun(t, tl, map[string]any{"op": "file_at", "rev": "HEAD~1", "path": "a.txt"}); out != "one\ntwo\nthree\n" {
		t.Errorf("file_at %q", out)
	}
	if out := aiRun(t, tl, map[string]any{"op": "search", "query": "second"}); !strings.Contains(out, "second") || strings.Contains(out, "first") {
		t.Errorf("search %q", out)
	}
	if out := aiRun(t, tl, map[string]any{"op": "line_history", "path": "a.txt", "start": 4, "end": 4}); !strings.Contains(out, "second") {
		t.Errorf("line_history %q", out)
	}
	if out := aiRun(t, tl, map[string]any{"op": "reflog"}); !strings.Contains(out, "0 ") || !strings.Contains(out, "checkout") {
		t.Errorf("reflog %q", out)
	}
	if out := aiRun(t, tl, map[string]any{"op": "patch", "rev": "HEAD"}); !strings.Contains(out, "diff --git a/a.txt") {
		t.Errorf("patch %q", out)
	}

	aiRun(t, tl, map[string]any{"op": "cherry_pick", "revs": []any{"side"}})
	if s := covGit(t, dir, "log", "-1", "--format=%s"); s != "side c" || aiFileText(t, dir, "c.txt") != "c\n" {
		t.Errorf("after cherry_pick HEAD is %q", s)
	}
	aiRun(t, tl, map[string]any{"op": "revert", "rev": "HEAD"})
	if s := covGit(t, dir, "log", "-1", "--format=%s"); !strings.HasPrefix(s, "Revert") || aiFileText(t, dir, "c.txt") != "<missing>" {
		t.Errorf("after revert HEAD is %q", s)
	}

	covWrite(t, dir, "a.txt", "dirty\n")
	out := aiRun(t, tl, map[string]any{"op": "reset", "rev": "HEAD~2", "mode": "hard"})
	if s := covGit(t, dir, "log", "-1", "--format=%s"); s != "second" {
		t.Errorf("after reset hard HEAD is %q", s)
	}
	if !strings.Contains(out, "refs/termocode/backup/") || !strings.Contains(out, "stash") {
		t.Errorf("reset hard %q, want the backup ref and the stash named", out)
	}
	if got := covGit(t, dir, "stash", "list"); !strings.Contains(got, "before reset --hard") {
		t.Errorf("stash %q, want the dirty change kept", got)
	}

	aiRun(t, tl, map[string]any{"op": "uncommit"})
	if s := covGit(t, dir, "log", "-1", "--format=%s"); s != "first" {
		t.Errorf("after uncommit HEAD is %q", s)
	}
	if st := covGit(t, dir, "diff", "--cached", "--name-only"); st != "a.txt" {
		t.Errorf("staged %q after uncommit, want a.txt", st)
	}
	covGit(t, dir, "commit", "-q", "-m", "again")
	aiRun(t, tl, map[string]any{"op": "undo"})
	if s := covGit(t, dir, "log", "-1", "--format=%s"); s != "first" {
		t.Errorf("after undo HEAD is %q", s)
	}
	aiRun(t, tl, map[string]any{"op": "redo"})
	if s := covGit(t, dir, "log", "-1", "--format=%s"); s != "again" {
		t.Errorf("after redo HEAD is %q", s)
	}
	aiRun(t, tl, map[string]any{"op": "restore_file", "path": "a.txt", "rev": "HEAD~1"})
	if got := aiFileText(t, dir, "a.txt"); got != "one\ntwo\nthree\n" {
		t.Errorf("a.txt %q after restore_file", got)
	}
	covGit(t, dir, "checkout", "-q", "--", "a.txt")
	covGit(t, dir, "reset", "-q", "HEAD")
	covGit(t, dir, "checkout", "-q", "--", ".")
	aiRun(t, tl, map[string]any{"op": "move_commit", "branch": "moved", "create": true})
	if s := covGit(t, dir, "log", "-1", "--format=%s", "moved"); s != "again" {
		t.Errorf("moved at %q", s)
	}
	if s := covGit(t, dir, "log", "-1", "--format=%s"); s != "first" {
		t.Errorf("main at %q after move_commit, want the commit gone from it", s)
	}
}

// Spec: git_rebase does the GUI's rebase actions: status and plan run at
// once; start (an interactive rebase with a todo of pick, reword, edit,
// squash, fixup, drop), onto, continue, skip, abort, fixup (staged
// changes into a commit), autosquash, reword, drop and squash ask every
// time (Cancel first), as the web's git.rebase / git.rebase_ctl cards do.
func TestAIGitRebaseOps(t *testing.T) {
	m, dir := covGitRepo(t)
	for _, s := range []string{"c1", "c2", "c3"} {
		covWrite(t, dir, s+".txt", s+"\n")
		covCommit(t, dir, s)
	}
	tl := aiGitToolAt(t, m, dir, "git_rebase")
	for _, op := range []string{"status", "plan"} {
		aiWantAtOnce(t, tl, map[string]any{"op": op})
	}
	for _, op := range []string{"start", "onto", "continue", "skip", "abort", "fixup", "autosquash", "reword", "drop", "squash"} {
		aiWantCancelFirst(t, tl, map[string]any{"op": op})
	}
	plan := aiRun(t, tl, map[string]any{"op": "plan", "base": "HEAD~3"})
	if !strings.Contains(plan, "pick") || !strings.Contains(plan, "c1") || !strings.Contains(plan, "c3") {
		t.Errorf("plan %q", plan)
	}
	c3 := covGit(t, dir, "rev-parse", "--short", "HEAD")
	aiRun(t, tl, map[string]any{"op": "start", "base": "HEAD~3", "todo": []any{map[string]any{"hash": c3, "action": "drop"}}})
	if s := covGit(t, dir, "log", "--format=%s", "-3"); s != "c2\nc1\nfirst" {
		t.Errorf("log after dropping c3: %q", s)
	}
	aiRun(t, tl, map[string]any{"op": "reword", "rev": "HEAD", "message": "c2 again"})
	if s := covGit(t, dir, "log", "-1", "--format=%s"); s != "c2 again" {
		t.Errorf("after reword %q", s)
	}
	covWrite(t, dir, "c1.txt", "c1 fixed\n")
	covGit(t, dir, "add", "c1.txt")
	aiRun(t, tl, map[string]any{"op": "fixup", "rev": "HEAD~1"})
	if got := covGit(t, dir, "show", "HEAD~1:c1.txt"); got != "c1 fixed" {
		t.Errorf("c1.txt in c1 is %q after fixup", got)
	}
	if n := covGit(t, dir, "rev-list", "--count", "HEAD"); n != "3" {
		t.Errorf("%s commits after fixup, want 3", n)
	}

	// A conflict stops the rebase; status names it; abort puts it back.
	covGit(t, dir, "checkout", "-q", "-b", "other", "HEAD~1")
	covWrite(t, dir, "a.txt", "theirs\n")
	covCommit(t, dir, "other a")
	covGit(t, dir, "checkout", "-q", "main")
	covWrite(t, dir, "a.txt", "ours\n")
	covCommit(t, dir, "main a")
	before := covGit(t, dir, "rev-parse", "HEAD")
	_, err := aiRunErr(tl, map[string]any{"op": "onto", "onto": "other"})
	if err == nil || !strings.Contains(err.Error(), "a.txt") {
		t.Errorf("onto with a conflict: %v, want the conflicted file named", err)
	}
	if out := aiRun(t, tl, map[string]any{"op": "status"}); !strings.Contains(out, "in progress") || !strings.Contains(out, "a.txt") {
		t.Errorf("status %q", out)
	}
	aiRun(t, tl, map[string]any{"op": "abort"})
	if after := covGit(t, dir, "rev-parse", "HEAD"); after != before {
		t.Errorf("HEAD %s after abort, want %s", after, before)
	}
	if out := aiRun(t, tl, map[string]any{"op": "status"}); !strings.Contains(out, "no rebase") {
		t.Errorf("status after abort %q", out)
	}
}
