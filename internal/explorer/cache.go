package explorer

import (
	"maps"
	"sync"
	"sync/atomic"

	"github.com/amin-jalali/termocode/internal/anim"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Render cache. The sidebar is drawn on every frame but changes only when
// the tree, the selection, the Model's own fields or View's arguments
// change. View keeps its last result keyed by all of them:
//
//   - treeGen: bumped by every change to shared state that a Model copy
//     does not hold by value — Node fields (Load, Toggle, expand, reveal)
//     and the selection set (a map shared between copies). Nodes are only
//     changed inside this package (touch at each place).
//   - the Model's value fields (cursor, scroll, size, meta, roots, …),
//   - View's arguments (focus, active file, dirty flag, git status), and
//   - the theme / icon mode / colour profile (theme.RenderKey).
//
// cache_test.go checks a cached View against a fresh draw after every
// step of random explorer operations.
var treeGen atomic.Uint64

// touch records a change to the tree or the selection.
func touch() { treeGen.Add(1) }

type viewKey struct {
	gen         uint64
	root        *Node
	extraRoots  []*Node
	cursor, top int
	w, h        int
	err         string
	showHidden  bool
	meta        [2]string // workspace, branch
	metaIsRepo  bool
	metaFiles   int
	scroll      anim.Value
	animOn      bool
	focused     bool
	activePath  string
	activeDirty bool
	git         GitStatus
	rk          theme.RenderKey
}

func (m *Model) viewKey(focused bool, activePath string, activeDirty bool, gs GitStatus) viewKey {
	return viewKey{
		gen: treeGen.Load(), root: m.root, extraRoots: m.extraRoots,
		cursor: m.cursor, top: m.top, w: m.w, h: m.h, err: m.err, showHidden: m.showHidden,
		meta: [2]string{m.metaWorkspace, m.metaBranch}, metaIsRepo: m.metaIsRepo, metaFiles: m.metaTotalFiles,
		scroll: m.scroll, animOn: anim.Enabled,
		focused: focused, activePath: activePath, activeDirty: activeDirty, git: gs,
		rk: theme.CurrentRenderKey(),
	}
}

func (a *viewKey) equal(b *viewKey) bool {
	if a.gen != b.gen || a.root != b.root || a.cursor != b.cursor || a.top != b.top ||
		a.w != b.w || a.h != b.h || a.err != b.err || a.showHidden != b.showHidden ||
		a.meta != b.meta || a.metaIsRepo != b.metaIsRepo || a.metaFiles != b.metaFiles ||
		a.scroll != b.scroll || a.animOn != b.animOn || a.focused != b.focused ||
		a.activePath != b.activePath || a.activeDirty != b.activeDirty || a.rk != b.rk ||
		len(a.extraRoots) != len(b.extraRoots) || (a.git == nil) != (b.git == nil) {
		return false
	}
	for i := range a.extraRoots {
		if a.extraRoots[i] != b.extraRoots[i] {
			return false
		}
	}
	return maps.Equal(a.git, b.git)
}

var viewCache struct {
	sync.Mutex
	ok  bool
	key viewKey
	out string
}

// countKey is what CountVisibleFiles reads.
type countKey struct {
	gen        uint64
	root       *Node
	extraRoots []*Node
	showHidden bool
}

var countCache struct {
	sync.Mutex
	ok  bool
	key countKey
	n   int
}

func (a *countKey) equal(b *countKey) bool {
	if a.gen != b.gen || a.root != b.root || a.showHidden != b.showHidden || len(a.extraRoots) != len(b.extraRoots) {
		return false
	}
	for i := range a.extraRoots {
		if a.extraRoots[i] != b.extraRoots[i] {
			return false
		}
	}
	return true
}
