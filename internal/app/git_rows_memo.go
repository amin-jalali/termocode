package app

import (
	"slices"
	"sort"
	"strings"
	"sync"
	"unsafe"
)

// gitFileRowsKey is everything gitFileRows reads. m.gitFiles is only
// ever replaced as a whole (a new status), so its backing array and
// length stand for its content.
type gitFileRowsKey struct {
	files                         unsafe.Pointer
	n                             int
	tree, rebase                  bool
	conflictsC, stagedC, changesC bool
	collapsed                     string // the folded folders
}

var gitRowsMemo struct {
	sync.Mutex
	fileKey   gitFileRowsKey
	fileOK    bool
	fileRows  []gitPanelRow
	joinOK    bool
	joinFrom  []gitPanelRow // the prefix the joined list was built on
	joinGraph []gitPanelRow
	joined    []gitPanelRow
}

func (m Model) gitFileRowsKey() gitFileRowsKey {
	var folded []string
	for dir, on := range m.gitCollapsed {
		if on {
			folded = append(folded, dir)
		}
	}
	sort.Strings(folded)
	return gitFileRowsKey{
		files: unsafe.Pointer(unsafe.SliceData(m.gitFiles)), n: len(m.gitFiles),
		tree: m.gitViewTree, rebase: m.gitRebase.InProgress,
		conflictsC: m.gitConflictsCollapsed, stagedC: m.gitStagedCollapsed, changesC: m.gitChangesCollapsed,
		collapsed: strings.Join(folded, "\x00"),
	}
}

// gitFileRowsCached is gitFileRows, reused while its inputs are the
// same. The returned slice is shared: callers must not change it.
func (m Model) gitFileRowsCached() []gitPanelRow {
	k := m.gitFileRowsKey()
	c := &gitRowsMemo
	c.Lock()
	defer c.Unlock()
	if c.fileOK && c.fileKey == k {
		return c.fileRows
	}
	c.fileKey, c.fileRows, c.fileOK = k, m.gitFileRows(), true
	return c.fileRows
}

// gitJoinRows returns prefix followed by graph, reusing the last joined
// list when both are the same as last time. The result is shared:
// callers must not change it.
func gitJoinRows(prefix, graph []gitPanelRow) []gitPanelRow {
	c := &gitRowsMemo
	c.Lock()
	defer c.Unlock()
	if c.joinOK && len(c.joinFrom) == len(prefix) && unsafe.SliceData(c.joinFrom) == unsafe.SliceData(prefix) &&
		slices.Equal(c.joinGraph, graph) {
		return c.joined
	}
	out := make([]gitPanelRow, 0, len(prefix)+len(graph))
	out = append(append(out, prefix...), graph...)
	c.joinOK, c.joinFrom, c.joinGraph, c.joined = true, prefix, graph, out
	return out
}
