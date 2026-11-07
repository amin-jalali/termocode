package ai

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

// hostFS is a workspace.FS in memory, rooted at /app.
type hostFS struct {
	mu    sync.Mutex
	files fstest.MapFS
}

func (f *hostFS) name(p string) string {
	r := strings.TrimPrefix(strings.TrimPrefix(p, "/app"), "/")
	if r == "" {
		return "."
	}
	return r
}
func (f *hostFS) Stat(_ context.Context, p string) (fs.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fs.Stat(f.files, f.name(p))
}
func (f *hostFS) ReadDir(_ context.Context, p string) ([]fs.DirEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fs.ReadDir(f.files, f.name(p))
}
func (f *hostFS) ReadFile(_ context.Context, p string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fs.ReadFile(f.files, f.name(p))
}
func (f *hostFS) WriteFile(_ context.Context, p string, data []byte, _ fs.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[f.name(p)] = &fstest.MapFile{Data: data, Mode: 0o644}
	return nil
}
func (f *hostFS) Mkdir(_ context.Context, p string, _ fs.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[f.name(p)] = &fstest.MapFile{Mode: fs.ModeDir | 0o755}
	return nil
}
func (f *hostFS) Rename(_ context.Context, from, to string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	mf, ok := f.files[f.name(from)]
	if !ok {
		return fs.ErrNotExist
	}
	delete(f.files, f.name(from))
	f.files[f.name(to)] = mf
	return nil
}
func (f *hostFS) Remove(_ context.Context, p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.files, f.name(p))
	return nil
}

// hostExec records argvs and answers from a func.
type hostExec struct {
	mu     sync.Mutex
	dirs   []string
	argvs  [][]string
	answer func(argv []string) (string, string, int)
}

func (x *hostExec) Capture(_ context.Context, dir string, argv ...string) (string, string, int, error) {
	x.mu.Lock()
	x.dirs = append(x.dirs, dir)
	x.argvs = append(x.argvs, slices.Clone(argv))
	x.mu.Unlock()
	if x.answer == nil {
		return "", "", 0, nil
	}
	o, e, c := x.answer(argv)
	return o, e, c, nil
}
func (x *hostExec) CaptureInput(ctx context.Context, dir string, _ []byte, argv ...string) (string, string, int, error) {
	return x.Capture(ctx, dir, argv...)
}
func (x *hostExec) Stream(context.Context, string, ...string) (io.ReadCloser, func(), error) {
	return nil, nil, errors.New("no stream")
}

// cmdOf drops the env prefix (env K=V …) from a recorded argv.
func cmdOf(argv []string) string {
	i := 0
	if len(argv) > 0 && argv[0] == "env" {
		i = 1
		for i < len(argv) && strings.Contains(argv[i], "=") {
			i++
		}
	}
	return strings.Join(argv[i:], " ")
}

func hostWorkspace(t *testing.T) (Workspace, *hostFS, *hostExec) {
	t.Helper()
	f := &hostFS{files: fstest.MapFS{
		"main.go":           {Data: []byte("package main\n// needle\n")},
		"pkg/a.go":          {Data: []byte("package pkg\n")},
		"pkg/a_test.go":     {Data: []byte("package pkg\n")},
		"node_modules/x.go": {Data: []byte("package x\n")},
		".env":              {Data: []byte("TOKEN=s3cret\n")},
	}}
	x := &hostExec{}
	return Workspace{Root: "/app", FS: f, Exec: x, HostLabel: "api-1"}, f, x
}

func runHostTool(t *testing.T, w Workspace, name string, args map[string]any) (string, error) {
	t.Helper()
	tool, ok := FindTool(WorkspaceTools(w), name)
	if !ok {
		t.Fatalf("no tool %s", name)
	}
	if tool.Validate != nil {
		if err := tool.Validate(args); err != nil {
			return "", err
		}
	}
	return tool.Run(context.Background(), args)
}

// RC-5 4e: a container workspace keeps every tool (same names, classes
// and approval) and runs them on the host.
func TestHostToolsKeepNamesAndClasses(t *testing.T) {
	local := WorkspaceTools(Workspace{Root: t.TempDir()})
	w, _, _ := hostWorkspace(t)
	host := WorkspaceTools(w)
	if len(local) != len(host) {
		t.Fatalf("local %d tools, host %d", len(local), len(host))
	}
	for i := range local {
		if local[i].Name != host[i].Name || local[i].Class != host[i].Class || local[i].Mutating != host[i].Mutating || local[i].ReadOnly != host[i].ReadOnly {
			t.Errorf("tool %s changed on the host", local[i].Name)
		}
	}
}

