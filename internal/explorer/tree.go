package explorer

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/amin-jalali/termocode/internal/workspace"
)

type Node struct {
	Name     string
	Path     string
	IsDir    bool
	Children []*Node
	Expanded bool
	Loaded   bool

	// fsys lists a folder that is not on this machine (a container
	// workspace, ADR 0018); nil = the local disk. Children inherit it.
	fsys workspace.FS
}

func NewRoot(path string) (*Node, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	n := &Node{
		Name:     filepath.Base(abs),
		Path:     abs,
		IsDir:    info.IsDir(),
		Expanded: info.IsDir(),
	}
	if n.IsDir {
		if err := n.Load(); err != nil {
			return nil, err
		}
	}
	return n, nil
}

// NewRootFS is NewRoot for the folder root reached through a workspace
// FS (a container): Path values are the host's own slash paths. Each
// folder is one listing call; entries are never stat'ed one by one.
func NewRootFS(fsys workspace.FS, root string) (*Node, error) {
	root = path.Clean(root)
	info, err := fsys.Stat(context.Background(), root)
	if err != nil {
		return nil, err
	}
	n := &Node{
		Name:     path.Base(root),
		Path:     root,
		IsDir:    info.IsDir(),
		Expanded: info.IsDir(),
		fsys:     fsys,
	}
	if n.IsDir {
		if err := n.Load(); err != nil {
			return nil, err
		}
	}
	return n, nil
}

// FS is the workspace FS the node is listed through (nil = local disk).
func (n *Node) FS() workspace.FS { return n.fsys }

func (n *Node) readDir() ([]os.DirEntry, error) {
	if n.fsys != nil {
		return n.fsys.ReadDir(context.Background(), n.Path)
	}
	return os.ReadDir(n.Path)
}

func (n *Node) join(name string) string {
	if n.fsys != nil {
		return path.Join(n.Path, name)
	}
	return filepath.Join(n.Path, name)
}

func (n *Node) Load() error {
	if !n.IsDir {
		return nil
	}
	entries, err := n.readDir()
	if err != nil {
		return err
	}
	touch()
	n.Children = nil
	for _, e := range entries {
		n.Children = append(n.Children, &Node{
			Name:  e.Name(),
			Path:  n.join(e.Name()),
			IsDir: e.IsDir(),
			fsys:  n.fsys,
		})
	}
	sort.SliceStable(n.Children, func(i, j int) bool {
		a, b := n.Children[i], n.Children[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		return a.Name < b.Name
	})
	n.Loaded = true
	return nil
}

func (n *Node) Toggle() error {
	if !n.IsDir {
		return nil
	}
	if !n.Loaded {
		if err := n.Load(); err != nil {
			return err
		}
	}
	touch()
	n.Expanded = !n.Expanded
	return nil
}

type VisibleEntry struct {
	Node  *Node
	Depth int
}

func (n *Node) Visible() []VisibleEntry {
	var out []VisibleEntry
	var walk func(*Node, int)
	walk = func(node *Node, depth int) {
		out = append(out, VisibleEntry{Node: node, Depth: depth})
		if node.IsDir && node.Expanded {
			for _, c := range node.Children {
				walk(c, depth+1)
			}
		}
	}
	walk(n, 0)
	return out
}

// ExpandedPaths walks the tree and returns the absolute paths of every
// currently-expanded directory. Suitable for persistence.
func (n *Node) ExpandedPaths() []string {
	var out []string
	var walk func(*Node)
	walk = func(node *Node) {
		if node.IsDir && node.Expanded {
			out = append(out, node.Path)
			for _, c := range node.Children {
				walk(c)
			}
		}
	}
	walk(n)
	return out
}

// ExpandPaths re-expands the directory paths in `set`, lazily loading
// each directory's children as needed. Paths whose ancestors aren't
// loaded yet are loaded on demand.
//
// Only the listed directories and their ancestors are read; other folders
// stay unloaded (this used to read the whole tree, every folder, on each
// session restore).
func (n *Node) ExpandPaths(set map[string]bool) {
	need := make(map[string]bool, len(set)*4)
	for p := range set {
		for d := p; ; {
			if need[d] {
				break
			}
			need[d] = true
			parent := filepath.Dir(d)
			if parent == d {
				break
			}
			d = parent
		}
	}
	n.expandPaths(set, need)
}

func (n *Node) expandPaths(set, need map[string]bool) {
	if !n.IsDir || !need[n.Path] {
		return
	}
	if !n.Loaded {
		_ = n.Load()
	}
	if set[n.Path] {
		touch()
		n.Expanded = true
	}
	for _, c := range n.Children {
		c.expandPaths(set, need)
	}
}
