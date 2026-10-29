package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// RebaseState is a stopped interactive rebase, read from
// .git/rebase-merge (the Source Control banner shows it).
type RebaseState struct {
	InProgress    bool
	Step, Total   int    // the row git is on, of how many
	Action        string // that row's action (pick, edit, …)
	Hash, Subject string // its commit (short hash)
	Branch        string // the branch being rebased ("" when detached)
	Conflicts     []string
}

// rebaseMergeDir is the absolute .git/rebase-merge path of dir's work tree.
func rebaseMergeDir(dir string) (string, bool) {
	p, err := runOut(dir, "git", "rev-parse", "--path-format=absolute", "--git-path", "rebase-merge")
	if err != nil {
		return "", false
	}
	p = strings.TrimSpace(p)
	if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
		return "", false
	}
	return p, true
}

// RebaseStatus reads the interactive rebase in progress in dir (InProgress
// false when there is none).
func RebaseStatus(dir string) RebaseState {
	rm, ok := rebaseMergeDir(dir)
	if !ok {
		return RebaseState{}
	}
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(rm, name))
		return strings.TrimSpace(string(b))
	}
	st := RebaseState{InProgress: true}
	st.Step, _ = strconv.Atoi(read("msgnum"))
	st.Total, _ = strconv.Atoi(read("end"))
	st.Branch = strings.TrimPrefix(read("head-name"), "refs/heads/")
	if st.Branch == "detached HEAD" {
		st.Branch = ""
	}
	if f := strings.Fields(lastTodoLine(read("done"))); len(f) >= 2 {
		st.Action = f[0]
		if out, err := runOut(dir, "git", "log", "-1", "--format=%h%x1f%s", f[1], "--"); err == nil {
			st.Hash, st.Subject, _ = strings.Cut(strings.TrimSpace(out), "\x1f")
		}
	}
	st.Conflicts = unmergedPaths(dir)
	return st
}

// lastTodoLine is the last command line of a todo / done file.
func lastTodoLine(s string) string {
	var last string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			last = l
		}
	}
	return last
}

// RebaseContinue goes on with a stopped rebase. Conflicted files the user
// fixed (no conflict markers left) are staged first; a file that still
// has markers stops it with an error naming the file.
func RebaseContinue(dir string, opt RebaseOptions) (RebaseOutcome, error) {
	paths := unmergedPaths(dir)
	var left []string
	for _, p := range paths {
		b, err := os.ReadFile(filepath.Join(dir, p))
		if err == nil && HasConflicts(string(b)) {
			left = append(left, p)
		}
	}
	if len(left) > 0 {
		return RebaseOutcome{}, fmt.Errorf("fix the conflicts first: %s", strings.Join(left, ", "))
	}
	if len(paths) > 0 {
		cmd := spawn.Command("git", append([]string{"add", "--"}, paths...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return RebaseOutcome{}, cmdError(err, out)
		}
	}
	return runRebaseCmd(dir, opt, keptPlan(dir), "rebase", "--continue")
}

// RebaseSkip drops the commit the rebase stopped on and goes on.
func RebaseSkip(dir string, opt RebaseOptions) (RebaseOutcome, error) {
	return runRebaseCmd(dir, opt, keptPlan(dir), "rebase", "--skip")
}

// RebaseAbort stops the rebase and puts the branch back where it was.
func RebaseAbort(dir string) error {
	cmd := spawn.Command("git", "rebase", "--abort")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmdError(err, out)
	}
	return nil
}

// keptPlan is the plan the helper kept in .git/rebase-merge; when it is
// missing (a rebase started outside termocode) an empty plan is written
// there so every message stays as git proposes it.
func keptPlan(dir string) string {
	rm, ok := rebaseMergeDir(dir)
	if !ok {
		return os.DevNull
	}
	p := filepath.Join(rm, keptPlanName)
	if _, err := os.Stat(p); err != nil {
		_ = os.WriteFile(p, []byte("{}"), 0o600)
	}
	return p
}
