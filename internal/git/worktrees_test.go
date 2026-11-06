package git

import (
	"path/filepath"
	"testing"
)

// A porcelain fixture with every kind of block: the main checkout, a
// user worktree, a locked one (with a reason), a prunable one, a
// detached HEAD, an agent lane in the git dir and a bare repository.
const worktreePorcelain = `worktree /src/app
HEAD 1111111111111111111111111111111111111111
branch refs/heads/main

worktree /src/app.worktrees/feat-x
HEAD 2222222222222222222222222222222222222222
branch refs/heads/feat/x

worktree /src/locked
HEAD 3333333333333333333333333333333333333333
branch refs/heads/locked-one
locked on a USB drive

worktree /tmp/gone
HEAD 4444444444444444444444444444444444444444
branch refs/heads/gone
prunable gitdir file points to non-existent location

worktree /src/detached
HEAD 5555555555555555555555555555555555555555
detached

worktree /src/app/.git/termocode/worktrees/r1-a
HEAD 6666666666666666666666666666666666666666
branch refs/heads/termocode/agent-r1-a

worktree /src/bare.git
bare
`

func TestParseWorktreeList(t *testing.T) {
	ws := ParseWorktreeList(worktreePorcelain, "/src/app/.git")
	if len(ws) != 7 {
		t.Fatalf("got %d worktrees, want 7: %+v", len(ws), ws)
	}
	main, user, locked, gone, det, agent, bare := ws[0], ws[1], ws[2], ws[3], ws[4], ws[5], ws[6]
	if !main.Main || main.Path != "/src/app" || main.Branch != "main" || main.Head != "1111111111111111111111111111111111111111" {
		t.Errorf("main: %+v", main)
	}
	if user.Main || user.Branch != "feat/x" || user.Agent || user.Locked || user.Prunable {
		t.Errorf("user: %+v", user)
	}
	if !locked.Locked || locked.LockReason != "on a USB drive" {
		t.Errorf("locked: %+v", locked)
	}
	if !gone.Prunable || gone.PruneReason == "" {
		t.Errorf("prunable: %+v", gone)
	}
	if !det.Detached || det.Branch != "" {
		t.Errorf("detached: %+v", det)
	}
	if !agent.Agent || agent.AgentID != "r1-a" || agent.Branch != "termocode/agent-r1-a" {
		t.Errorf("agent: %+v", agent)
	}
	if !bare.Bare || bare.Head != "" {
		t.Errorf("bare: %+v", bare)
	}
}

// A lone "locked" line (no reason) still locks; an agent branch outside
// the git dir is still an agent lane; a folder in the git dir's worktrees
// folder is one even on another branch.
func TestParseWorktreeListEdges(t *testing.T) {
	ws := ParseWorktreeList("worktree /a\nHEAD 1\nbranch refs/heads/main\n\n"+
		"worktree /b\nHEAD 2\nbranch refs/heads/termocode/agent-x\nlocked\n\n"+
		"worktree /a/.git/termocode/worktrees/y\nHEAD 3\nbranch refs/heads/other\n", "/a/.git")
	if len(ws) != 3 {
		t.Fatalf("got %+v", ws)
	}
	if !ws[1].Locked || ws[1].LockReason != "" || !ws[1].Agent || ws[1].AgentID != "x" {
		t.Errorf("b: %+v", ws[1])
	}
	if !ws[2].Agent || ws[2].AgentID != "y" {
		t.Errorf("y: %+v", ws[2])
	}
	if got := ParseWorktreeList("", "/a/.git"); len(got) != 0 {
		t.Errorf("empty: %+v", got)
	}
}

// ListWorktrees on a real repository: the main checkout, a linked one
// and an agent lane, with the git common dir.
func TestListWorktreesRealRepo(t *testing.T) {
	r := committedRepo(t)
	linked := filepath.Join(t.TempDir(), "app.worktrees", "feat")
	r.git(t, "worktree", "add", "-q", "-b", "feat", linked)
	if _, err := CreateAgentWorktree(r.dir, "r9-lane"); err != nil {
		t.Fatal(err)
	}
	ws, common, err := ListWorktrees(linked) // from inside the linked one
	if err != nil {
		t.Fatal(err)
	}
	real := func(p string) string { q, _ := filepath.EvalSymlinks(p); return q }
	if real(common) != real(filepath.Join(r.dir, ".git")) {
		t.Errorf("common %q", common)
	}
	if len(ws) != 3 {
		t.Fatalf("got %+v", ws)
	}
	if !ws[0].Main || real(ws[0].Path) != real(r.dir) {
		t.Errorf("main %+v", ws[0])
	}
	var sawLinked, sawAgent bool
	for _, w := range ws[1:] {
		switch {
		case w.Agent && w.AgentID == "r9-lane":
			sawAgent = true
		case real(w.Path) == real(linked) && w.Branch == "feat":
			sawLinked = true
		}
	}
	if !sawLinked || !sawAgent {
		t.Errorf("linked %v agent %v: %+v", sawLinked, sawAgent, ws)
	}
	if _, _, err := ListWorktrees(t.TempDir()); err == nil {
		t.Error("a folder outside git listed worktrees")
	}
}
