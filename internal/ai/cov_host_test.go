package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// On a container host every tool keeps its rules: paths stay inside the
// root, secret files stay closed, a failed git says why.
func TestHostToolsRefuseWhatLocalToolsRefuse(t *testing.T) {
	w, _, _ := hostWorkspace(t)
	outside := map[string]map[string]any{
		"read_file":   {"path": "../etc/passwd"},
		"list_dir":    {"path": "../x"},
		"glob":        {"pattern": "*.go", "path": "../x"},
		"git_diff":    {"path": "../x"},
		"write_file":  {"path": "../x", "content": "c"},
		"edit_file":   {"path": "../x", "old_text": "a", "new_text": "b"},
		"create_file": {"path": "../x", "content": "c"},
		"create_dir":  {"path": "../x"},
		"delete":      {"path": "../x"},
		"rename":      {"from": "../x", "to": "y"},
		"git_stage":   {"path": "../x"},
	}
	for name, args := range outside {
		if _, err := runHostTool(t, w, name, args); !errors.Is(err, ErrOutsideWorkspace) {
			t.Errorf("%s: %v, want ErrOutsideWorkspace", name, err)
		}
	}
	if _, err := runHostTool(t, w, "rename", map[string]any{"from": "main.go", "to": "../y"}); !errors.Is(err, ErrOutsideWorkspace) {
		t.Errorf("rename target: %v", err)
	}
	for _, name := range []string{"read_file", "edit_file"} {
		args := map[string]any{"path": ".git-credentials", "old_text": "a", "new_text": "b"}
		if _, err := runHostTool(t, w, name, args); err == nil {
			t.Errorf("%s opened a secret file", name)
		}
	}
	if _, err := runHostTool(t, w, "delete", map[string]any{"path": "."}); err == nil || !strings.Contains(err.Error(), "root") {
		t.Errorf("delete root: %v", err)
	}
	if _, err := runHostTool(t, w, "glob", map[string]any{"pattern": "  "}); err == nil {
		t.Error("an empty glob pattern")
	}
	if _, err := runHostTool(t, w, "create_file", map[string]any{"path": "main.go", "content": "x"}); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Errorf("create over a file: %v", err)
	}
	// files that are not there
	for name, args := range map[string]map[string]any{
		"read_file": {"path": "nope.go"}, "list_dir": {"path": "nope"}, "glob": {"pattern": "*.go", "path": "nope"},
		"edit_file": {"path": "nope.go", "old_text": "a", "new_text": "b"},
	} {
		if _, err := runHostTool(t, w, name, args); err == nil {
			t.Errorf("%s: a missing path must be an error", name)
		}
	}
	if _, err := runHostTool(t, w, "edit_file", map[string]any{"path": "main.go", "old_text": "not there", "new_text": "b"}); err == nil {
		t.Error("an edit of text that is not in the file")
	}
	if _, err := runHostTool(t, w, "git_checkout", map[string]any{"branch": "-D"}); err == nil {
		t.Error("a branch name that starts with a dash")
	}
	if _, err := runHostTool(t, w, "run_command", map[string]any{"command": "npm run dev & # web"}); err != nil && !errors.Is(err, ErrNoWebPanel) {
		t.Errorf("web panel check: %v", err)
	}
}

