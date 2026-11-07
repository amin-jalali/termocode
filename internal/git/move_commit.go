package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// GITX-39 (J13): move the last commit to another branch. The commit is
// put on the other branch without a checkout (a private index applies its
// change on top of that branch), then the current branch drops it with
// `git reset --keep`, which keeps the uncommitted changes and refuses
// when they touch the same files. A commit that is already pushed is only
// copied: the current branch keeps it, so no history that others have is
// rewritten.

// MoveCommitInfo is what the move card says about the last commit.
type MoveCommitInfo struct {
	Branch  string // the current branch
	Hash    string // the full id of the last commit
	Short   string
	Subject string
	// Pushed: a remote-tracking branch has the commit already.
	Pushed bool
}

// LastCommitForMove reads the last commit and refuses one that cannot be
// moved: a detached HEAD, the first commit, a merge commit, or a merge,
// rebase or other operation in progress.
func LastCommitForMove(dir string) (MoveCommitInfo, error) {
	var in MoveCommitInfo
	b, err := gitOut(dir, "symbolic-ref", "-q", "--short", "HEAD")
	if err != nil || strings.TrimSpace(b) == "" {
		return in, errors.New("HEAD is not on a branch: switch to the branch of the commit first")
	}
	in.Branch = strings.TrimSpace(b)
	if op := OperationInProgress(dir); op != "" {
		return in, fmt.Errorf("a %s is in progress: finish or abort it first", op)
	}
	out, err := gitOut(dir, "log", "-1", "--format=%H%x00%h%x00%P%x00%s", "HEAD")
	if err != nil {
		return in, errors.New("this branch has no commits yet")
	}
	f := strings.SplitN(strings.TrimRight(out, "\n"), "\x00", 4)
	if len(f) < 4 {
		return in, errors.New("could not read the last commit")
	}
	in.Hash, in.Short, in.Subject = f[0], f[1], f[3]
	switch parents := strings.Fields(f[2]); {
	case len(parents) == 0:
		return in, errors.New("the first commit of the repository cannot be moved")
	case len(parents) > 1:
		return in, errors.New("the last commit is a merge commit: it cannot be moved")
	}
	rem, _ := gitOut(dir, "branch", "-r", "--contains", in.Hash)
	in.Pushed = strings.TrimSpace(rem) != ""
	return in, nil
}

// MoveLastCommit puts the last commit (hash, from LastCommitForMove) on
// target: a new branch at the commit when create is set, else a copy of
// the commit on top of the existing branch. Without keep the current
// branch then drops the commit. Nothing changes when a step fails.
func MoveLastCommit(dir, hash, target string, create, keep bool) error {
	head, err := HeadSHA(dir)
	if err != nil || head != hash {
		return errors.New("the last commit changed: try again")
	}
	if target == "" || strings.HasPrefix(target, "-") {
		return fmt.Errorf("%q is not a branch name", target)
	}
	if err := runGit(dir, "check-ref-format", "--branch", target); err != nil {
		return fmt.Errorf("%q is not a branch name", target)
	}
	ref := "refs/heads/" + target
	cur, _ := gitOut(dir, "symbolic-ref", "-q", "HEAD")
	if strings.TrimSpace(cur) == ref {
		return errors.New("the commit is on that branch already")
	}
	oldTip, tipErr := gitOut(dir, "rev-parse", "-q", "--verify", ref+"^{commit}")
	oldTip = strings.TrimSpace(oldTip)
	newTip := hash
	if create {
		if tipErr == nil && oldTip != "" {
			return fmt.Errorf("a branch %s exists already", target)
		}
		oldTip = ""
	} else {
		if tipErr != nil || oldTip == "" {
			return fmt.Errorf("there is no branch %s", target)
		}
		if ws, _, err := ListWorktrees(dir); err == nil {
			for _, w := range ws {
				if w.Branch == target {
					return fmt.Errorf("%s is checked out in %s: move the commit there with cherry-pick", target, w.Path)
				}
			}
		}
		if runGit(dir, "merge-base", "--is-ancestor", hash, oldTip) == nil {
			return fmt.Errorf("%s has this commit already", target)
		}
		if newTip, err = copyCommitOnto(dir, hash, oldTip); err != nil {
			return err
		}
	}
	if !keep {
		if err := runGit(dir, "reset", "-q", "--keep", hash+"^"); err != nil {
			return fmt.Errorf("the commit stays here: %w", err)
		}
	}
	if err := runGit(dir, "update-ref", "-m", "termocode: move commit "+hash[:min(12, len(hash))], ref, newTip, oldTip); err != nil {
		if !keep {
			_ = runGit(dir, "reset", "-q", "--keep", hash)
		}
		return err
	}
	return nil
}

// copyCommitOnto makes a commit with the change, message and author of
// hash on top of onto, in a private index: the working tree, the index
// and every branch stay as they are. A change that does not apply cleanly
// is refused.
func copyCommitOnto(dir, hash, onto string) (string, error) {
	gd, err := gitOut(dir, "rev-parse", "--path-format=absolute", "--git-dir")
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(strings.TrimSpace(gd), "termocode-move-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	env := append(os.Environ(), "GIT_INDEX_FILE="+filepath.Join(tmp, "index"))
	run := func(stdin string, extra []string, args ...string) (string, error) {
		cmd := spawn.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(append([]string{}, env...), extra...)
		if stdin != "" {
			cmd.Stdin = strings.NewReader(stdin)
		}
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return "", cmdError(err, []byte(stderr.String()))
		}
		return string(out), nil
	}
	if _, err := run("", nil, "read-tree", onto); err != nil {
		return "", err
	}
	patch, err := run("", nil, "diff-tree", "-p", "--binary", "--full-index", hash+"^", hash)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(patch) != "" {
		if _, err := run(patch, nil, "apply", "--cached", "--whitespace=nowarn"); err != nil {
			return "", errors.New("the commit does not apply cleanly on that branch: use cherry-pick there and solve the conflicts")
		}
	}
	tree, err := run("", nil, "write-tree")
	if err != nil {
		return "", err
	}
	meta, err := run("", nil, "log", "-1", "--format=%an%x00%ae%x00%ad", "--date=raw", hash)
	if err != nil {
		return "", err
	}
	a := strings.SplitN(strings.TrimRight(meta, "\n"), "\x00", 3)
	if len(a) < 3 {
		return "", errors.New("could not read the commit's author")
	}
	msg, err := run("", nil, "log", "-1", "--format=%B", hash)
	if err != nil {
		return "", err
	}
	out, err := run(msg, []string{"GIT_AUTHOR_NAME=" + a[0], "GIT_AUTHOR_EMAIL=" + a[1], "GIT_AUTHOR_DATE=" + a[2]},
		"commit-tree", strings.TrimSpace(tree), "-p", onto)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
