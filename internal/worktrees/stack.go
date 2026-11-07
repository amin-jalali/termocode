package worktrees

import (
	"path/filepath"
	"strings"
)

// ── Stacked worktrees (CMP-17) ───────────────────────────────────────────
//
// A child worktree (c in the view) is stacked on its parent: its branch
// starts from the parent's branch, its pull request targets that branch
// and Restack replays it onto the parent again after the parent moved.

// StackBase is the branch the pull request of the worktree at path
// targets: its parent's branch as git shows it now (branchOf: worktree
// path → branch, "" detached), else the branch an archived parent kept,
// else the base kept when the child was made. "" = not stacked.
func StackBase(metas map[string]Meta, path string, branchOf map[string]string) string {
	md, ok := metas[path]
	if !ok || md.Parent == "" {
		return ""
	}
	if b := branchOf[md.Parent]; b != "" {
		return b
	}
	if p := metas[md.Parent]; p.State == StateArchived && p.Branch != "" {
		return p.Branch
	}
	return md.Base
}

// Owner is the worktree of paths that holds dir: the deepest one that is
// dir or a parent folder of it ("" none).
func Owner(paths []string, dir string) string {
	if dir == "" {
		return ""
	}
	dir = filepath.Clean(dir)
	best := ""
	for _, p := range paths {
		p = filepath.Clean(p)
		if (dir == p || strings.HasPrefix(dir, p+string(filepath.Separator))) && len(p) > len(best) {
			best = p
		}
	}
	return best
}
