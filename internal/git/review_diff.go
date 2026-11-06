package git

import (
	"errors"
	"strings"
)

// Diffs for an AI review (GITX-28).

// WorkDiff is every uncommitted change (git diff HEAD, untracked files
// not included), at most max bytes; cut says it was longer.
func WorkDiff(dir string, max int) (string, bool, error) {
	return runCapped(dir, max, false, "-c", "core.quotepath=off", "diff", "--no-color", "--no-ext-diff", "--no-textconv", "HEAD", "--")
}

// DefaultBase is the branch a pull request of HEAD goes to: origin's
// default branch (origin/HEAD), else main or master (local or on origin).
func DefaultBase(dir string) (string, error) {
	if out, err := runOut(dir, "git", "symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD"); err == nil && strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out), nil
	}
	for _, b := range []string{"main", "master", "origin/main", "origin/master"} {
		if _, err := runOut(dir, "git", "rev-parse", "--verify", "-q", b+"^{commit}"); err == nil {
			return b, nil
		}
	}
	return "", errors.New("no main or master branch")
}

// BranchDiff is the change of HEAD since it left base (git diff
// base...HEAD), at most max bytes.
func BranchDiff(dir, base string, max int) (string, bool, error) {
	return runCapped(dir, max, false, "-c", "core.quotepath=off", "diff", "--no-color", "--no-ext-diff", "--no-textconv", base+"...HEAD", "--")
}
