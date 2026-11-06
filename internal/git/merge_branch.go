package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// MergeIncoming counts the commits of ref that are not on HEAD yet: what
// a merge of ref would bring in.
func MergeIncoming(dir, ref string) (int, error) {
	cmd := spawn.Command("git", "rev-list", "--count", "HEAD.."+ref, "--")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, cmdError(err, out)
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// MergeBranch merges ref into the current branch: a fast-forward when it
// can, unless noFF asks for a merge commit always. A merge that stops on
// conflicts returns the conflicted files and an error; it stays in
// progress for OpContinue or OpAbort.
func MergeBranch(dir, ref string, noFF bool) ([]string, error) {
	args := []string{"merge", "--no-edit"}
	if noFF {
		args = append(args, "--no-ff")
	}
	cmd := spawn.Command("git", append(args, ref)...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "GIT_EDITOR=true")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil, nil
	}
	if conflicts := unmergedPaths(dir); len(conflicts) > 0 {
		return conflicts, fmt.Errorf("merge stopped: %d conflict(s)", len(conflicts))
	}
	return nil, cmdError(err, out)
}

// stoppedMarks are the files git keeps while an op is stopped; a rebase
// first (it has its own banner, and may leave the others too).
var stoppedMarks = []struct{ path, op string }{
	{"rebase-merge", ""},
	{"rebase-apply", ""},
	{"MERGE_HEAD", "merge"},
	{"CHERRY_PICK_HEAD", "cherry-pick"},
	{"REVERT_HEAD", "revert"},
}

// stoppedPaths caches each repo's marker paths: the poll asks every two
// seconds and then only stats files, it starts no process.
var stoppedPaths sync.Map // dir -> []string

// StoppedOp names a merge, cherry-pick or revert stopped half-way in dir,
// "" when there is none. A rebase is left out: it has its own banner.
func StoppedOp(dir string) string {
	paths, _ := stoppedPaths.Load(dir)
	ps, _ := paths.([]string)
	if len(ps) != len(stoppedMarks) {
		args := []string{"rev-parse", "--path-format=absolute"}
		for _, mk := range stoppedMarks {
			args = append(args, "--git-path", mk.path)
		}
		out, err := runOut(dir, "git", args...)
		if err != nil {
			return ""
		}
		ps = strings.Split(strings.TrimSpace(out), "\n")
		if len(ps) != len(stoppedMarks) {
			return ""
		}
		stoppedPaths.Store(dir, ps)
	}
	for i, mk := range stoppedMarks {
		if _, err := os.Stat(ps[i]); err == nil {
			return mk.op
		}
	}
	return ""
}

// stoppable are the ops OpContinue and OpAbort take.
func stoppable(op string) bool {
	return op == "merge" || op == "cherry-pick" || op == "revert"
}

// OpContinue finishes a stopped merge, cherry-pick or revert. Fixed
// conflicted files (no markers left) are staged first; a file that still
// has markers stops it with an error naming the file.
func OpContinue(dir, op string) error {
	if !stoppable(op) {
		return fmt.Errorf("cannot continue a %s here", op)
	}
	paths := unmergedPaths(dir)
	var left []string
	for _, p := range paths {
		b, err := os.ReadFile(filepath.Join(dir, p))
		if err == nil && HasConflicts(string(b)) {
			left = append(left, p)
		}
	}
	if len(left) > 0 {
		return fmt.Errorf("fix the conflicts first: %s", strings.Join(left, ", "))
	}
	if len(paths) > 0 {
		cmd := spawn.Command("git", append([]string{"add", "--"}, paths...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return cmdError(err, out)
		}
	}
	cmd := spawn.Command("git", op, "--continue")
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "GIT_EDITOR=true")
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmdError(err, out)
	}
	return nil
}

// OpAbort stops a merge, cherry-pick or revert and puts the branch back
// where it was.
func OpAbort(dir, op string) error {
	if !stoppable(op) {
		return fmt.Errorf("cannot abort a %s here", op)
	}
	cmd := spawn.Command("git", op, "--abort")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmdError(err, out)
	}
	return nil
}

// RemoteBranches lists the remote-tracking branches ("origin/main"),
// without the remotes' HEAD aliases.
func RemoteBranches(dir string) ([]string, error) {
	out, err := runOut(dir, "git", "branch", "--remotes", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || !strings.Contains(l, "/") || strings.HasSuffix(l, "/HEAD") {
			continue
		}
		names = append(names, l)
	}
	return names, nil
}
