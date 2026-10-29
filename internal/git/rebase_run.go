package git

import (
	"errors"
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
