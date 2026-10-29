package ai

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// wsTool returns the named built-in tool for a workspace rooted at dir.
func wsTool(t *testing.T, w Workspace, name string) Tool {
	t.Helper()
	tool, ok := FindTool(WorkspaceTools(w), name)
	if !ok {
		t.Fatalf("no tool %q", name)
	}
	return tool
}

func runTool(t *testing.T, w Workspace, name string, args map[string]any) (string, error) {
	t.Helper()
	return wsTool(t, w, name).Run(context.Background(), args)
}

// gitRepo makes a git repo with one commit of a.txt, cut off from the
// developer's git config.
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "Test", "GIT_AUTHOR_EMAIL": "t@example.com",
		"GIT_COMMITTER_NAME": "Test", "GIT_COMMITTER_EMAIL": "t@example.com",
	} {
		t.Setenv(k, v)
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	writeFile(t, filepath.Join(dir, "a.txt"), "one\n")
	git("add", "a.txt")
	git("commit", "-q", "-m", "first")
	return dir
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadFileToolReadsAndRefuses(t *testing.T) {
	dir := t.TempDir()
	w := Workspace{Root: dir}
	writeFile(t, filepath.Join(dir, "sub", "x.go"), "package x\n")

	out, err := runTool(t, w, "read_file", map[string]any{"path": "sub/x.go"})
	if err != nil || out != "package x\n" {
		t.Fatalf("read = %q, %v", out, err)
	}
	if _, err := runTool(t, w, "read_file", map[string]any{"path": "nope.go"}); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing file: err = %v, want not exist", err)
	}
	if _, err := runTool(t, w, "read_file", map[string]any{"path": "../secret"}); err != ErrOutsideWorkspace {
		t.Errorf("escape: err = %v", err)
	}
	if got := wsTool(t, w, "read_file").Describe(map[string]any{"path": "sub/x.go"}); got != "Read sub/x.go" {
		t.Errorf("describe = %q", got)
	}
}

func TestReadFileToolCapsHugeFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "big.txt"), strings.Repeat("x", maxReadBytes+10))
	out, err := runTool(t, Workspace{Root: dir}, "read_file", map[string]any{"path": "big.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out, "[truncated, 10 more bytes]") || len(out) > maxReadBytes+100 {
		t.Errorf("not capped: len %d, tail %q", len(out), out[len(out)-40:])
	}
}

func TestListDirToolMarksFoldersAndFails(t *testing.T) {
	dir := t.TempDir()
	w := Workspace{Root: dir}
	writeFile(t, filepath.Join(dir, "f.txt"), "")
	writeFile(t, filepath.Join(dir, "pkg", "g.txt"), "")

	out, err := runTool(t, w, "list_dir", map[string]any{})
	if err != nil || out != "f.txt\npkg/\n" {
		t.Fatalf("root list = %q, %v", out, err)
	}
	if out, _ := runTool(t, w, "list_dir", map[string]any{"path": "pkg"}); out != "g.txt\n" {
		t.Errorf("pkg list = %q", out)
	}
	if _, err := runTool(t, w, "list_dir", map[string]any{"path": "missing"}); err == nil {
		t.Error("missing dir must fail")
	}
	if _, err := runTool(t, w, "list_dir", map[string]any{"path": "/"}); err != ErrOutsideWorkspace {
		t.Errorf("escape: err = %v", err)
	}
	ld := wsTool(t, w, "list_dir")
	if got := ld.Describe(map[string]any{}); got != "List ." {
		t.Errorf("describe root = %q", got)
	}
	if got := ld.Describe(map[string]any{"path": "pkg"}); got != "List pkg" {
		t.Errorf("describe pkg = %q", got)
	}
}

func TestSearchToolFindsAndReportsNone(t *testing.T) {
	dir := t.TempDir()
	w := Workspace{Root: dir}
	writeFile(t, filepath.Join(dir, "a.go"), "package a\n\nfunc needleHere() {}\n")

	if _, err := runTool(t, w, "search", map[string]any{"query": ""}); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty query: err = %v", err)
	}
	out, err := runTool(t, w, "search", map[string]any{"query": "needleHere"})
	if err != nil || !strings.Contains(out, "a.go:3: func needleHere() {}") {
		t.Fatalf("search = %q, %v", out, err)
	}
	if out, err := runTool(t, w, "search", map[string]any{"query": "zzz-not-there"}); err != nil || out != "no matches" {
		t.Errorf("no match = %q, %v", out, err)
	}
	if got := wsTool(t, w, "search").Describe(map[string]any{"query": "x y"}); got != `Search: "x y"` {
		t.Errorf("describe = %q", got)
	}
}

