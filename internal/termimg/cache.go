package termimg

import (
	"sync"
	"time"

	"github.com/muesli/termenv"
)

// CacheKey identifies one rendering of one file.
type CacheKey struct {
	Path    string
	Mod     time.Time
	Size    int64
	Cols    int
	Rows    int
	Proto   Protocol
	Profile termenv.Profile
}

// Entry is a cached rendering: the half-block lines (always present, they
// are the base layer) and the graphics sequence for Kitty/Sixel (empty for
// Blocks).
type Entry struct {
	Lines []string
	Seq   string
	ImgW  int // source image size in pixels
	ImgH  int
}

// Cache is a small LRU so moving between files or resizing does not
// re-decode and re-encode images on every frame. It holds at most max
// entries and about maxCacheBytes of rendered text (a Kitty / Sixel
// sequence of a large image is megabytes); the newest entry always stays.
type Cache struct {
	mu    sync.Mutex
	max   int
	order []CacheKey // most recent last
	items map[CacheKey]Entry
	bytes int // sum of entrySize over items
}

// maxCacheBytes bounds the memory a Cache keeps for closed images.
const maxCacheBytes = 16 << 20

// entrySize is the bytes an entry holds (its strings).
func entrySize(e Entry) int {
	n := len(e.Seq)
	for _, l := range e.Lines {
		n += len(l)
	}
	return n
}

// NewCache returns a cache holding at most max entries (default 16).
func NewCache(max int) *Cache {
	if max <= 0 {
		max = 16
	}
	return &Cache{max: max, items: map[CacheKey]Entry{}}
}

// Get returns the entry for k and marks it recently used.
func (c *Cache) Get(k CacheKey) (Entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[k]
	if ok {
		c.touch(k)
	}
	return e, ok
}

// Put stores e under k, evicting the least recently used entry when full.
func (c *Cache) Put(k CacheKey, e Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.items[k]; ok {
		c.bytes -= entrySize(old)
		c.items[k] = e
		c.bytes += entrySize(e)
		c.touch(k)
	} else {
		c.items[k] = e
		c.bytes += entrySize(e)
		c.order = append(c.order, k)
	}
	for len(c.order) > c.max || (c.bytes > maxCacheBytes && len(c.order) > 1) {
		old := c.order[0]
		c.order = c.order[1:]
		c.bytes -= entrySize(c.items[old])
		delete(c.items, old)
	}
}

// Bytes returns the bytes held by the cached entries.
func (c *Cache) Bytes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bytes
}

// Len returns the number of cached entries.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

func (c *Cache) touch(k CacheKey) {
	for i, o := range c.order {
		if o == k {
			copy(c.order[i:], c.order[i+1:])
			c.order[len(c.order)-1] = k
			return
		}
	}
}
