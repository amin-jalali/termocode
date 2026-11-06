package worktrees

import (
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/git"
)

// A repo with the main checkout, a user worktree in <repo>.worktrees/, an
// external one and an agent lane lists all four with the right kinds; a
// folder that was deleted (prunable) and a metadata entry git no longer
// lists both show as gone; bare blocks are left out.
func TestBuild(t *testing.T) {
	list := []git.Worktree{
		{Path: "/src/app", Branch: "main", Main: true},
		{Path: "/src/app.worktrees/feat", Branch: "feat/x"},
		{Path: "/elsewhere/api-v2", Branch: "api-v2"},
		{Path: "/src/app/.git/termocode/worktrees/r1", Branch: "termocode/agent-r1", Agent: true, AgentID: "r1"},
		{Path: "/tmp/deleted", Branch: "old", Prunable: true},
		{Path: "/src/bare.git", Bare: true},
	}
	metas := map[string]Meta{
		"/src/app.worktrees/feat": {Name: "wt sidebar", Pinned: true},
		"/elsewhere/made-here":    {Name: "made here", Created: mustTime("2026-11-01T10:00:00Z")},
		"/src/forgotten":          {Name: "forgotten"},
	}
	es := Build(list, metas)
	if len(es) != 7 {
		t.Fatalf("got %d entries: %+v", len(es), es)
	}
	by := map[string]Entry{}
	for _, e := range es {
		by[e.Path] = e
	}
	want := map[string]Kind{
		"/src/app": KindMain, "/src/app.worktrees/feat": KindUser, "/elsewhere/api-v2": KindExternal,
		"/src/app/.git/termocode/worktrees/r1": KindAgent, "/tmp/deleted": KindExternal,
		"/elsewhere/made-here": KindUser, "/src/forgotten": KindExternal,
	}
	for p, k := range want {
		if by[p].Kind != k {
			t.Errorf("%s: kind %v, want %v", p, by[p].Kind, k)
		}
	}
	for _, p := range []string{"/tmp/deleted", "/elsewhere/made-here", "/src/forgotten"} {
		if !by[p].Gone {
			t.Errorf("%s is not gone", p)
		}
	}
	if by["/src/app"].Gone || by["/src/app.worktrees/feat"].Gone {
		t.Error("a listed worktree is gone")
	}
	if f := by["/src/app.worktrees/feat"]; !f.Meta.Pinned || f.Title() != "wt sidebar" {
		t.Errorf("feat: %+v title %q", f, f.Title())
	}
	if by["/elsewhere/api-v2"].Title() != "api-v2" || by["/src/forgotten"].Title() != "forgotten" {
		t.Error("titles")
	}
	if got := (Entry{Worktree: git.Worktree{Path: "/x/detached-dir", Detached: true}}).Title(); got != "detached-dir" {
		t.Errorf("detached title %q", got)
	}
	if UserDir("/src/app") != filepath.FromSlash("/src/app.worktrees") {
		t.Errorf("user dir %q", UserDir("/src/app"))
	}
}

// Paths compare cleaned: a trailing slash in the metadata still joins.
func TestBuildCleansPaths(t *testing.T) {
	es := Build([]git.Worktree{{Path: "/a", Main: true}, {Path: "/b"}}, map[string]Meta{"/b/": {Pinned: true}})
	if len(es) != 2 || !es[1].Meta.Pinned || es[1].Gone {
		t.Errorf("got %+v", es)
	}
}
