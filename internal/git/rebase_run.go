package git

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// RebaseOptions tune RunRebase.
type RebaseOptions struct {
	Exe       string // the helper binary; "" = this program
	Autostash bool   // stash uncommitted changes first, put them back after
}

// ErrRebaseDirty: there are uncommitted changes and Autostash is off.
var ErrRebaseDirty = errors.New("you have uncommitted changes")

// WorkTreeDirty says whether tracked files have uncommitted changes
// (staged or not); untracked files do not count.
func WorkTreeDirty(dir string) bool {
	out, err := runOut(dir, "git", "status", "--porcelain", "--untracked-files=no")
	return err == nil && strings.TrimSpace(out) != ""
}

// RebaseOutcome is how a rebase run ended without an error.
type RebaseOutcome struct {
	Stopped   bool     // git stopped (a conflict or an edit row); the rebase is in progress
	Conflicts []string // unmerged paths when it stopped on a conflict
}

// RunRebase runs `git rebase -i` with todo as the todo list: git starts
// termocode as its sequence editor, which writes the prepared list. git
// keeps the old HEAD in ORIG_HEAD.
func RunRebase(dir string, todo RebaseTodo, opt RebaseOptions) (RebaseOutcome, error) {
	if err := todo.Validate(); err != nil {
		return RebaseOutcome{}, err
	}
	args := []string{"rebase", "-i"}
	if opt.Autostash {
		args = append(args, "--autostash")
	} else if WorkTreeDirty(dir) {
		return RebaseOutcome{}, ErrRebaseDirty
	}
	if todo.Base == "" {
		args = append(args, "--root")
	} else {
		args = append(args, todo.Base)
	}
	plan := rebasePlan{Todo: todo.Format(), Messages: map[string]string{}}
	for _, it := range todo.Items {
		if it.Action == RebaseReword && strings.TrimSpace(it.Message) != "" {
			plan.Messages[it.FullHash] = strings.TrimSpace(it.Message)
		}
	}
	gitDir, err := runOut(dir, "git", "rev-parse", "--absolute-git-dir")
	if err != nil {
		return RebaseOutcome{}, err
	}
	plan.GitDir = strings.TrimSpace(gitDir)
	planPath, cleanup, err := writeRebasePlan(plan)
	if err != nil {
		return RebaseOutcome{}, err
	}
	defer cleanup()
	return runRebaseCmd(dir, opt, planPath, args...)
}

// runRebaseCmd runs one git rebase command with the helper (reading the
// plan at planPath) as both editors, then reads where it stopped.
func runRebaseCmd(dir string, opt RebaseOptions, planPath string, args ...string) (RebaseOutcome, error) {
	exe := opt.Exe
	if exe == "" {
		var err error
		if exe, err = spawn.Executable(); err != nil {
			return RebaseOutcome{}, err
		}
	}
	cmd := spawn.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(),
		"GIT_SEQUENCE_EDITOR="+helperCommand(exe, "seq", planPath),
		"GIT_EDITOR="+helperCommand(exe, "msg", planPath))
	out, runErr := cmd.CombinedOutput()
	if !rebaseInProgress(dir) {
		if runErr != nil {
			return RebaseOutcome{}, cmdError(runErr, out)
		}
		return RebaseOutcome{}, nil
	}
	return RebaseOutcome{Stopped: true, Conflicts: unmergedPaths(dir)}, nil
}

// rebaseInProgress says whether an interactive rebase is stopped in dir.
func rebaseInProgress(dir string) bool {
	_, ok := rebaseMergeDir(dir)
	return ok
}

// unmergedPaths lists the conflicted paths.
func unmergedPaths(dir string) []string {
	out, err := runOut(dir, "git", "diff", "--name-only", "--diff-filter=U", "-z")
	if err != nil {
		return nil
	}
	var paths []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

// RebasePushed is the branch's upstream ("origin/main"; "" without one)
// and how many commits of base..HEAD (base "" = from the root) are on it
// already: rewriting those means a force-push.
func RebasePushed(dir, base string) (upstream string, n int) {
	out, err := runOut(dir, "git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return "", 0
	}
	upstream = strings.TrimSpace(out)
	rng := []string{"HEAD"}
	if base != "" {
		rng = append(rng, "^"+base)
	}
	count := func(extra ...string) int {
		out, err := runOut(dir, "git", append(append([]string{"rev-list", "--count"}, rng...), extra...)...)
		if err != nil {
			return 0
		}
		c, _ := strconv.Atoi(strings.TrimSpace(out))
		return c
	}
	return upstream, count() - count("^@{upstream}")
}

// ErrNothingStaged: a fixup needs staged changes.
var ErrNothingStaged = errors.New("nothing is staged")

// FixupInto commits the staged changes as `git commit --fixup=target` and
// folds them into target with `git rebase -i --autosquash` (git's own
// todo, unchanged). Unstaged changes need Autostash; with them refused,
// nothing is committed.
func FixupInto(dir, target string, opt RebaseOptions) (RebaseOutcome, error) {
	if err := spawnQuiet(dir, "diff", "--cached", "--quiet"); err == nil {
		return RebaseOutcome{}, ErrNothingStaged
	}
	if !opt.Autostash && spawnQuiet(dir, "diff", "--quiet") != nil {
		return RebaseOutcome{}, ErrRebaseDirty
	}
	base := ""
	if p, err := runOut(dir, "git", "rev-parse", "--verify", "-q", target+"^"); err == nil {
		base = strings.TrimSpace(p)
	}
	if err := spawnQuiet(dir, "merge-base", "--is-ancestor", target, "HEAD"); err != nil {
		return RebaseOutcome{}, fmt.Errorf("%s is not on the current branch", target)
	}
	if _, err := LoadRebaseTodo(dir, base); err != nil { // a merge commit after target
		return RebaseOutcome{}, err
	}
	cmd := spawn.Command("git", "commit", "--fixup="+target)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return RebaseOutcome{}, cmdError(err, out)
	}
	args := []string{"rebase", "-i", "--autosquash"}
	if opt.Autostash {
		args = append(args, "--autostash")
	}
	if base == "" {
		args = append(args, "--root")
	} else {
		args = append(args, base)
	}
	planPath, cleanup, err := writeRebasePlan(rebasePlan{})
	if err != nil {
		return RebaseOutcome{}, err
	}
	defer cleanup()
	return runRebaseCmd(dir, opt, planPath, args...)
}

// spawnQuiet runs git and only reports its exit.
func spawnQuiet(dir string, args ...string) error {
	cmd := spawn.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}
