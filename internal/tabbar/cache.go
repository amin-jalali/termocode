package tabbar

import (
	"sync"

	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Render caches. The tab bar is drawn on every frame and its width is
// measured on every buffer update (tabWidths renders each tab), but it
// changes only when the tabs, the active tab, focus, width, scroll or the
// theme change. Both caches are keyed by exactly those inputs.

// cellsKey is everything renderTabCellsSfx reads.
type cellsKey struct {
	b       nvim.BufferInfo
	active  bool
	suffix  string
	welcome bool
	maxW    int
	rk      theme.RenderKey
}

// maxCellsCache bounds the tab-cells cache; past it the cache starts
// over (a few dozen tabs is the normal size).
const maxCellsCache = 256

var cellsCache struct {
	sync.Mutex
	m map[cellsKey][]string
}

// tabCellsCached is renderTabCellsSfx through the cache. The returned
// slice is shared: callers must not modify it.
func tabCellsCached(b nvim.BufferInfo, active bool, suffix string, welcome bool, maxW int) []string {
	k := cellsKey{b: b, active: active, suffix: suffix, welcome: welcome, maxW: maxW, rk: theme.CurrentRenderKey()}
	cellsCache.Lock()
	defer cellsCache.Unlock()
	if c, ok := cellsCache.m[k]; ok {
		return c
	}
	if cellsCache.m == nil || len(cellsCache.m) >= maxCellsCache {
		cellsCache.m = make(map[cellsKey][]string)
	}
	c := renderTabCellsSfx(b, active, suffix, welcome, maxW)
	cellsCache.m[k] = c
	return c
}

// viewKey is everything View reads.
type viewKey struct {
	bufs     []nvim.BufferInfo
	activeID int
	w        int
	first    int
	blurred  bool
	rk       theme.RenderKey
}

func (a *viewKey) equal(b *viewKey) bool {
	if a.activeID != b.activeID || a.w != b.w || a.first != b.first || a.blurred != b.blurred ||
		a.rk != b.rk || len(a.bufs) != len(b.bufs) {
		return false
	}
	for i := range a.bufs {
		if a.bufs[i] != b.bufs[i] {
			return false
		}
	}
	return true
}

var viewCache struct {
	sync.Mutex
	ok  bool
	key viewKey
	out string
}