func TestHostToolsShowResults(t *testing.T) {
	w, f, x := hostWorkspace(t)
	run := func(name string, args map[string]any) string {
		t.Helper()
		out, err := runHostTool(t, w, name, args)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return out
	}
	if out := run("list_dir", map[string]any{"path": "."}); !strings.Contains(out, "pkg/\n") || !strings.Contains(out, "main.go\n") {
		t.Errorf("list_dir %q", out)
	}
	if out := run("glob", map[string]any{"pattern": "*.nothing"}); out != "no files" {
		t.Errorf("glob %q", out)
	}
	if out := run("glob", map[string]any{"pattern": "**/*.go, !**/*_test.go"}); !strings.Contains(out, "pkg/a.go") || strings.Contains(out, "_test") || strings.Contains(out, "node_modules") {
		t.Errorf("glob %q", out)
	}
	if out := run("glob", map[string]any{"pattern": "!**/*_test.go"}); strings.Contains(out, "_test") || !strings.Contains(out, "main.go") {
		t.Errorf("exclude-only glob %q", out)
	}
	for i := 0; i < 250; i++ {
		f.files["big/"+strings.Repeat("a", 1)+string(rune('a'+i%26))+strings.Repeat("b", i/26)+".txt"] = f.files["main.go"]
	}
	if out := run("glob", map[string]any{"pattern": "big/*.txt"}); !strings.Contains(out, "showing 200 of") {
		t.Errorf("a long glob says it is cut: %q", out[len(out)-80:])
	}
	run("write_file", map[string]any{"path": "w.txt", "content": "hello"})
	run("create_dir", map[string]any{"path": "d"})
	run("rename", map[string]any{"from": "w.txt", "to": "d/w.txt"})
	if out := run("delete", map[string]any{"path": "d/w.txt"}); out != "deleted d/w.txt" {
		t.Errorf("delete %q", out)
	}
	// git: output words
	x.answer = func(argv []string) (string, string, int) { return "", "", 0 }
	if run("git_status", nil) != "clean" || run("git_diff", nil) != "no changes" {
		t.Error("clean status / no changes")
	}
	run("git_diff", map[string]any{"path": "main.go"})
	if c := cmdOf(x.argvs[len(x.argvs)-1]); !strings.HasSuffix(c, "diff --no-color --no-ext-diff HEAD -- main.go") {
		t.Errorf("diff command %q", c)
	}
	x.answer = func(argv []string) (string, string, int) { return " M a.go\n", "", 0 }
	if !strings.Contains(run("git_status", nil), "M a.go") || !strings.Contains(run("git_diff", nil), "M a.go") || !strings.Contains(run("git_branch", nil), "M a.go") {
		t.Error("git output is shown")
	}
	// writes: the done text, and the exact argv
	x.answer = func(argv []string) (string, string, int) { return "", "", 0 }
	for _, c := range []struct {
		tool string
		args map[string]any
		want string
		cmd  string
	}{
		{"git_commit", map[string]any{"message": "m", "signoff": true}, "committed", "commit --signoff -m m"},
		{"git_commit", map[string]any{"amend": true}, "amended the last commit", "commit --amend --no-edit"},
		{"git_commit", map[string]any{"amend": true, "message": "new"}, "amended the last commit", "commit --amend -m new"},
		{"git_checkout", map[string]any{"branch": "b", "create": true}, "created and switched to b", "checkout -b b"},
		{"git_checkout", map[string]any{"branch": "b"}, "switched to b", "checkout b"},
		{"git_push", nil, "pushed", "push"},
		{"git_pull", nil, "pulled", "pull"},
	} {
		if got := run(c.tool, c.args); got != c.want {
			t.Errorf("%s: %q, want %q", c.tool, got, c.want)
		}
		if got := cmdOf(x.argvs[len(x.argvs)-1]); got != "git "+c.cmd {
			t.Errorf("%s ran %q, want git %s", c.tool, got, c.cmd)
		}
	}
	// a failing git: its own first line, or the exit code
	x.answer = func(argv []string) (string, string, int) { return "", "fatal: not a repo\nhint: x", 128 }
	if _, err := runHostTool(t, w, "git_push", nil); err == nil || err.Error() != "fatal: not a repo" {
		t.Errorf("push failure: %v", err)
	}
	x.answer = func(argv []string) (string, string, int) { return "", "", 3 }
	if _, err := runHostTool(t, w, "git_pull", nil); err == nil || !strings.Contains(err.Error(), "code 3") {
		t.Errorf("pull failure: %v", err)
	}
	if out := run("run_command", map[string]any{"command": "false"}); !strings.Contains(out, "[exit status 3]") {
		t.Errorf("failed command: %q", out)
	}
	x.answer = func(argv []string) (string, string, int) { return "  \n", "", 0 }
	if out := run("run_command", map[string]any{"command": "true"}); out != "[no output, exit 0]" {
		t.Errorf("silent command: %q", out)
	}
}

func TestHostWithoutExecAndOnTimeout(t *testing.T) {
	w, _, _ := hostWorkspace(t)
	noExec := w
	noExec.Exec = nil
	if _, err := noExec.hostRun(context.Background(), time.Second, "git", "status"); err == nil {
		t.Error("a host without Exec cannot run commands")
	}
	if _, err := noExec.hostGitOK(context.Background(), "x", "status"); err == nil {
		t.Error("a host without Exec cannot run git")
	}
	// a command that outlives its time limit says so
	w.Exec = &slowExec{}
	out, err := w.hostRun(context.Background(), 20*time.Millisecond, "sleep", "9")
	if err != nil || !strings.HasSuffix(out, "[timed out]") {
		t.Errorf("timeout: %q %v", out, err)
	}
	// a stop from the host is an error
	w.Exec = &errExec{}
	if _, err := w.hostRun(context.Background(), time.Second, "x"); err == nil {
		t.Error("an Exec error is returned")
	}
	if _, err := w.hostGitOK(context.Background(), "x", "y"); err == nil {
		t.Error("an Exec error is returned by hostGitOK")
	}
	// glob: a cancelled context stops the walk
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w, _, _ = hostWorkspace(t)
	if _, _, err := w.hostGlob(ctx, "/app", []string{"*.go"}, 10); err == nil {
		t.Error("a dead context stops the glob")
	}
}

type slowExec struct{ hostExec }

func (*slowExec) Capture(ctx context.Context, _ string, _ ...string) (string, string, int, error) {
	<-ctx.Done()
	return "partial", "", 0, ctx.Err()
}

type errExec struct{ hostExec }

func (*errExec) Capture(context.Context, string, ...string) (string, string, int, error) {
	return "", "", 0, errors.New("docker is gone")
}
