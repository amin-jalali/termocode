package app

import (
	"path/filepath"
	"sort"
	"strings"

	"termocode/internal/git"
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
func buildGitTree(files []git.FileStatus) *gitTreeNode {
	root := &gitTreeNode{isDir: true, fileIndex: -1}
	for i, f := range files {
		segs := strings.Split(filepath.ToSlash(f.Path), "/")
		cur := root
		for d, seg := range segs {
			if seg == "" {
				continue
			}
			if d == len(segs)-1 {
				cur.children = append(cur.children, &gitTreeNode{
					name:      seg,
					path:      f.Path,
					isDir:     false,
					fileIndex: i,
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

// gitVisibleRows is the single source of truth for what the Source-Control
// list shows, in both modes. Flat mode emits one file row per gitFiles
// entry; tree mode walks the built tree, skipping the children of any
// collapsed directory. gitCursor and the mouse hit-test both index into
// this slice, so the two modes share all navigation logic.
func (m Model) gitVisibleRows() []gitVisRow {
	if !m.gitViewTree {
		rows := make([]gitVisRow, len(m.gitFiles))
		for i, f := range m.gitFiles {
			rows[i] = gitVisRow{Depth: 0, Name: f.Path, FileIndex: i}
		}
		return rows
	}
	root := buildGitTree(m.gitFiles)
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
