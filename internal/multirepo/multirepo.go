// Package multirepo makes one task's worktrees across the repositories
// of a multi-repo workspace (CMP-22): a window whose folders hold several
// repositories (frontend + backend, a folder of services) gets one new
// worktree in each of them, on the same branch, in one step.
//
// It is pure planning plus an all-or-nothing create: the git calls are
// passed in, so the package never runs git itself. Each repository keeps
// its own git state; the group is only a tag (the branch name) that
// termocode keeps in each repository's worktrees.json.
package multirepo

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/amin-jalali/termocode/internal/worktreeform"
)

// Repo is one repository of the workspace.
type Repo struct {
	Name   string // a short label, unique in the list (the main checkout's folder name)
	Main   string // the main checkout: git runs here
	Common string // the git common dir (worktrees.json lives under it)
}

// Resolver reads the main checkout and the git common dir of the
// repository dir is in (an error: dir is no repository).
type Resolver func(dir string) (main, common string, err error)

// Repos are the repositories of dirs, once each: two folders of one
// repository (its main checkout and a worktree) count once, by git
// common dir. Folders that are no repository are left out. Order is the
// order of dirs.
func Repos(dirs []string, resolve Resolver) []Repo {
	var out []Repo
	seen := map[string]bool{}
	names := map[string]int{}
	for _, d := range dirs {
		main, common, err := resolve(d)
		if err != nil || main == "" || common == "" {
			continue
		}
		key := filepath.Clean(common)
		if seen[key] {
			continue
		}
		seen[key] = true
		name := filepath.Base(filepath.Clean(main))
		names[name]++
		if n := names[name]; n > 1 {
			name += "-" + strconv.Itoa(n)
		}
		out = append(out, Repo{Name: name, Main: filepath.Clean(main), Common: key})
	}
	return out
}

// Item is one worktree to make.
type Item struct {
	Repo   Repo
	Folder string
	Err    string // why it cannot be made ("" = fine), set by Check
}

// Plan is where each repository's worktree on branch goes: the folder
// Worktree: New… would pick (setting is config.json worktree_dir). When
// two repositories would share a folder (an absolute worktree_dir), the
// later one gets its name added.
func Plan(repos []Repo, branch, setting, home string) []Item {
	items := make([]Item, 0, len(repos))
	used := map[string]bool{}
	for _, r := range repos {
		base := worktreeform.BaseDir(r.Main, setting, home)
		folder := worktreeform.Folder(base, branch)
		if used[folder] {
			folder = filepath.Join(base, worktreeform.FolderName(branch)+"-"+worktreeform.FolderName(r.Name))
		}
		used[folder] = true
		items = append(items, Item{Repo: r, Folder: folder})
	}
	return items
}

// Probe is the git and disk reads Check needs.
type Probe struct {
	BranchName   func(dir, branch string) error // git's rule for a branch name
	BranchExists func(dir, branch string) bool
	FolderExists func(path string) bool
}

// Check sets each item's Err and says whether all can be made.
func Check(items []Item, branch string, p Probe) bool {
	ok := true
	for i := range items {
		it := &items[i]
		it.Err = ""
		switch {
		case p.BranchName != nil && p.BranchName(it.Repo.Main, branch) != nil:
			it.Err = fmt.Sprintf("%q is not a branch name git takes", branch)
		case p.BranchExists != nil && p.BranchExists(it.Repo.Main, branch):
			it.Err = "the branch " + branch + " is there already"
		case p.FolderExists != nil && p.FolderExists(it.Folder):
			it.Err = "the folder is there already"
		}
		if it.Err != "" {
			ok = false
		}
	}
	return ok
}

// AddFunc makes one worktree (git worktree add -b branch folder ref in
// dir); it leaves nothing behind when it fails.
type AddFunc func(dir, folder, branch, ref string) error

// UndoFunc removes a worktree Create made and its new branch.
type UndoFunc func(dir, folder, branch string) error

// Create makes every item's worktree on branch from ref ("" = each
// repository's current HEAD). All or nothing: when one fails, the ones
// made before go again (last first) and the error names the repository.
func Create(items []Item, branch, ref string, add AddFunc, undo UndoFunc) error {
	if ref == "" {
		ref = "HEAD"
	}
	for i, it := range items {
		if err := add(it.Repo.Main, it.Folder, branch, ref); err != nil {
			for j := i - 1; j >= 0; j-- {
				_ = undo(items[j].Repo.Main, items[j].Folder, branch)
			}
			return fmt.Errorf("%s: %w", it.Repo.Name, err)
		}
	}
	return nil
}
