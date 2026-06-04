package app

import (
	"path/filepath"
	"sort"
	"strings"
)

// gitTreeNode is one node of the virtual Source-Control tree. Unlike the
// explorer tree (which mirrors the filesystem), this tree contains ONLY the
// directories that lead to a changed file — it's rebuilt from the flat
// status list and never touches disk.
type gitTreeNode struct {
	name      string // path segment shown in the row
	path      string // repo-relative path of this node ("" for the synthetic root)
	isDir     bool
	fileIndex int // index into Model.gitFiles for file nodes; -1 for dirs
	children  []*gitTreeNode
}

// gitVisRow is one rendered line of the Source-Control list. In flat mode
// every row is a file at depth 0. In tree mode directory headers are
// interleaved and Depth drives indentation. FileIndex links a file row back
// to Model.gitFiles (-1 for dir rows); DirPath is the collapse key for a
// dir row.
type gitVisRow struct {
	IsDir     bool
	Depth     int
	Name      string
	FileIndex int
	DirPath   string
}

// buildGitTree folds the flat list of changed files into a directory tree.
// Each path is split on "/" and walked from the root, creating the dir nodes
// it passes through on demand and hanging the file off the final segment.
//
// Design choice worth knowing: we do NOT compact single-child directory
// chains (VSCode does, rendering "a/b/c" as one row). Matching the file
// explorer's one-row-per-level layout was the ask, so each directory level
// gets its own collapsible row. To switch to compaction later, collapse any
// dir node that has exactly one dir child and no file children into its
// parent here.
// gitFileRef pairs a changed file's path with its index into Model.gitFiles,
// so a tree built from a SUBSET of files (e.g. only staged) still maps each
// file row back to the master list for stage/diff/discard actions.
type gitFileRef struct {
	path  string
	index int
}

func buildGitTree(files []gitFileRef) *gitTreeNode {
	root := &gitTreeNode{isDir: true, fileIndex: -1}
	for _, f := range files {
		segs := strings.Split(filepath.ToSlash(f.path), "/")
		cur := root
		for d, seg := range segs {
			if seg == "" {
				continue
			}
			if d == len(segs)-1 {
				cur.children = append(cur.children, &gitTreeNode{
					name:      seg,
					path:      f.path,
					isDir:     false,
					fileIndex: f.index,
				})
				break
			}
			var next *gitTreeNode
			for _, c := range cur.children {
				if c.isDir && c.name == seg {
					next = c
					break
				}
			}
			if next == nil {
				next = &gitTreeNode{
					name:      seg,
					path:      strings.Join(segs[:d+1], "/"),
					isDir:     true,
					fileIndex: -1,
				}
				cur.children = append(cur.children, next)
			}
			cur = next
		}
	}
	sortGitTree(root)
	return root
}

// sortGitTree orders each node's children dirs-first then alphabetically —
// the same ordering the file explorer uses, so the two trees read alike.
func sortGitTree(n *gitTreeNode) {
	sort.SliceStable(n.children, func(i, j int) bool {
		a, b := n.children[i], n.children[j]
		if a.isDir != b.isDir {
			return a.isDir
		}
		return a.name < b.name
	})
	for _, c := range n.children {
		if c.isDir {
			sortGitTree(c)
		}
	}
}

// gitVisibleRows renders ALL changed files (used by the standalone tree
// tests). The accordion uses gitVisibleRowsFor with staged/unstaged subsets.
func (m Model) gitVisibleRows() []gitVisRow {
	refs := make([]gitFileRef, len(m.gitFiles))
	for i, f := range m.gitFiles {
		refs[i] = gitFileRef{path: f.Path, index: i}
	}
	return m.gitVisibleRowsFor(refs)
}

// gitVisibleRowsFor flattens a file subset into display rows. Flat mode emits
// one row per file; tree mode walks the built tree, skipping the children of
// any collapsed directory. The returned rows carry master gitFiles indices.
func (m Model) gitVisibleRowsFor(refs []gitFileRef) []gitVisRow {
	if !m.gitViewTree {
		rows := make([]gitVisRow, len(refs))
		for j, r := range refs {
			rows[j] = gitVisRow{Depth: 0, Name: r.path, FileIndex: r.index}
		}
		return rows
	}
	root := buildGitTree(refs)
	var rows []gitVisRow
	var walk func(n *gitTreeNode, depth int)
	walk = func(n *gitTreeNode, depth int) {
		for _, c := range n.children {
			if c.isDir {
				rows = append(rows, gitVisRow{
					IsDir:     true,
					Depth:     depth,
					Name:      c.name,
					FileIndex: -1,
					DirPath:   c.path,
				})
				if !m.gitCollapsed[c.path] {
					walk(c, depth+1)
				}
			} else {
				rows = append(rows, gitVisRow{
					Depth:     depth,
					Name:      c.name,
					FileIndex: c.fileIndex,
				})
			}
		}
	}
	walk(root, 0)
	return rows
}

// gitClampCursor keeps gitCursor inside the current row set after the row
// count changes (collapse/expand, mode toggle, a fresh status fetch), and
// nudges it off any non-selectable connector line onto the nearest row.
func (m *Model) gitClampCursor() {
	rows := m.gitPanelRows()
	if len(rows) == 0 {
		m.gitCursor = 0
		return
	}
	m.gitCursor = clampInt(m.gitCursor, 0, len(rows)-1)
	if !rows[m.gitCursor].selectable() {
		m.gitCursor = gitNearestSelectable(rows, m.gitCursor)
	}
}

// gitToggleCollapse flips the collapsed state of a directory row and
// re-clamps the cursor (collapsing a dir above the cursor shrinks the list).
func (m *Model) gitToggleCollapse(dir string) {
	if m.gitCollapsed == nil {
		m.gitCollapsed = map[string]bool{}
	}
	m.gitCollapsed[dir] = !m.gitCollapsed[dir]
	m.gitClampCursor()
}

// toggleGitViewMode switches between tree and flat list, re-clamps the
// cursor, and persists the choice so it survives a relaunch.
func (m *Model) toggleGitViewMode() {
	m.gitViewTree = !m.gitViewTree
	m.gitClampCursor()
	m.persistGitViewTree()
}

// persistGitViewTree writes the tree/flat preference into the session file.
// Mirrors persistTerminalRows / persistExplorerState.
func (m *Model) persistGitViewTree() {
	existing := loadSession()
	v := m.gitViewTree
	existing.GitViewTree = &v
	saveSession(existing)
}
