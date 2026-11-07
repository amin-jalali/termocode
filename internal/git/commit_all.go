package git

import (
	"errors"
	"strings"
)

// CommitAll commits every change of the worktree at dir in one commit
// (git add -A, then git commit): new, changed, deleted and renamed
// files. An empty message or a clean tree is an error and commits
// nothing.
func CommitAll(dir, message string) error {
	if strings.TrimSpace(message) == "" {
		return errEmptyMessage
	}
	st, err := Status(dir)
	if err != nil {
		return err
	}
	if len(st) == 0 {
		return errors.New("nothing to commit: the worktree is clean")
	}
	if _, ok, err := remoteRun(dir, "add", "-A"); ok {
		if err != nil {
			return err
		}
	} else if err := runGit(dir, "add", "-A"); err != nil {
		return err
	}
	return CommitOpts(dir, message, CommitOptions{})
}
