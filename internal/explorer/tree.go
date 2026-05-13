package explorer

import (
	"os"
	"path/filepath"
	"sort"
)

type Node struct {
	Name     string
	Path     string
	IsDir    bool
	Children []*Node
	Expanded bool
	Loaded   bool
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

func (n *Node) Load() error {
	if !n.IsDir {
		return nil
	}
	entries, err := os.ReadDir(n.Path)
	if err != nil {
		return err
	}
	n.Children = nil
	for _, e := range entries {
		n.Children = append(n.Children, &Node{
			Name:  e.Name(),
			Path:  filepath.Join(n.Path, e.Name()),
			IsDir: e.IsDir(),
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
func (n *Node) ExpandPaths(set map[string]bool) {
	if !n.IsDir {
		return
	}
	if !n.Loaded {
		_ = n.Load()
	}
	if set[n.Path] {
		n.Expanded = true
	}
	for _, c := range n.Children {
		c.ExpandPaths(set)
	}
}
