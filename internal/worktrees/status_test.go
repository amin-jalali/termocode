package worktrees

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/git"
)

// fakeStatus is a status reader that counts calls and how many run at
// once; gate (when set) holds every call until it is closed.
type fakeStatus struct {
	calls, now, peak atomic.Int32
	gate             chan struct{}
}

func (f *fakeStatus) get(path string) (git.WorktreeStatus, error) {
	f.calls.Add(1)
	n := f.now.Add(1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	if f.gate != nil {
		<-f.gate
	}
	f.now.Add(-1)
	if path == "/bad" {
		return git.WorktreeStatus{}, errors.New("not a git repository")
	}
	return git.WorktreeStatus{Dirty: len(path), Ahead: 1}, nil
}

// Refresh never blocks: it starts at most 4 git calls at once, the done
// callback runs once when the batch is in, and Get then has every value.
func TestStatusCacheWorkers(t *testing.T) {
	f := &fakeStatus{gate: make(chan struct{})}
	c := NewStatusCache(f.get)
	var paths []string
	for i := 0; i < 12; i++ {
		paths = append(paths, fmt.Sprintf("/w/%02d", i))
	}
	paths = append(paths, "/bad")
	var done sync.WaitGroup
	done.Add(1)
	t0 := time.Now()
	if !c.Refresh(paths, done.Done) {
		t.Fatal("nothing started")
	}
	if d := time.Since(t0); d > 50*time.Millisecond {
		t.Errorf("Refresh blocked %v", d)
	}
	if _, ok := c.Get("/w/00"); ok {
		t.Error("a value before any call came back")
	}
	close(f.gate)
	done.Wait()
	if p := f.peak.Load(); p > StatusWorkers {
		t.Errorf("%d git calls at once, want at most %d", p, StatusWorkers)
	}
	if st, ok := c.Get("/w/03"); !ok || st.Dirty != 5 || st.Ahead != 1 || st.Err != "" {
		t.Errorf("got %+v %v", st, ok)
	}
	if st, ok := c.Get("/bad"); !ok || st.Err == "" {
		t.Errorf("bad: %+v %v", st, ok)
	}
}

// Within the TTL a refresh starts nothing (no git call per frame); after
// it the paths are read again; Invalidate forces one path.
func TestStatusCacheTTL(t *testing.T) {
	f := &fakeStatus{}
	c := NewStatusCache(f.get)
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }
	wait := func(paths ...string) bool {
		var wg sync.WaitGroup
		wg.Add(1)
		if !c.Refresh(paths, wg.Done) {
			return false
		}
		wg.Wait()
		return true
	}
	if !wait("/a", "/b") || f.calls.Load() != 2 {
		t.Fatalf("first: %d calls", f.calls.Load())
	}
	now = now.Add(StatusTTL - time.Second)
	if wait("/a", "/b") || f.calls.Load() != 2 {
		t.Errorf("inside the TTL: %d calls", f.calls.Load())
	}
	c.Invalidate("/a")
	if !wait("/a", "/b") || f.calls.Load() != 3 {
		t.Errorf("after Invalidate: %d calls", f.calls.Load())
	}
	now = now.Add(StatusTTL + time.Second)
	if !wait("/a", "/b") || f.calls.Load() != 5 {
		t.Errorf("after the TTL: %d calls", f.calls.Load())
	}
	// The stale value stays readable while the new one is read.
	if st, ok := c.Get("/b"); !ok || st.Dirty != 2 {
		t.Errorf("stale value: %+v %v", st, ok)
	}
}

// A path already being read is not read twice at once.
func TestStatusCacheNoDoubleRead(t *testing.T) {
	f := &fakeStatus{gate: make(chan struct{})}
	c := NewStatusCache(f.get)
	var wg sync.WaitGroup
	wg.Add(1)
	c.Refresh([]string{"/a"}, wg.Done)
	if c.Refresh([]string{"/a"}, func() { t.Error("second batch ran") }) {
		t.Error("a path in flight started again")
	}
	close(f.gate)
	wg.Wait()
	if f.calls.Load() != 1 {
		t.Errorf("%d calls", f.calls.Load())
	}
}
