// Package localhost is the workspace Host for a folder on this machine:
// the os package and services.SpawnExec.
package localhost

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// New is the local workspace of root (made absolute).
func New(root string) (workspace.Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return workspace.Workspace{}, err
	}
	abs = filepath.Clean(abs)
	return workspace.Workspace{
		ID:   workspace.LocalID(abs),
		Kind: workspace.KindLocal,
		Root: abs,
		Host: host{fs: localFS{root: abs}},
	}, nil
}

// Cwd is the local workspace of the process's current folder, spelled
// as os.Getwd spells it (so $PWD with its symlinks wins, as before). A
// failed Getwd gives a workspace with an empty Root.
func Cwd() workspace.Workspace {
	cwd, err := os.Getwd()
	if err != nil {
		return workspace.Workspace{Kind: workspace.KindLocal, Host: host{}}
	}
	ws, err := New(cwd)
	if err != nil {
		return workspace.Workspace{Kind: workspace.KindLocal, Host: host{}}
	}
	return ws
}

type host struct{ fs localFS }

func (h host) FS() workspace.FS                        { return h.fs }
func (host) Exec() services.Exec                       { return services.SpawnExec{} }
func (host) Online() bool                              { return true }
func (host) Label() string                             { return "local" }
func (host) Caps() workspace.Caps                      { return workspace.Caps{HasShell: true, Arch: runtime.GOARCH} }
func (host) TermArgv(_ string, argv []string) []string { return argv }

type localFS struct{ root string }

func (f localFS) resolve(ctx context.Context, p string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return workspace.Resolve(f.root, p)
}

func (f localFS) Stat(ctx context.Context, p string) (fs.FileInfo, error) {
	abs, err := f.resolve(ctx, p)
	if err != nil {
		return nil, err
	}
	return os.Stat(abs)
}

func (f localFS) ReadDir(ctx context.Context, p string) ([]fs.DirEntry, error) {
	abs, err := f.resolve(ctx, p)
	if err != nil {
		return nil, err
	}
	return os.ReadDir(abs)
}

func (f localFS) ReadFile(ctx context.Context, p string) ([]byte, error) {
	abs, err := f.resolve(ctx, p)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}

// WriteFile writes like the AI write_file tool: parents 0755, then
// os.WriteFile (an existing file keeps its mode and owner).
func (f localFS) WriteFile(ctx context.Context, p string, data []byte, perm fs.FileMode) error {
	abs, err := f.resolve(ctx, p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, data, perm)
}

func (f localFS) Mkdir(ctx context.Context, p string, perm fs.FileMode) error {
	abs, err := f.resolve(ctx, p)
	if err != nil {
		return err
	}
	return os.MkdirAll(abs, perm)
}

func (f localFS) Rename(ctx context.Context, from, to string) error {
	src, err := f.resolve(ctx, from)
	if err != nil {
		return err
	}
	dst, err := f.resolve(ctx, to)
	if err != nil {
		return err
	}
	if src == f.root || dst == f.root {
		return workspace.ErrIsRoot
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

func (f localFS) Remove(ctx context.Context, p string) error {
	abs, err := f.resolve(ctx, p)
	if err != nil {
		return err
	}
	if abs == f.root {
		return workspace.ErrIsRoot
	}
	return os.RemoveAll(abs)
}
