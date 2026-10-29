package git

import (
	"os"
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// RebaseOptions tune RunRebase.
type RebaseOptions struct {
	Exe string // the helper binary; "" = this program
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
	if todo.Base == "" {
		args = append(args, "--root")
	} else {
		args = append(args, todo.Base)
	}
	return runRebaseCmd(dir, opt, rebasePlan{Todo: todo.Format()}, args...)
}

// runRebaseCmd runs one git rebase command with the helper as both
// editors, then reads where it stopped.
func runRebaseCmd(dir string, opt RebaseOptions, plan rebasePlan, args ...string) (RebaseOutcome, error) {
	exe := opt.Exe
	if exe == "" {
		var err error
		if exe, err = spawn.Executable(); err != nil {
			return RebaseOutcome{}, err
		}
	}
	planPath, cleanup, err := writeRebasePlan(plan)
	if err != nil {
		return RebaseOutcome{}, err
	}
	defer cleanup()
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
	p, err := runOut(dir, "git", "rev-parse", "--path-format=absolute", "--git-path", "rebase-merge")
	if err != nil {
		return false
	}
	_, err = os.Stat(strings.TrimSpace(p))
	return err == nil
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
