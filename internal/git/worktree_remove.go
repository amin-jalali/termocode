package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Archive, restore and delete of worktrees (WTX-14) and the bulk cleanup
// (WTX-15). Archive is `git worktree remove`: the folder goes, the
// branch stays. A dirty folder is refused unless the caller passes
// discard (the user saw the changed files count on a card); a locked
// worktree is never removed, with or without discard; the main checkout
// never.

var (
	// ErrWorktreeDirty: the folder has changed or new files.
	ErrWorktreeDirty = errors.New("the worktree has changed files")
	// ErrWorktreeLocked: git worktree lock is on it.
	ErrWorktreeLocked = errors.New("the worktree is locked")
)

// WorktreeDirty is the number of changed, new or conflicted files in the
// worktree at path.
func WorktreeDirty(path string) (int, error) {
	st, err := GetWorktreeStatus(path)
	return st.Dirty, err
}

// findWorktree is git's block of the worktree at path.
func findWorktree(dir, path string) (Worktree, bool, error) {
	list, _, err := ListWorktrees(dir)
	if err != nil {
		return Worktree{}, false, err
	}
	want := filepath.Clean(path)
	if real, err := filepath.EvalSymlinks(want); err == nil {
		want = real
	}
	for _, w := range list {
		p := filepath.Clean(w.Path)
		if real, err := filepath.EvalSymlinks(p); err == nil {
			p = real
		}
		if p == want {
			return w, true, nil
		}
	}
	return Worktree{}, false, nil
}

// RemoveWorktree removes the worktree folder at path (`git worktree
// remove`); the branch stays. discard: the changed files go too.
func RemoveWorktree(dir, path string, discard bool) error {
	w, ok, err := findWorktree(dir, path)
	if err != nil {
		return err
	}
	switch {
	case !ok:
		return fmt.Errorf("%s is not a worktree of this repository", path)
	case w.Main:
		return errors.New("the main checkout is never removed")
	case w.Locked:
		if w.LockReason != "" {
			return fmt.Errorf("%w (%s): git worktree unlock it first", ErrWorktreeLocked, w.LockReason)
		}
		return fmt.Errorf("%w: git worktree unlock it first", ErrWorktreeLocked)
	}
	if !discard {
		n, err := WorktreeDirty(w.Path)
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%w: %d", ErrWorktreeDirty, n)
		}
	}
	args := []string{"worktree", "remove"}
	if discard {
		args = append(args, "--force") // one --force: git still refuses a locked one
	}
	if _, err := gitRetry(dir, append(args, "--", w.Path)...); err != nil {
		return err
	}
	return nil
}

// RestoreWorktree makes the folder of an archived worktree again from
// its branch (`git worktree add path branch`).
func RestoreWorktree(dir, path, branch string) error {
	if branch == "" || strings.HasPrefix(branch, "-") || !BranchExists(dir, branch) {
		return fmt.Errorf("the branch %s is gone", branch)
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s is not an absolute path", path)
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("the folder %s is there already", path)
	}
	_, _ = gitOut(dir, "worktree", "prune") // a stale record of the same path
	made := missingParents(path)
	agentCreateMu.Lock()
	defer agentCreateMu.Unlock()
	if _, err := gitRetry(dir, "-c", "checkout.workers=0", "worktree", "add", "-q", path, branch); err != nil {
		_ = os.RemoveAll(path)
		for _, d := range made {
			_ = os.Remove(d)
		}
		return err
	}
	return nil
}

// DefaultBranch is the branch work is merged into: the remote's HEAD
// when there is one, else main, else master ("" when none is there).
func DefaultBranch(dir string) string {
	if out, err := gitOut(dir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if b := strings.TrimPrefix(strings.TrimSpace(out), "origin/"); b != "" && BranchExists(dir, b) {
			return b
		}
	}
	for _, b := range []string{"main", "master"} {
		if BranchExists(dir, b) {
			return b
		}
	}
	return ""
}

// BranchUnmerged is how many commits of branch are not on base.
func BranchUnmerged(dir, branch, base string) (int, error) {
	if strings.HasPrefix(branch, "-") || strings.HasPrefix(base, "-") {
		return 0, errors.New("not a branch name")
	}
	out, err := gitOut(dir, "rev-list", "--count", "refs/heads/"+base+"..refs/heads/"+branch)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}

// DeleteBranch deletes a local branch: `git branch -d` (merged only), or
// -D with force.
func DeleteBranch(dir, branch string, force bool) error {
	if branch == "" || strings.HasPrefix(branch, "-") {
		return fmt.Errorf("%q is not a branch name", branch)
	}
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := gitRetry(dir, "branch", flag, "--", branch)
	return err
}

// PruneWorktrees drops git's records of worktrees whose folders are gone
// (`git worktree prune`); it never touches a folder.
func PruneWorktrees(dir string) error {
	_, err := gitRetry(dir, "worktree", "prune")
	return err
}

// HeadCommitTime is when the worktree's HEAD commit was made (zero when
// it cannot be read).
func HeadCommitTime(path string) time.Time {
	out, err := gitOut(path, "log", "-1", "--format=%ct", "HEAD")
	if err != nil {
		return time.Time{}
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
