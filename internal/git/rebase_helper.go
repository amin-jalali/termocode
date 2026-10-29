package git

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The rebase helper is termocode itself, re-run by git as its sequence
// editor (GIT_SEQUENCE_EDITOR) and message editor (GIT_EDITOR) with a
// hidden first argument. It reads the prepared plan from a JSON file in a
// private temp folder: commit messages never pass through a shell string,
// only the quoted paths of the binary and the plan do.

// RebaseHelperArg is the hidden first argument that runs the helper.
const RebaseHelperArg = "__git-rebase-helper"

// keptPlanName is the plan's copy in .git/rebase-merge.
const keptPlanName = "termocode-plan.json"

// rebasePlan is what the helper needs.
type rebasePlan struct {
	Todo     string            `json:"todo"`     // "" keeps git's own todo (autosquash)
	Messages map[string]string `json:"messages"` // reword: full hash → new message
	GitDir   string            `json:"git_dir"`  // absolute git dir of the work tree
}

// RunRebaseHelperIfAsked runs the helper (and exits) when the process was
// started as one. termocode's main calls it first; so does a test
// binary's TestMain.
func RunRebaseHelperIfAsked() {
	if len(os.Args) > 1 && os.Args[1] == RebaseHelperArg {
		os.Exit(runRebaseHelper(os.Args[2:]))
	}
}

// runRebaseHelper: args are mode ("seq" or "msg"), the plan file and the
// file git wants edited.
func runRebaseHelper(args []string) int {
	if len(args) != 3 {
		fmt.Fprintln(os.Stderr, "termocode rebase helper: want <mode> <plan> <file>")
		return 2
	}
	mode, planPath, file := args[0], args[1], args[2]
	b, err := os.ReadFile(planPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "termocode rebase helper:", err)
		return 1
	}
	var plan rebasePlan
	if err := json.Unmarshal(b, &plan); err != nil {
		fmt.Fprintln(os.Stderr, "termocode rebase helper:", err)
		return 1
	}
	switch mode {
	case "seq":
		// Keep the plan next to git's todo: Continue and Skip after a
		// stop read it from there, and git removes it with the folder.
		_ = os.WriteFile(filepath.Join(filepath.Dir(file), keptPlanName), b, 0o600)
		if plan.Todo == "" {
			return 0
		}
		if err := os.WriteFile(file, []byte(plan.Todo), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "termocode rebase helper:", err)
			return 1
		}
		return 0
	case "msg":
		msg, ok := plan.rewordMessage()
		if !ok {
			return 0 // keep git's message (a squash shows both)
		}
		if err := os.WriteFile(file, []byte(msg+"\n"), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "termocode rebase helper:", err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, "termocode rebase helper: unknown mode", mode)
	return 2
}

// writeRebasePlan stores plan in a new private (0700) temp folder; cleanup
// removes the folder.
func writeRebasePlan(plan rebasePlan) (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "termocode-rebase-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	if err := os.Chmod(dir, 0o700); err != nil {
		cleanup()
		return "", func() {}, err
	}
	b, err := json.Marshal(plan)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	path = filepath.Join(dir, "plan.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}

// helperCommand is the editor command git runs (through sh, adding the
// file to edit): the binary, the hidden argument, the mode and the plan,
// each single-quoted.
func helperCommand(exe, mode, plan string) string {
	return strings.Join([]string{shellQuote(exe), RebaseHelperArg, mode, shellQuote(plan)}, " ")
}

// shellQuote quotes s for sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// rewordMessage is the new message when git asks for the message of a
// reword row the user typed one for. The row git is on is the last line
// of rebase-merge/done.
func (p rebasePlan) rewordMessage() (string, bool) {
	if p.GitDir == "" || len(p.Messages) == 0 {
		return "", false
	}
	b, err := os.ReadFile(filepath.Join(p.GitDir, "rebase-merge", "done"))
	if err != nil {
		return "", false
	}
	f := strings.Fields(lastTodoLine(string(b)))
	if len(f) < 2 || (f[0] != "reword" && f[0] != "r") {
		return "", false
	}
	for full, msg := range p.Messages {
		if strings.HasPrefix(full, f[1]) || strings.HasPrefix(f[1], full) {
			return msg, true
		}
	}
	return "", false
}