func TestSearchToolSaysWhenResultsAreCut(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "many.txt"), strings.Repeat("hit\n", 80))
	out, err := runTool(t, Workspace{Root: dir}, "search", map[string]any{"query": "hit"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "(showing 50 of 80)") {
		t.Errorf("no truncation note:\n%s", out)
	}
}

func TestSearchToolStopsOnCancel(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "hit\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := wsTool(t, Workspace{Root: dir}, "search").Run(ctx, map[string]any{"query": "hit"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want canceled", err)
	}
}

func TestGitReadToolsInRepo(t *testing.T) {
	dir := gitRepo(t)
	w := Workspace{Root: dir}

	if out, err := runTool(t, w, "git_status", nil); err != nil || out != "clean" {
		t.Fatalf("clean status = %q, %v", out, err)
	}
	if out, err := runTool(t, w, "git_diff", map[string]any{}); err != nil || out != "no changes" {
		t.Errorf("clean diff = %q, %v", out, err)
	}
	writeFile(t, filepath.Join(dir, "a.txt"), "one\ntwo\n")
	out, err := runTool(t, w, "git_status", nil)
	if err != nil || !strings.Contains(out, "a.txt") {
		t.Errorf("dirty status = %q, %v", out, err)
	}
	for _, args := range []map[string]any{{}, {"path": "a.txt"}} {
		out, err := runTool(t, w, "git_diff", args)
		if err != nil || !strings.Contains(out, "+two") {
			t.Errorf("diff %v = %q, %v", args, out, err)
		}
	}
	if out, err := runTool(t, w, "git_branch", nil); err != nil || !strings.Contains(out, "* main") {
		t.Errorf("branches = %q, %v", out, err)
	}
	gd := wsTool(t, w, "git_diff")
	if gd.Describe(map[string]any{}) != "Git diff (all)" || gd.Describe(map[string]any{"path": "a.txt"}) != "Git diff a.txt" {
		t.Error("git_diff describe")
	}
}

func TestGitReadToolsOutsideRepoFail(t *testing.T) {
	gitRepo(t) // isolates git config
	w := Workspace{Root: t.TempDir()}
	if _, err := runTool(t, w, "git_status", nil); err == nil {
		t.Error("git_status outside a repo must fail")
	}
	if _, err := runTool(t, w, "git_diff", map[string]any{}); err == nil {
		t.Error("git_diff outside a repo must fail")
	}
	// git branch prints its error as text so the model can read it.
	if out, err := runTool(t, w, "git_branch", nil); err != nil || !strings.Contains(out, "exit status") {
		t.Errorf("git_branch outside repo = %q, %v", out, err)
	}
}

