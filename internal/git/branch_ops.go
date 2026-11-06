package git

import (
	"errors"
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// ErrBranchNotMerged says a safe delete refused a branch whose commits
// are on no other branch; only a force delete takes it.
var ErrBranchNotMerged = errors.New("the branch is not fully merged")

func runGit(dir string, args ...string) error {
	cmd := spawn.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmdError(err, out)
	}
	return nil
}

// RenameBranch renames a local branch (`git branch -m`); an existing
// name is refused.
func RenameBranch(dir, old, name string) error {
	return runGit(dir, "branch", "-m", "--", old, name)
}

// DeleteBranch deletes a local branch. Without force a branch with
// commits no other branch has is refused with ErrBranchNotMerged.
func DeleteBranch(dir, name string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	err := runGit(dir, "branch", flag, "--", name)
	if err != nil && !force && BranchTip(dir, name) != "" && runGit(dir, "merge-base", "--is-ancestor", "refs/heads/"+name, "HEAD") != nil {
		return ErrBranchNotMerged // checked, not read from git's (maybe translated) text
	}
	return err
}

// BranchTip is the short hash a branch points at ("" when unknown):
// `git branch <name> <tip>` brings a deleted branch back.
func BranchTip(dir, name string) string {
	out, err := runOut(dir, "git", "rev-parse", "--short", "--verify", "-q", "refs/heads/"+name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// CreateBranchAt makes a branch that starts at ref (a branch, tag or
// commit) and switches to it.
func CreateBranchAt(dir, name, ref string) error {
	return runGit(dir, "checkout", "-q", "-b", name, ref, "--")
}

// CheckoutDetached checks a commit out on its own (detached HEAD).
func CheckoutDetached(dir, rev string) error {
	return runGit(dir, "checkout", "-q", "--detach", rev, "--")
}

// DeleteRemoteBranch deletes "remote/branch" on the remote
// (`git push <remote> --delete <branch>`); the tracking ref goes too.
func DeleteRemoteBranch(dir, remoteRef string) error {
	remote, branch, ok := strings.Cut(remoteRef, "/")
	if !ok || remote == "" || branch == "" {
		return fmt.Errorf("%q is not a remote branch", remoteRef)
	}
	cmd := remoteCmd(dir, "push", remote, "--delete", branch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmdError(err, out)
	}
	return nil
}

// RemoteTrackingBranches lists the remote-tracking branches
// ("origin/main"), without the remotes' HEAD aliases.
func RemoteTrackingBranches(dir string) []string {
	out, err := runOut(dir, "git", "branch", "--remotes", "--format=%(refname:short)")
	if err != nil {
		return nil
	}
	var names []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if strings.Contains(l, "/") && !strings.HasSuffix(l, "/HEAD") {
			names = append(names, l)
		}
	}
	return names
}
