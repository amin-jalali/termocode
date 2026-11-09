package app

import (
	"context"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec: worktree does what the WORKTREES view does with one worktree.
// notes, diff, pin, sleep / wake and link run at once (the web's
// worktrees.pin / note / link / sleep have no card); create and
// add_folder ask with Always allow git; switch, archive, restore,
// restack and delete ask every time on a Cancel-first card, and after yes
// the view's own flow runs with its own cards (the delete card, where the
// user types the branch name of an unmerged worktree): the model never
// confirms them.
func TestAIWorktreeOps(t *testing.T) {
	m, dir := covGitRepo(t)
	// The UI part runs on m; a delete prep it starts comes back to m as
	// Update would route it.
	call := func(_ context.Context, fn aiIDEFunc) (string, error) {
		out, cmd, err := fn(m)
		if cmd != nil {
			if msg, ok := cmd().(wtDeletePrepMsg); ok {
				m.onWTDeletePrep(msg)
			}
		}
		return out, err
	}
	tl, ok := ai.FindTool(aiGitTools(dir, call), "worktree")
	if !ok {
		t.Fatal("no tool worktree")
	}
	for _, op := range []string{"notes", "diff", "pin", "unpin", "sleep", "wake", "link"} {
		aiWantAtOnce(t, tl, map[string]any{"op": op})
	}
	for _, op := range []string{"create", "add_folder"} {
		aiWantAlways(t, tl, map[string]any{"op": op}, ai.ClassGit)
	}
	for _, op := range []string{"switch", "archive", "restore", "restack", "delete"} {
		aiWantCancelFirst(t, tl, map[string]any{"op": op})
	}

	out := aiRun(t, tl, map[string]any{"op": "create", "name": "fix login", "from": "main"})
	if !strings.Contains(out, "fix-login") {
		t.Fatalf("create %q", out)
	}
	if got := covGit(t, dir, "branch", "--list", "fix-login"); !strings.Contains(got, "fix-login") {
		t.Fatalf("no branch fix-login: %q (%s)", got, out)
	}
	aiRun(t, tl, map[string]any{"op": "pin", "worktree": "fix-login"})
	aiRun(t, tl, map[string]any{"op": "link", "worktree": "fix-login", "kind": "issue", "number": 12})
	entries, common, err := wtRepoAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	e, found := wtFind(entries, "fix-login")
	if !found || !e.Meta.Pinned || e.Meta.Issue != 12 {
		t.Fatalf("worktree %+v: want pinned and linked to #12", e)
	}
	if err := wtAddNote(common, e.Path, "started", ""); err != nil {
		t.Fatal(err)
	}
	if out := aiRun(t, tl, map[string]any{"op": "notes", "worktree": "fix-login"}); !strings.Contains(out, "started") {
		t.Errorf("notes %q", out)
	}
	covWrite(t, e.Path, "z.txt", "z\n")
	covCommit(t, e.Path, "add z")
	if out := aiRun(t, tl, map[string]any{"op": "diff", "worktree": "fix-login"}); !strings.Contains(out, "z.txt") {
		t.Errorf("diff %q", out)
	}

	var opened string
	old := wtOpenFolder
	wtOpenFolder = func(_ *Model, path string) tea.Cmd { opened = path; return nil }
	t.Cleanup(func() { wtOpenFolder = old })
	aiRun(t, tl, map[string]any{"op": "switch", "worktree": "fix-login"})
	if opened != e.Path {
		t.Errorf("switch opened %q, want %q", opened, e.Path)
	}

	out = aiRun(t, tl, map[string]any{"op": "delete", "worktree": "fix-login"})
	if !m.confirmOpen || (m.confirmKind != confirmKindWTDelete && m.confirmKind != confirmKindWTDeleteDirty) {
		t.Errorf("delete opened no WORKTREES card (%v, kind %v): %q", m.confirmOpen, m.confirmKind, out)
	}
	if _, err := os.Stat(e.Path); err != nil {
		t.Errorf("the worktree went before the user answered: %v", err)
	}
	m.confirmOpen = false
	if st := m.wt; st == nil || !st.loaded {
		t.Error("the WORKTREES list was not loaded")
	}
}
