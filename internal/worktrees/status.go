package worktrees

import (
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/git"
)

// StatusWorkers is how many git status calls run at once; StatusTTL is
// how long a value is fresh. 200 worktrees never mean 200 git processes
// at once, and the view draws from the cache (no git call per frame).
const (
	StatusWorkers = 4
	StatusTTL     = 10 * time.Second
)

// Status is a cached worktree status. Err is why it could not be read.
type Status struct {
	git.WorktreeStatus
	Err string
	At  time.Time
}

// StatusCache holds the status of each worktree by path.
type StatusCache struct {
	get func(path string) (git.WorktreeStatus, error)
	now func() time.Time
	sem chan struct{}

	mu       sync.Mutex
	vals     map[string]Status
	inflight map[string]bool
}

// NewStatusCache reads statuses with get (git.GetWorktreeStatus in the
// app, a fake in tests).
func NewStatusCache(get func(string) (git.WorktreeStatus, error)) *StatusCache {
	return &StatusCache{get: get, now: time.Now, sem: make(chan struct{}, StatusWorkers),
		vals: map[string]Status{}, inflight: map[string]bool{}}
}

// Get is the cached status of path (fresh or stale); ok is false until
// one read came back.
func (c *StatusCache) Get(path string) (Status, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, ok := c.vals[path]
	return st, ok
}

// Invalidate makes the next Refresh read path again.
func (c *StatusCache) Invalidate(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if st, ok := c.vals[path]; ok {
		st.At = time.Time{}
		c.vals[path] = st
	}
}

// Refresh reads the paths whose value is missing or older than StatusTTL
// and not being read already, in the background. It never blocks; done
// runs once (on a worker goroutine) when they are all in. It reports
// whether it started anything (done never runs when it did not).
func (c *StatusCache) Refresh(paths []string, done func()) bool {
	now := c.now()
	var todo []string
	c.mu.Lock()
	for _, p := range paths {
		if c.inflight[p] {
			continue
		}
		if st, ok := c.vals[p]; ok && !st.At.IsZero() && now.Sub(st.At) < StatusTTL {
			continue
		}
		c.inflight[p] = true
		todo = append(todo, p)
	}
	c.mu.Unlock()
	if len(todo) == 0 {
		return false
	}
	var wg sync.WaitGroup
	wg.Add(len(todo))
	for _, p := range todo {
		crash.GoWith("worktree status", func() {
			defer wg.Done()
			c.sem <- struct{}{}
			defer func() { <-c.sem }()
			st, err := c.get(p)
			v := Status{WorktreeStatus: st, At: c.now()}
			if err != nil {
				v.Err = err.Error()
			}
			c.mu.Lock()
			c.vals[p] = v
			delete(c.inflight, p)
			c.mu.Unlock()
		}, func() {
			c.mu.Lock()
			delete(c.inflight, p)
			c.mu.Unlock()
		})
	}
	crash.Go("worktree status batch", func() {
		wg.Wait()
		if done != nil {
			done()
		}
	})
	return true
}
