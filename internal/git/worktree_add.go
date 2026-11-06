package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// User worktrees (WTX-5): Worktree: New… makes one on a new branch in a
// folder next to the repository (<repo>.worktrees/<name>). Nothing is
// half made: a failed add removes the folders it made and the branch.

// CheckBranchName is git's rule for a new branch name
// (`git check-ref-format --branch`).
func CheckBranchName(dir, name string) error {
	if name == "" {
		return errors.New("the branch needs a name")
	}
	if name == "HEAD" || name == "@" || strings.HasPrefix(name, "-") {
		return fmt.Errorf("%q is not a branch name git takes", name)
	}
	if _, err := gitOut(dir, "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("%q is not a branch name git takes", name)
	}
	return nil
}

// BranchExists says whether the local branch name is there.
func BranchExists(dir, name string) bool {
	_, err := gitOut(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

// AddWorktree makes a worktree at folder on the new branch from ref
// (`git worktree add -b branch folder ref`). The folder must not exist;
// its missing parents are made and removed again when git fails.
func AddWorktree(dir, folder, branch, ref string) error {
	if err := CheckBranchName(dir, branch); err != nil {
		return err
	}
	if BranchExists(dir, branch) {
		return fmt.Errorf("the branch %s already exists", branch)
	}
	if _, err := os.Lstat(folder); err == nil {
		return fmt.Errorf("the folder %s is there already", folder)
	}
	if ref == "" {
		ref = "HEAD"
	}
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("%q is not a ref", ref)
	}
	made := missingParents(folder)
	agentCreateMu.Lock()
	defer agentCreateMu.Unlock()
	_, err := gitRetry(dir, "-c", "checkout.workers=0", "worktree", "add", "-q", "-b", branch, folder, ref)
	if err == nil {
		return nil
	}
	_ = os.RemoveAll(folder)
	for _, d := range made { // deepest first; only empty ones go
		_ = os.Remove(d)
	}
	_, _ = gitOut(dir, "worktree", "prune")
	if BranchExists(dir, branch) {
		_, _ = gitOut(dir, "branch", "-D", branch)
	}
	return err
}

// missingParents are the parents of p that are not there yet, deepest
// first.
func missingParents(p string) []string {
	var out []string
	for d := filepath.Dir(p); d != filepath.Dir(d); d = filepath.Dir(d) {
		if _, err := os.Lstat(d); err == nil {
			break
		}
		out = append(out, d)
	}
	return out
}

// WorktreeRefs is what a new worktree can start from: the current
// branch first, then the local branches, the remote branches and the
// tags. current is "" when HEAD is detached.
func WorktreeRefs(dir string) (refs []string, current string, err error) {
	if b, err := gitOut(dir, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		current = strings.TrimSpace(b)
	}
	out, err := gitOut(dir, "for-each-ref", "--format=%(refname)", "refs/heads", "refs/remotes", "refs/tags")
	if err != nil {
		return nil, current, err
	}
	var heads, remotes, tags []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		switch {
		case strings.HasPrefix(l, "refs/heads/"):
			if n := strings.TrimPrefix(l, "refs/heads/"); n != current {
				heads = append(heads, n)
			}
		case strings.HasPrefix(l, "refs/remotes/"):
			if n := strings.TrimPrefix(l, "refs/remotes/"); !strings.HasSuffix(n, "/HEAD") {
				remotes = append(remotes, n)
			}
		case strings.HasPrefix(l, "refs/tags/"):
			tags = append(tags, strings.TrimPrefix(l, "refs/tags/"))
		}
	}
	if current != "" {
		refs = append(refs, current)
	}
	refs = append(append(append(refs, heads...), remotes...), tags...)
	return refs, current, nil
}

// RemoteHosts lists the hosts of the repository's remotes (fetch URLs),
// once each, in remote name order (WTX-9: the hosts a link URL may use).
func RemoteHosts(dir string) []string {
	out, err := runOut(dir, "git", "remote", "-v")
	if err != nil {
		return nil
	}
	var hosts []string
	seen := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 3 || f[2] != "(fetch)" {
			continue
		}
		if h := strings.ToLower(HostFromURL(f[1])); h != "" && !seen[h] {
			seen[h] = true
			hosts = append(hosts, h)
		}
	}
	return hosts
}

// ForgetWorktree drops git's record of the worktree at path whose folder
// is gone (`git worktree remove --force`, one worktree, not a prune of
// all). A folder that is still there is refused; a path git no longer
// lists is fine.
func ForgetWorktree(dir, path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("%s is still there", path)
	}
	cmd := spawn.Command("git", "worktree", "remove", "--force", "--", path)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "is not a working tree") {
			return nil
		}
		return fmt.Errorf("git worktree remove: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
