package explorer

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

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
	// workspace, ADR 0018); nil = the local disk. Children inherit it,
	// and ld, the queue its listings go through off the UI loop
	// (fs_load.go). loading: a listing of the folder runs. root: the
	// workspace root of ld.
	fsys    workspace.FS
	ld      *fsLoader
	loading bool
	root    bool
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
// folder is one listing call; entries are never stat'ed one by one. It
// reads nothing itself: the root's listing is asked (fs_load.go) and an
// error comes with its answer.
func NewRootFS(fsys workspace.FS, root string) (*Node, error) {
	root = path.Clean(root)
	n := &Node{
		Name:     path.Base(root),
		Path:     root,
		IsDir:    true,
		Expanded: true,
		fsys:     fsys,
		ld:       newFSLoader(),
		root:     true,
	}
	_ = n.Load()
	return n, nil
}

// FS is the workspace FS the node is listed through (nil = local disk).
func (n *Node) FS() workspace.FS { return n.fsys }

func (n *Node) readDir() ([]os.DirEntry, error) {
	if n.fsys != nil {
		return n.fsys.ReadDir(context.Background(), n.Path)
	}
	// f.ReadDir, not os.ReadDir: that one sorts by name first, and Load
	// sorts anyway.
	f, err := os.Open(n.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}

func (n *Node) Load() error {
	if !n.IsDir {
		return nil
	}
	if n.ld != nil { // a workspace FS: asked, read off the UI loop
		if !n.loading {
			touch()
		}
		n.ld.ask(n.Path)
		n.loading = true
		return nil
	}
	entries, err := n.readDir()
	if err != nil {
		return err
	}
	touch()
	// One block for all the nodes, and the child paths built by hand:
	// a name from ReadDir has no separator, so a Join's Clean is not
	// needed (a folder of 5 000 files loads 2x faster).
	sep := string(filepath.Separator)
	if n.fsys != nil {
		sep = "/"
	}
	dir := n.Path
	if !strings.HasSuffix(dir, sep) {
		dir += sep
	}
	nodes := make([]Node, len(entries))
	n.Children = make([]*Node, len(entries))
	for i, e := range entries {
		nodes[i] = Node{Name: e.Name(), Path: dir + e.Name(), IsDir: e.IsDir(), fsys: n.fsys}
		n.Children[i] = &nodes[i]
	}
	slices.SortFunc(n.Children, func(a, b *Node) int {
		if a.IsDir != b.IsDir {
			if a.IsDir {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
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
//
// It reports whether a folder it needs is still being read (a workspace
// FS: the caller runs it again when the listing arrives).
func (n *Node) ExpandPaths(set map[string]bool) bool {
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
	return n.expandPaths(set, need)
}

func (n *Node) expandPaths(set, need map[string]bool) bool {
	if !n.IsDir || !need[n.Path] {
		return false
	}
	if !n.Loaded {
		_ = n.Load()
	}
	if set[n.Path] && !n.Expanded {
		touch()
		n.Expanded = true
	}
	waiting := !n.Loaded && n.ld != nil
	for _, c := range n.Children {
		if c.expandPaths(set, need) {
			waiting = true
		}
	}
	return waiting
}
