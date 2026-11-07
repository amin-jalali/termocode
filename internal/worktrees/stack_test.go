package worktrees

import "testing"

// CMP-17: a stacked child's pull request targets its parent's branch:
// the branch git shows on the parent now, else the archived parent's
// kept branch, else the branch kept when the child was made. A worktree
// with no parent is not stacked.
func TestStackBase(t *testing.T) {
	metas := map[string]Meta{
		"/w/child":    {Parent: "/w/parent", Base: "feat/old"},
		"/w/orphan":   {Parent: "/w/gone", Base: "feat/kept"},
		"/w/arch-kid": {Parent: "/w/arch"},
		"/w/arch":     {State: StateArchived, Branch: "feat/arch"},
		"/w/root":     {Name: "root"},
		"/w/detached": {Parent: "/w/parent-detached"},
	}
	branchOf := map[string]string{"/w/parent": "feat/parent", "/w/parent-detached": ""}
	cases := map[string]string{
		"/w/child":    "feat/parent", // the parent's branch now wins
		"/w/orphan":   "feat/kept",   // the parent is gone: the kept base
		"/w/arch-kid": "feat/arch",   // an archived parent keeps its branch
		"/w/root":     "",
		"/w/unknown":  "",
		"/w/detached": "", // a detached parent and nothing kept
	}
	for path, want := range cases {
		if got := StackBase(metas, path, branchOf); got != want {
			t.Errorf("StackBase(%s) = %q, want %q", path, got, want)
		}
	}
}

// CMP-17: Owner finds the worktree a folder is in: the deepest worktree
// path that holds it, never a sibling that only shares a prefix.
func TestStackOwner(t *testing.T) {
	paths := []string{"/src/app", "/src/app.worktrees/x", "/src/app.worktrees/x2"}
	cases := map[string]string{
		"/src/app":                  "/src/app",
		"/src/app/internal":         "/src/app",
		"/src/app.worktrees/x":      "/src/app.worktrees/x",
		"/src/app.worktrees/x2/a/b": "/src/app.worktrees/x2",
		"/src/application":          "",
		"/elsewhere":                "",
	}
	for dir, want := range cases {
		if got := Owner(paths, dir); got != want {
			t.Errorf("Owner(%s) = %q, want %q", dir, got, want)
		}
	}
}