func TestFileWriteTools(t *testing.T) {
	dir := t.TempDir()
	w := Workspace{Root: dir}

	out, err := runTool(t, w, "write_file", map[string]any{"path": "deep/new/f.txt", "content": "a\nb"})
	if err != nil || out != "wrote "+filepath.Join("deep", "new", "f.txt") {
		t.Fatalf("write = %q, %v", out, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "deep", "new", "f.txt")); string(b) != "a\nb" {
		t.Errorf("content = %q", b)
	}
	if got := wsTool(t, w, "write_file").Describe(map[string]any{"path": "f", "content": "a\nb"}); got != "Write f (2 lines)" {
		t.Errorf("describe = %q", got)
	}
	if _, err := runTool(t, w, "write_file", map[string]any{"path": "../x", "content": ""}); err != ErrOutsideWorkspace {
		t.Errorf("write escape: %v", err)
	}

	if _, err := runTool(t, w, "create_file", map[string]any{"path": "deep/new/f.txt"}); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Errorf("create over existing: %v", err)
	}
	if out, err := runTool(t, w, "create_file", map[string]any{"path": "c/new.txt", "content": "hi"}); err != nil || out != "created "+filepath.Join("c", "new.txt") {
		t.Errorf("create = %q, %v", out, err)
	}
	if _, err := runTool(t, w, "create_file", map[string]any{"path": "/etc/x"}); err != ErrOutsideWorkspace {
		t.Errorf("create escape: %v", err)
	}

	if out, err := runTool(t, w, "create_dir", map[string]any{"path": "d1/d2"}); err != nil || out != "created "+filepath.Join("d1", "d2") {
		t.Errorf("mkdir = %q, %v", out, err)
	}
	if st, err := os.Stat(filepath.Join(dir, "d1", "d2")); err != nil || !st.IsDir() {
		t.Error("dir not made")
	}
	if _, err := runTool(t, w, "create_dir", map[string]any{"path": "../d"}); err != ErrOutsideWorkspace {
		t.Errorf("mkdir escape: %v", err)
	}

	if out, err := runTool(t, w, "rename", map[string]any{"from": "c/new.txt", "to": "moved/there.txt"}); err != nil || out != "renamed to "+filepath.Join("moved", "there.txt") {
		t.Errorf("rename = %q, %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "moved", "there.txt")); err != nil {
		t.Error("rename target missing")
	}
	if _, err := runTool(t, w, "rename", map[string]any{"from": "../a", "to": "b"}); err != ErrOutsideWorkspace {
		t.Errorf("rename from escape: %v", err)
	}
	if _, err := runTool(t, w, "rename", map[string]any{"from": "moved/there.txt", "to": "../b"}); err != ErrOutsideWorkspace {
		t.Errorf("rename to escape: %v", err)
	}
	if _, err := runTool(t, w, "rename", map[string]any{"from": "ghost", "to": "b"}); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("rename missing: %v", err)
	}

	if out, err := runTool(t, w, "delete", map[string]any{"path": "deep"}); err != nil || out != "deleted deep" {
		t.Errorf("delete = %q, %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "deep")); !errors.Is(err, os.ErrNotExist) {
		t.Error("deep still there")
	}
	if _, err := runTool(t, w, "delete", map[string]any{"path": "../../"}); err != ErrOutsideWorkspace {
		t.Errorf("delete escape: %v", err)
	}

	for name, want := range map[string]string{
		"create_file": "Create p", "create_dir": "Create folder p", "delete": "Delete p",
	} {
		if got := wsTool(t, w, name).Describe(map[string]any{"path": "p"}); got != want {
			t.Errorf("%s describe = %q, want %q", name, got, want)
		}
	}
	if got := wsTool(t, w, "rename").Describe(map[string]any{"from": "a", "to": "b"}); got != "Rename a → b" {
		t.Errorf("rename describe = %q", got)
	}
}

func TestFileWriteToolsReportPermissionErrors(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	dir := t.TempDir()
	ro := filepath.Join(dir, "ro")
	if err := os.Mkdir(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	w := Workspace{Root: dir}
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"write_file", map[string]any{"path": "ro/f", "content": "x"}},
		{"write_file", map[string]any{"path": "ro/sub/f", "content": "x"}},
		{"create_file", map[string]any{"path": "ro/f"}},
		{"create_file", map[string]any{"path": "ro/sub/f"}},
		{"create_dir", map[string]any{"path": "ro/sub"}},
	} {
		if _, err := runTool(t, w, c.tool, c.args); !errors.Is(err, os.ErrPermission) {
			t.Errorf("%s %v: err = %v, want permission", c.tool, c.args, err)
		}
	}
	writeFile(t, filepath.Join(dir, "f"), "x")
	if _, err := runTool(t, w, "rename", map[string]any{"from": "f", "to": "ro/sub/f"}); !errors.Is(err, os.ErrPermission) {
		t.Errorf("rename into read-only: %v", err)
	}
}

