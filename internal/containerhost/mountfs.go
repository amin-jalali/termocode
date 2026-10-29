package containerhost

import (
	"context"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/workspace"
	"github.com/amin-jalali/termocode/internal/workspace/localhost"
)

// mountfs: when the workspace folder is a writable bind mount of a local
// folder on a local engine, files are read and written on the local disk
// (the same files, without an engine call). A path whose symlinks lead
// out of the mount (an absolute link means a container path) goes the
// container way instead.

// mountOf is the local folder bind-mounted at the root, or "" when the
// local disk cannot stand in for the container: a remote engine, no
// writable bind mount at or above the root, another mount inside the
// workspace, or a source folder that is not here.
func (h *Host) mountOf() string {
	if !h.cfg.LocalEngine {
		return ""
	}
	h.mu.Lock()
	mounts := append([]mount(nil), h.mounts...)
	h.mu.Unlock()
	root := h.cfg.Root
	var best *mount
	for i := range mounts {
		m := &mounts[i]
		dst := path.Clean(m.Destination)
		if dst != root && strings.HasPrefix(dst, strings.TrimSuffix(root, "/")+"/") {
			return "" // a volume or mount inside the workspace hides part of the folder
		}
		if dst == root || strings.HasPrefix(root, strings.TrimSuffix(dst, "/")+"/") {
			if best == nil || len(dst) > len(path.Clean(best.Destination)) {
				best = m
			}
		}
	}
	if best == nil || best.Type != "bind" || !best.RW || !filepath.IsAbs(best.Source) {
		return ""
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(root, path.Clean(best.Destination)), "/")
	local := filepath.Join(best.Source, filepath.FromSlash(rel))
	real, err := filepath.EvalSymlinks(local)
	if err != nil {
		return ""
	}
	if fi, err := os.Stat(real); err != nil || !fi.IsDir() {
		return ""
	}
	return real
}

type mountFS struct {
	h     *Host
	local string // the real local folder of the root
	lfs   workspace.FS
	cfs   containerFS
}

func newMountFS(h *Host, local string) (mountFS, error) {
	ws, err := localhost.New(local)
	if err != nil {
		return mountFS{}, err
	}
	return mountFS{h: h, local: local, lfs: ws.Host.FS(), cfs: containerFS{h}}, nil
}

var _ workspace.FS = mountFS{}

// rel maps p to a path relative to the root, and reports whether the
// local disk shows the same thing the container does: every symlink on
// the way (of the path, or of its nearest existing parent) stays inside
// the mount.
func (m mountFS) rel(p string) (string, bool, error) {
	abs, err := resolve(m.h.cfg.Root, p)
	if err != nil {
		return "", false, err
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(abs, m.h.cfg.Root), "/")
	if rel == "" {
		rel = "."
	}
	probe := filepath.Join(m.local, filepath.FromSlash(rel))
	for {
		if _, err := os.Lstat(probe); err == nil {
			// It exists here; a link that cannot be followed here
			// (dangling, or absolute) is a container matter.
			real, err := filepath.EvalSymlinks(probe)
			if err != nil {
				return rel, false, nil
			}
			inside := real == m.local || strings.HasPrefix(real, m.local+string(filepath.Separator))
			return rel, inside, nil
		}
		if probe == m.local {
			return rel, false, nil
		}
		probe = filepath.Dir(probe)
	}
}

func (m mountFS) Stat(ctx context.Context, p string) (fs.FileInfo, error) {
	rel, ok, err := m.rel(p)
	if err != nil {
		return nil, err
	}
	if !ok {
		return m.cfs.Stat(ctx, p)
	}
	return m.lfs.Stat(ctx, rel)
}

func (m mountFS) ReadDir(ctx context.Context, p string) ([]fs.DirEntry, error) {
	rel, ok, err := m.rel(p)
	if err != nil {
		return nil, err
	}
	if !ok {
		return m.cfs.ReadDir(ctx, p)
	}
	return m.lfs.ReadDir(ctx, rel)
}

func (m mountFS) ReadFile(ctx context.Context, p string) ([]byte, error) {
	rel, ok, err := m.rel(p)
	if err != nil {
		return nil, err
	}
	if !ok {
		return m.cfs.ReadFile(ctx, p)
	}
	return m.lfs.ReadFile(ctx, rel)
}

func (m mountFS) WriteFile(ctx context.Context, p string, data []byte, perm fs.FileMode) error {
	rel, ok, err := m.rel(p)
	if err != nil {
		return err
	}
	if !ok {
		return m.cfs.WriteFile(ctx, p, data, perm)
	}
	return m.lfs.WriteFile(ctx, rel, data, perm)
}

func (m mountFS) Mkdir(ctx context.Context, p string, perm fs.FileMode) error {
	rel, ok, err := m.rel(p)
	if err != nil {
		return err
	}
	if !ok {
		return m.cfs.Mkdir(ctx, p, perm)
	}
	return m.lfs.Mkdir(ctx, rel, perm)
}

func (m mountFS) Rename(ctx context.Context, from, to string) error {
	relFrom, okFrom, err := m.rel(from)
	if err != nil {
		return err
	}
	relTo, okTo, err := m.rel(to)
	if err != nil {
		return err
	}
	if !okFrom || !okTo {
		return m.cfs.Rename(ctx, from, to)
	}
	return m.lfs.Rename(ctx, relFrom, relTo)
}

func (m mountFS) Remove(ctx context.Context, p string) error {
	rel, ok, err := m.rel(p)
	if err != nil {
		return err
	}
	if !ok {
		return m.cfs.Remove(ctx, p)
	}
	return m.lfs.Remove(ctx, rel)
}
