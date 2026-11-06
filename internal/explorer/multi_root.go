package explorer

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// Multi-root workspace support.
//
// The explorer's primary root (m.root) is the cwd the user launched termocode
// in — git, terminal, format-on-save and other "single project" features stay
// pinned to it. Multi-root adds *additional* roots stored in m.extraRoots,
// each rendered as its own depth-0 section under the primary root.
//
// visible() walks the primary first, then each extra in order, concatenating
// their per-root Visible() slices. The existing tree expand/collapse logic
// handles the rest — each extra root is just another *Node, identical in
// shape to the primary, so the renderer can't tell them apart.

// Roots returns the absolute paths of every root currently in the explorer,
// starting with the primary (cwd-based) root.
func (m Model) Roots() []string {
	var out []string
	if m.root != nil {
		out = append(out, m.root.Path)
	}
	for _, r := range m.extraRoots {
		if r != nil {
			out = append(out, r.Path)
		}
	}
	return out
}

// PrimaryRoot returns the primary (cwd-based) root path. Empty string if
// the explorer never initialised successfully.
func (m Model) PrimaryRoot() string {
	if m.root == nil {
		return ""
	}
	return m.root.Path
}

// AddRoot loads `path` as a new directory root and appends it to the
// explorer's extra-roots list. The primary root and any already-added extras
// are left untouched. Returns an error if path can't be statted, isn't a
// directory, or is already present (primary or extra).
//
// The extras of an explorer whose primary root is listed through a
// workspace FS (a container) live on that FS too: path must be absolute
// there, and the local disk is never read.
func (m *Model) AddRoot(path string) error {
	if m.root != nil && m.root.fsys != nil {
		return m.addRootFS(path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	// Dedup: skip if already loaded as primary or as an extra. Compare by
	// cleaned absolute path so trailing slashes / .. don't slip through.
	abs = filepath.Clean(abs)
	if m.root != nil && filepath.Clean(m.root.Path) == abs {
		return nil
	}
	for _, r := range m.extraRoots {
		if r != nil && filepath.Clean(r.Path) == abs {
			return nil
		}
	}
	node, err := NewRoot(abs)
	if err != nil {
		return err
	}
	if !node.IsDir {
		return fmt.Errorf("%s is not a folder", abs)
	}
	m.extraRoots = append(m.extraRoots, node)
	m.scrollIntoView()
	return nil
}

// addRootFS is AddRoot on the primary root's workspace FS.
func (m *Model) addRootFS(p string) error {
	if !path.IsAbs(p) {
		return fmt.Errorf("%s is not an absolute path", p)
	}
	p = path.Clean(p)
	if path.Clean(m.root.Path) == p {
		return nil
	}
	for _, r := range m.extraRoots {
		if r != nil && path.Clean(r.Path) == p {
			return nil
		}
	}
	fsys := m.root.fsys
	st, err := fsys.Stat(context.Background(), p)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not a folder", p)
	}
	node, err := NewRootFS(fsys, p)
	if err != nil {
		return err
	}
	m.extraRoots = append(m.extraRoots, node)
	m.scrollIntoView()
	return nil
}

// RemoveRoot drops `path` from the extra-roots list. The primary root cannot
// be removed (returns false). Returns true if a root was actually removed.
func (m *Model) RemoveRoot(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	abs = filepath.Clean(abs)
	if m.root != nil && filepath.Clean(m.root.Path) == abs {
		// Primary root can't be removed.
		return false
	}
	for i, r := range m.extraRoots {
		if r != nil && filepath.Clean(r.Path) == abs {
			m.extraRoots = append(m.extraRoots[:i], m.extraRoots[i+1:]...)
			if c := len(m.visible()); c > 0 && m.cursor >= c {
				m.cursor = c - 1
			}
			m.scrollIntoView()
			return true
		}
	}
	return false
}

// allRoots returns every loaded root node (primary first, then extras).
// nil-safe — skips an unset primary.
func (m Model) allRoots() []*Node {
	out := make([]*Node, 0, 1+len(m.extraRoots))
	if m.root != nil {
		out = append(out, m.root)
	}
	for _, r := range m.extraRoots {
		if r != nil {
			out = append(out, r)
		}
	}
	return out
}

// findRootForPath returns the root whose Path is an ancestor of (or equal
// to) abs. Returns nil if no root matches.
func (m Model) findRootForPath(abs string) *Node {
	for _, r := range m.allRoots() {
		if abs == r.Path {
			return r
		}
		if strings.HasPrefix(abs, r.Path+string(filepath.Separator)) {
			return r
		}
	}
	return nil
}
