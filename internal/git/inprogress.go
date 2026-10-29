package git

import (
	"os"
	"strings"
)

// OperationInProgress names the git action stopped half-way in dir
// ("rebase", "merge", "cherry-pick", "revert"), or "" when there is none.
// Moving HEAD then would mix two actions, so undo refuses.
func OperationInProgress(dir string) string {
	marks := []struct{ path, op string }{
		{"rebase-merge", "rebase"},
		{"rebase-apply", "rebase"},
		{"MERGE_HEAD", "merge"},
		{"CHERRY_PICK_HEAD", "cherry-pick"},
		{"REVERT_HEAD", "revert"},
	}
	args := []string{"rev-parse", "--path-format=absolute"}
	for _, mk := range marks {
		args = append(args, "--git-path", mk.path)
	}
	out, err := runOut(dir, "git", args...)
	if err != nil {
		return ""
	}
	paths := strings.Split(strings.TrimSpace(out), "\n")
	for i, mk := range marks {
		if i < len(paths) {
			if _, err := os.Stat(paths[i]); err == nil {
				return mk.op
			}
		}
	}
	return ""
}
