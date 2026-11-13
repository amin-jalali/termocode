// Package lspmirror keeps a local, read-only copy of a container
// workspace's code so a local language server can read it (plan RC,
// phase 5). The copy lives in ~/.cache/termocode/mirror/<key>/ (0700),
// is filled from one tar stream of the workspace root, leaves out the
// heavy folders (node_modules, .venv, vendor, target, __pycache__,
// .git/objects), stops at a size cap, and is updated on every save from
// termocode. The container is only read; the mirror is never the source
// of truth.
package lspmirror

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultCap is the most one mirror holds (512 MB).
const DefaultCap int64 = 512 << 20

// DefaultTotalCap is the most all mirrors together hold (2 GB); the
// least recently used ones go first (Prune).
const DefaultTotalCap int64 = 2 << 30

// Excluded are the folder names left out at any depth.
var Excluded = []string{"node_modules", ".venv", "vendor", "target", "__pycache__"}

// ErrCap is returned by Fill when the copy reached its size cap; what
// was copied so far stays.
var ErrCap = errors.New("the mirror reached its size limit")

// BaseDir is ~/.cache/termocode/mirror (or $XDG_CACHE_HOME/…).
func BaseDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "termocode", "mirror"), nil
}

// Mirror is one workspace's local copy.
type Mirror struct {
	// Remote is the workspace root inside the container (/app).
	Remote string
	// Dir is the local folder of the copy.
	Dir string
	// Cap is the size limit (0 = DefaultCap).
	Cap int64

	mu    sync.Mutex
	bytes int64
	files int
}

// New is the mirror of remote in base/key. Nothing is created yet.
func New(base, key, remote string) *Mirror {
	return &Mirror{Remote: path.Clean(remote), Dir: filepath.Join(base, key)}
}

// Stats is what a Fill copied.
type Stats struct {
	Files int
	Bytes int64
}

// Stats is the current size of the copy.
func (m *Mirror) Stats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Stats{Files: m.files, Bytes: m.bytes}
}

func (m *Mirror) cap() int64 {
	if m.Cap > 0 {
		return m.Cap
	}
	return DefaultCap
}

// SkipPath reports whether rel (slash path, relative to the root) is
// left out of the mirror.
func SkipPath(rel string) bool {
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		for _, x := range Excluded {
			if p == x {
				return true
			}
		}
		if p == ".git" && i+1 < len(parts) && parts[i+1] == "objects" {
			return true
		}
	}
	return false
}

// Fill replaces the copy with the tar stream r of the workspace root
// (`docker cp <c>:/app -`: every name starts with the root's base
// name). Only regular files and folders are copied; links, devices
// and names that would leave the folder are skipped. At the cap it
// stops with ErrCap and keeps what it has.
func (m *Mirror) Fill(r io.Reader) (Stats, error) {
	if err := m.Clear(); err != nil {
		return Stats{}, err
	}
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		return Stats{}, err
	}
	if err := os.Chmod(m.Dir, 0o700); err != nil {
		return Stats{}, err
	}
	tr := tar.NewReader(r)
	var st Stats
	limit := m.cap()
	for {
		hd, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			m.setStats(st)
			return st, err
		}
		rel, ok := stripFirst(hd.Name)
		if !ok || rel == "" || SkipPath(rel) {
			continue
		}
		dst := filepath.Join(m.Dir, filepath.FromSlash(rel))
		switch hd.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o700); err != nil {
				m.setStats(st)
				return st, err
			}
		case tar.TypeReg:
			if st.Bytes+hd.Size > limit {
				m.setStats(st)
				return st, ErrCap
			}
			if err := writeFrom(dst, tr, hd.Size); err != nil {
				m.setStats(st)
				return st, err
			}
			st.Files++
			st.Bytes += hd.Size
		}
	}
	m.setStats(st)
	m.touch()
	return st, nil
}

func (m *Mirror) setStats(st Stats) {
	m.mu.Lock()
	m.files, m.bytes = st.Files, st.Bytes
	m.mu.Unlock()
}