func TestHostFileToolsUseTheHostFS(t *testing.T) {
	w, f, _ := hostWorkspace(t)
	if out, err := runHostTool(t, w, "read_file", map[string]any{"path": "main.go"}); err != nil || !strings.Contains(out, "needle") {
		t.Fatalf("read_file = %q %v", out, err)
	}
	if _, err := runHostTool(t, w, "read_file", map[string]any{"path": ".env"}); err == nil {
		t.Fatal("a secret file was read")
	}
	if _, err := runHostTool(t, w, "read_file", map[string]any{"path": "../etc/passwd"}); !errors.Is(err, ErrOutsideWorkspace) {
		t.Fatalf("outside root: %v", err)
	}
	if out, _ := runHostTool(t, w, "list_dir", map[string]any{"path": "pkg"}); out != "a.go\na_test.go\n" {
		t.Fatalf("list_dir = %q", out)
	}
	if _, err := runHostTool(t, w, "write_file", map[string]any{"path": "new/b.go", "content": "package b\n"}); err != nil {
		t.Fatal(err)
	}
	if string(f.files["new/b.go"].Data) != "package b\n" {
		t.Fatal("write_file did not reach the host")
	}
	if _, err := runHostTool(t, w, "edit_file", map[string]any{"path": "main.go", "old_text": "needle", "new_text": "pin"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(f.files["main.go"].Data), "// pin") {
		t.Fatal("edit_file did not reach the host")
	}
	if _, err := runHostTool(t, w, "create_file", map[string]any{"path": "main.go"}); err == nil {
		t.Fatal("create_file over an existing file")
	}
	if _, err := runHostTool(t, w, "rename", map[string]any{"from": "new/b.go", "to": "c.go"}); err != nil || f.files["c.go"] == nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := runHostTool(t, w, "delete", map[string]any{"path": "c.go"}); err != nil || f.files["c.go"] != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := runHostTool(t, w, "delete", map[string]any{"path": "."}); err == nil {
		t.Fatal("deleted the root")
	}
}

func TestHostSearchAndGlobWalkTheHost(t *testing.T) {
	w, _, _ := hostWorkspace(t)
	out, err := runHostTool(t, w, "search", map[string]any{"query": "needle"})
	if err != nil || !strings.Contains(out, "/app/main.go:2:") {
		t.Fatalf("search = %q %v", out, err)
	}
	out, err = runHostTool(t, w, "glob", map[string]any{"pattern": "*.go,!*_test.go"})
	if err != nil || out != "main.go\npkg/a.go\n" {
		t.Fatalf("glob = %q %v", out, err)
	}
}

// git and run_command go through the Host Exec in the root; the command
// text is one argument to the container's sh.
func TestHostGitAndRunUseTheHostExec(t *testing.T) {
	w, _, x := hostWorkspace(t)
	x.answer = func(argv []string) (string, string, int) {
		switch cmdOf(argv) {
		case "git status --porcelain=v1":
			return " M main.go\n", "", 0
		case "git push":
			return "", "fatal: no upstream\n", 128
		}
		return "ok\n", "", 0
	}
	if out, _ := runHostTool(t, w, "git_status", nil); !strings.Contains(out, "M main.go") {
		t.Fatalf("git_status = %q", out)
	}
	if out, err := runHostTool(t, w, "git_stage", map[string]any{"path": "main.go"}); err != nil || out != "staged main.go" {
		t.Fatalf("git_stage = %q %v", out, err)
	}
	if _, err := runHostTool(t, w, "git_push", nil); err == nil || err.Error() != "fatal: no upstream" {
		t.Fatalf("git_push err = %v", err)
	}
	if _, err := runHostTool(t, w, "git_checkout", map[string]any{"branch": "--orphan"}); err == nil {
		t.Fatal("a branch starting with - was passed to git")
	}
	if out, _ := runHostTool(t, w, "run_command", map[string]any{"command": "echo hi; ls"}); out != "ok\n" {
		t.Fatalf("run_command = %q", out)
	}
	var got []string
	for i, a := range x.argvs {
		if x.dirs[i] != "/app" {
			t.Errorf("ran in %q", x.dirs[i])
		}
		got = append(got, cmdOf(a))
	}
	want := []string{"git status --porcelain=v1", "git add -- main.go", "git push", "sh -c echo hi; ls"}
	if !slices.Equal(got, want) {
		t.Fatalf("commands = %q, want %q", got, want)
	}
	if x.argvs[0][0] != "env" || !slices.Contains(x.argvs[0], "GIT_TERMINAL_PROMPT=0") {
		t.Fatalf("not run non-interactive: %q", x.argvs[0])
	}
}

// Nothing touches the local disk at the same path.
func TestHostToolsNeverTouchTheLocalDisk(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "local.go"), []byte("needle locally"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &hostFS{files: fstest.MapFS{"remote.go": {Data: []byte("in the container")}}}
	w := Workspace{Root: dir, FS: rootedFS{f, dir}, Exec: &hostExec{}, RunTimeout: time.Second}
	if _, err := runHostTool(t, w, "read_file", map[string]any{"path": "local.go"}); err == nil {
		t.Fatal("read_file read the local file")
	}
	if out, _ := runHostTool(t, w, "search", map[string]any{"query": "needle"}); out != "no matches" {
		t.Fatalf("search found local text: %q", out)
	}
	if _, err := runHostTool(t, w, "write_file", map[string]any{"path": "x.txt", "content": "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "x.txt")); err == nil {
		t.Fatal("write_file wrote the local disk")
	}
}

// rootedFS maps a root other than /app onto hostFS.
type rootedFS struct {
	*hostFS
	root string
}

func (r rootedFS) to(p string) string { return "/app" + strings.TrimPrefix(p, r.root) }
func (r rootedFS) Stat(ctx context.Context, p string) (fs.FileInfo, error) {
	return r.hostFS.Stat(ctx, r.to(p))
}
func (r rootedFS) ReadDir(ctx context.Context, p string) ([]fs.DirEntry, error) {
	return r.hostFS.ReadDir(ctx, r.to(p))
}
func (r rootedFS) ReadFile(ctx context.Context, p string) ([]byte, error) {
	return r.hostFS.ReadFile(ctx, r.to(p))
}
func (r rootedFS) WriteFile(ctx context.Context, p string, d []byte, m fs.FileMode) error {
	return r.hostFS.WriteFile(ctx, r.to(p), d, m)
}
