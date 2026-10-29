package git

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
