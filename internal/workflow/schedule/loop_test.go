package schedule

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeClock: time moves only on Advance; After waits are visible.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []fakeWait
}

type fakeWait struct {
	at time.Time
	ch chan time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, fakeWait{at: c.now.Add(d), ch: ch})
	return ch
}

// Advance moves time and fires the waits that are due (a late wake after
// a suspend is one big Advance).
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	keep := c.waiters[:0]
	for _, w := range c.waiters {
		if w.at.After(c.now) {
			keep = append(keep, w)
			continue
		}
		w.ch <- c.now
	}
	c.waiters = keep
}

// waitFor waits until n waits are pending and returns their deadlines.
func (c *fakeClock) waitFor(t *testing.T, n int) []time.Time {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		if len(c.waiters) >= n {
			var ats []time.Time
			for _, w := range c.waiters {
				ats = append(ats, w.at)
			}
			c.mu.Unlock()
			return ats
		}
		c.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("want %d pending waits", n)
	return nil
}

func newLoop(t *testing.T, start string) (*Scheduler, *fakeClock, *recorder, chan Due) {
	t.Helper()
	fc := &fakeClock{now: at(t, start)}
	rec := &recorder{}
	fired := make(chan Due, 16)
	s := New(Options{Clock: fc, OnEvent: rec.add, Fire: func(_ context.Context, d Due) error {
		fired <- d
		return nil
	}})
	t.Cleanup(s.Stop)
	return s, fc, rec, fired
}

func TestLoopFiresOnFakeClock(t *testing.T) {
	s, fc, rec, fired := newLoop(t, "08:59:00")
	if err := s.Add(Job{ID: "w", Schedule: compile(t, "daily 09:00")}); err != nil {
		t.Fatal(err)
	}
	s.Start()
	// 60 s to the slot; the wait is capped at MaxSleep so a suspend that
	// stops timers is noticed soon after the wake.
	if ats := fc.waitFor(t, 1); !ats[0].Equal(at(t, "08:59:30")) {
		t.Fatalf("first wait until %v, want 08:59:30", ats[0])
	}
	fc.Advance(30 * time.Second)
	if ats := fc.waitFor(t, 1); !ats[0].Equal(at(t, "09:00:00")) {
		t.Fatalf("second wait until %v, want 09:00:00", ats[0])
	}
	fc.Advance(30 * time.Second)
	d := <-fired
	if d.ID != "w" || !d.Slot.Equal(at(t, "09:00:00")) || d.Late {
		t.Fatalf("fired %+v", d)
	}
	rec.waitFor(t, 1)
	select {
	case d := <-fired:
		t.Fatalf("fired twice: %+v", d)
	case <-time.After(20 * time.Millisecond):
	}
}

// TestLoopWakeAfterSleep: a laptop suspended over three slots wakes,
// records the old one as missed and runs the one within the grace.
func TestLoopWakeAfterSleep(t *testing.T) {
	s, fc, rec, fired := newLoop(t, "08:59:00")
	_ = s.Add(Job{ID: "w", Schedule: compile(t, "hourly")})
	s.Start()
	fc.waitFor(t, 1)
	fc.Advance(2*time.Hour + 15*time.Minute) // 11:14
	d := <-fired
	if !d.Slot.Equal(at(t, "11:00:00")) || !d.Late {
		t.Fatalf("fired %+v", d)
	}
	evs := rec.waitFor(t, 2)
	if evs[0].Outcome != Missed || !evs[0].Slot.Equal(at(t, "09:00:00")) || evs[1].Outcome != Ran {
		t.Fatalf("events = %+v", evs)
	}
}

// TestLoopIdleNoTimer: with no job the loop sets no timer (no idle
// wakeups); Add wakes it.
func TestLoopIdleNoTimer(t *testing.T) {
	s, fc, _, fired := newLoop(t, "08:59:00")
	if _, ok := s.sleepFor(fc.Now()); ok {
		t.Fatal("no jobs, but the loop would set a timer")
	}
	s.Start()
	time.Sleep(10 * time.Millisecond)
	fc.mu.Lock()
	n := len(fc.waiters)
	fc.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d timers with no job", n)
	}
	_ = s.Add(Job{ID: "w", Schedule: compile(t, "daily 09:00")})
	fc.waitFor(t, 1)
	fc.Advance(time.Minute)
	if d := <-fired; !d.Slot.Equal(at(t, "09:00:00")) {
		t.Fatalf("fired %+v", d)
	}
	s.Remove("w")
	if _, ok := s.sleepFor(fc.Now()); ok {
		t.Fatal("job removed, but the loop would set a timer")
	}
}

func TestLoopStop(t *testing.T) {
	s, fc, _, _ := newLoop(t, "08:59:00")
	_ = s.Add(Job{ID: "w", Schedule: compile(t, "daily 09:00")})
	s.Start()
	s.Start() // twice starts one loop
	fc.waitFor(t, 1)
	time.Sleep(10 * time.Millisecond)
	if ats := fc.waitFor(t, 1); len(ats) != 1 {
		t.Fatalf("%d timers, want 1 loop", len(ats))
	}
	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not end the loop")
	}
}