func TestGitWriteTools(t *testing.T) {
	dir := gitRepo(t)
	w := Workspace{Root: dir}

	writeFile(t, filepath.Join(dir, "b.txt"), "bee\n")
	if out, err := runTool(t, w, "git_stage", map[string]any{"path": "b.txt"}); err != nil || out != "staged b.txt" {
		t.Fatalf("stage = %q, %v", out, err)
	}
	if _, err := runTool(t, w, "git_stage", map[string]any{"path": "../x"}); err != ErrOutsideWorkspace {
		t.Errorf("stage escape: %v", err)
	}
	if out, err := runTool(t, w, "git_commit", map[string]any{"message": "add b\n\nbody"}); err != nil || out != "committed" {
		t.Fatalf("commit = %q, %v", out, err)
	}
	if got := wsTool(t, w, "git_commit").Describe(map[string]any{"message": "  add b\n\nbody"}); got != `Commit: "add b"` {
		t.Errorf("commit describe = %q", got)
	}
	if out, _ := runTool(t, w, "git_status", nil); out != "clean" {
		t.Errorf("after commit status = %q", out)
	}

	co := wsTool(t, w, "git_checkout")
	if co.Describe(map[string]any{"branch": "f", "create": true}) != "Create branch f" ||
		co.Describe(map[string]any{"branch": "f"}) != "Checkout f" {
		t.Error("checkout describe")
	}
	if out, err := runTool(t, w, "git_checkout", map[string]any{"branch": "feat", "create": "true"}); err != nil || out != "created and switched to feat" {
		t.Fatalf("create branch = %q, %v", out, err)
	}
	if out, _ := runTool(t, w, "git_branch", nil); !strings.Contains(out, "* feat") {
		t.Errorf("branches = %q", out)
	}
	if out, err := runTool(t, w, "git_checkout", map[string]any{"branch": "main", "create": false}); err != nil || out != "switched to main" {
		t.Errorf("checkout = %q, %v", out, err)
	}
	if _, err := runTool(t, w, "git_checkout", map[string]any{"branch": "no-such"}); err == nil {
		t.Error("checkout of a missing branch must fail")
	}
	// No remote: push and pull fail with an error the model sees.
	if _, err := runTool(t, w, "git_push", nil); err == nil {
		t.Error("push without remote must fail")
	}
	if _, err := runTool(t, w, "git_pull", nil); err == nil {
		t.Error("pull without remote must fail")
	}
	if wsTool(t, w, "git_push").Describe(nil) != "Git push" || wsTool(t, w, "git_pull").Describe(nil) != "Git pull" {
		t.Error("push/pull describe")
	}
}

func TestRunCommandToolTimeoutAndEmptyOutput(t *testing.T) {
	dir := t.TempDir()
	w := Workspace{Root: dir, RunTimeout: 200 * time.Millisecond}
	out, err := runTool(t, w, "run_command", map[string]any{"command": "true"})
	if err != nil || out != "[no output, exit 0]" {
		t.Errorf("silent = %q, %v", out, err)
	}
	out, err = runTool(t, w, "run_command", map[string]any{"command": "echo start; exec sleep 30"})
	if err != nil || !strings.HasSuffix(out, "[timed out]") {
		t.Errorf("timeout = %q, %v", out, err)
	}
	out, err = runTool(t, w, "run_command", map[string]any{"command": "pwd"})
	if err != nil || !strings.Contains(out, filepath.Base(dir)) {
		t.Errorf("runs in root: %q, %v", out, err)
	}
	if got := wsTool(t, w, "run_command").Describe(map[string]any{"command": "go test"}); got != "Run: go test" {
		t.Errorf("describe = %q", got)
	}
}

func TestRunCommandToolStopsWhenCancelled(t *testing.T) {
	w := Workspace{Root: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := wsTool(t, w, "run_command").Run(ctx, map[string]any{"command": "sleep 30"})
	if err == nil {
		t.Error("a cancelled run must return an error, not output")
	}
}

func TestRunCmdMissingProgram(t *testing.T) {
	_, err := runCmd(context.Background(), t.TempDir(), time.Second, "termocode-no-such-program")
	if err == nil {
		t.Error("missing program must be an error")
	}
}

func TestArgHelpersAcceptJSONShapes(t *testing.T) {
	a := map[string]any{"f": float64(7), "i": 3, "s": "12", "bad": "x", "b": true, "bs": "true", "bn": "yes", "n": 4.0, "nil": nil}
	for key, want := range map[string]int{"f": 7, "i": 3, "s": 12, "bad": 0, "b": 0, "missing": 0} {
		if got := ArgInt(a, key); got != want {
			t.Errorf("ArgInt(%s) = %d, want %d", key, got, want)
		}
	}
	for key, want := range map[string]bool{"b": true, "bs": true, "bn": false, "f": false, "missing": false} {
		if got := ArgBool(a, key); got != want {
			t.Errorf("ArgBool(%s) = %v, want %v", key, got, want)
		}
	}
	if ArgString(a, "n") != "4" || ArgString(a, "nil") != "" || ArgString(a, "missing") != "" {
		t.Errorf("ArgString non-string: %q %q", ArgString(a, "n"), ArgString(a, "nil"))
	}
}
