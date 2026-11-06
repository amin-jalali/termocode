package app

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

	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// rcHost is a fake container Host (FIX-3): its files live in memory and
// its commands are canned answers. The workspace root is a real local
// folder holding decoy files with other names, so a feature that still
// reads the local disk shows the decoys instead of the container's
// files, and the folder must stay as it was.
type rcHost struct {
	root  string
	files fstest.MapFS
	exec  *rcExec

	mu    sync.Mutex
	reads []string // FS calls, "op path"
}

func (h *rcHost) FS() workspace.FS                 { return rcFS{h} }
func (h *rcHost) Exec() services.Exec              { return h.exec }
func (*rcHost) TermArgv(string, []string) []string { return nil }
func (*rcHost) Caps() workspace.Caps               { return workspace.Caps{HasShell: true} }
func (*rcHost) Online() bool                       { return true }
func (*rcHost) Label() string                      { return "api-1" }
func (h *rcHost) note(op, p string)                { h.mu.Lock(); h.reads = append(h.reads, op+" "+p); h.mu.Unlock() }
func (h *rcHost) calls() []string                  { h.mu.Lock(); defer h.mu.Unlock(); return slices.Clone(h.reads) }

type rcFS struct{ h *rcHost }

// name maps a host path to its MapFS name.
func (f rcFS) name(p string) (string, error) {
	abs, err := workspace.Resolve(f.h.root, p)
	if err != nil {
		return "", err
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(abs, f.h.root), "/")
	if rel == "" {
		rel = "."
	}
	return rel, nil
}

func (f rcFS) Stat(_ context.Context, p string) (fs.FileInfo, error) {
	f.h.note("stat", p)
	n, err := f.name(p)
	if err != nil {
		return nil, err
	}
	return fs.Stat(f.h.files, n)
}

func (f rcFS) ReadDir(_ context.Context, p string) ([]fs.DirEntry, error) {
	f.h.note("readdir", p)
	n, err := f.name(p)
	if err != nil {
		return nil, err
	}
	return fs.ReadDir(f.h.files, n)
}

func (f rcFS) ReadFile(_ context.Context, p string) ([]byte, error) {
	f.h.note("read", p)
	n, err := f.name(p)
	if err != nil {
		return nil, err
	}
	return fs.ReadFile(f.h.files, n)
}

func (rcFS) WriteFile(context.Context, string, []byte, fs.FileMode) error { return fs.ErrPermission }
func (rcFS) Mkdir(context.Context, string, fs.FileMode) error             { return fs.ErrPermission }
func (rcFS) Rename(context.Context, string, string) error                 { return fs.ErrPermission }
func (rcFS) Remove(context.Context, string) error                         { return fs.ErrPermission }

// rcExec answers commands run in the container: answer gets the argv
// and gives stdout and the exit code. Every call is recorded.
type rcExec struct {
	answer func(argv []string) (string, int)
	mu     sync.Mutex
	argvs  [][]string
}

func (x *rcExec) run(argv []string) (string, string, int, error) {
	x.mu.Lock()
	x.argvs = append(x.argvs, slices.Clone(argv))
	x.mu.Unlock()
	if x.answer == nil {
		return "", "not found", 127, nil
	}
	out, code := x.answer(argv)
	return out, "", code, nil
}

func (x *rcExec) Capture(_ context.Context, _ string, argv ...string) (string, string, int, error) {
	return x.run(argv)
}

func (x *rcExec) CaptureInput(_ context.Context, _ string, _ []byte, argv ...string) (string, string, int, error) {
	return x.run(argv)
}

func (x *rcExec) Stream(context.Context, string, ...string) (io.ReadCloser, func(), error) {
	return nil, nil, errors.New("no stream in the fake")
}

func (x *rcExec) calls() [][]string { x.mu.Lock(); defer x.mu.Unlock(); return slices.Clone(x.argvs) }

// newRCWorkspace builds a container workspace on a fake host whose root
// is a fresh local folder with decoy files (local.go, local.png, a .git
// folder). The folder is checked at the end of the test: nothing may be
// added, removed or changed there.
func newRCWorkspace(t *testing.T, files fstest.MapFS) (workspace.Workspace, *rcHost) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	decoys := map[string]string{
		"local.go":  "package local // needle in the LOCAL folder",
		"local.png": "not an image",
		".git/HEAD": "ref: refs/heads/local-only\n",
		"local.txt": "needle",
		// A folder only the local disk has: it must never become a
		// workspace folder of the container.
		"localdir/keep.txt": "local",
	}
	for name, text := range decoys {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before := rcSnapshot(t, root)
	t.Cleanup(func() {
		if after := rcSnapshot(t, root); after != before {
			t.Errorf("the local folder changed:\nbefore %s\nafter  %s", before, after)
		}
	})
	h := &rcHost{root: root, files: files, exec: &rcExec{}}
	ws := workspace.Workspace{ID: "container:dev/api-1:" + root, Kind: workspace.KindContainer, Root: root, Host: h}
	return ws, h
}

// rcSnapshot lists every file under root with its size and text.
func rcSnapshot(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		b.WriteString(rel)
		if !d.IsDir() {
			data, _ := os.ReadFile(p)
			b.WriteString("=" + string(data))
		}
		b.WriteString(";")
		return nil
	})
	return b.String()
}