// touch marks the mirror as used now (Prune keeps recent ones).
func (m *Mirror) touch() {
	now := time.Now()
	_ = os.Chtimes(m.Dir, now, now)
}

// stripFirst drops the first element of a tar name and cleans the
// rest; ok is false for a name that climbs out.
func stripFirst(name string) (string, bool) {
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	_, rest, _ := strings.Cut(name, "/")
	if rest == "" {
		return "", true
	}
	rest = path.Clean(rest)
	if rest == ".." || strings.HasPrefix(rest, "../") || path.IsAbs(rest) {
		return "", false
	}
	return rest, true
}

func writeFrom(dst string, r io.Reader, n int64) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.CopyN(f, r, n); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Local maps a container path under Remote to its mirror file (ok
// false: outside the root or left out).
func (m *Mirror) Local(remote string) (string, bool) {
	p := path.Clean(remote)
	var rel string
	switch {
	case p == m.Remote:
		return m.Dir, true
	case m.Remote == "/":
		rel = strings.TrimPrefix(p, "/")
	default:
		r, ok := strings.CutPrefix(p, m.Remote+"/")
		if !ok {
			return "", false
		}
		rel = r
	}
	if rel == "" || SkipPath(rel) {
		return "", false
	}
	return filepath.Join(m.Dir, filepath.FromSlash(rel)), true
}

// Put updates one file after a save from termocode. A mirror that was
// never filled, or a path it leaves out, is a no-op.
func (m *Mirror) Put(remote string, data []byte) error {
	if _, err := os.Stat(m.Dir); err != nil {
		return nil
	}
	dst, ok := m.Local(remote)
	if !ok || dst == m.Dir {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var old int64
	existed := false
	if fi, err := os.Stat(dst); err == nil {
		old, existed = fi.Size(), true
	}
	if m.bytes-old+int64(len(data)) > m.cap() {
		return ErrCap
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return err
	}
	if !existed {
		m.files++
	}
	m.bytes += int64(len(data)) - old
	return nil
}

// Remove drops one file or folder after a delete from termocode.
func (m *Mirror) Remove(remote string) error {
	dst, ok := m.Local(remote)
	if !ok || dst == m.Dir {
		return nil
	}
	return os.RemoveAll(dst)
}

// Rename follows a rename or move from termocode: the copy moves to the
// new name, or goes when the new name is left out (or outside the root).
func (m *Mirror) Rename(from, to string) error {
	src, ok := m.Local(from)
	if !ok || src == m.Dir {
		return nil
	}
	if _, err := os.Lstat(src); err != nil {
		return nil
	}
	dst, ok := m.Local(to)
	if !ok || dst == m.Dir {
		return os.RemoveAll(src)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	_ = os.RemoveAll(dst)
	return os.Rename(src, dst)
}

// Clear deletes the copy (a missing one is fine).
func (m *Mirror) Clear() error {
	if m.Dir == "" || m.Dir == "/" {
		return fmt.Errorf("lspmirror: refusing to clear %q", m.Dir)
	}
	m.setStats(Stats{})
	if err := os.RemoveAll(m.Dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Prune deletes the least recently used mirrors in base until all of
// them together fit in total. keep is never deleted.
func Prune(base string, total int64, keep string) error {
	entries, err := os.ReadDir(base)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	type dir struct {
		path string
		size int64
		used time.Time
	}
	var dirs []dir
	var sum int64
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(base, e.Name())
		fi, err := e.Info()
		if err != nil {
			continue
		}
		sz := dirSize(p)
		sum += sz
		dirs = append(dirs, dir{p, sz, fi.ModTime()})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].used.Before(dirs[j].used) })
	for _, d := range dirs {
		if sum <= total {
			break
		}
		if filepath.Clean(d.path) == filepath.Clean(keep) {
			continue
		}
		if err := os.RemoveAll(d.path); err != nil {
			return err
		}
		sum -= d.size
	}
	return nil
}

func dirSize(p string) int64 {
	var n int64
	_ = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}
