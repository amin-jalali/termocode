package session

import (
	"testing"
	"time"
)

// fakeClock moves both clocks together, or only the wall clock (a sleep).
type fakeClock struct{ r clockReading }

func newFakeClock() *fakeClock {
	return &fakeClock{clockReading{mono: time.Hour, wall: time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)}}
}

func (c *fakeClock) run(d time.Duration) clockReading {
	c.r.mono += d
	c.r.wall = c.r.wall.Add(d)
	return c.r
}

func (c *fakeClock) sleep(d time.Duration) clockReading {
	c.r.wall = c.r.wall.Add(d)
	return c.r
}

// TestLinkMonitorHeartbeat: a ping every 5 s; a link that answers stays
// up; one silent for 15 s is gone.
func TestLinkMonitorHeartbeat(t *testing.T) {
	c := newFakeClock()
	m := newLinkMonitor(5*time.Second, 15*time.Second, c.r)
	pings := 0
	for range 30 { // 30 s, answered every second
		now := c.run(time.Second)
		m.Received(now)
		ping, dead, woke := m.Tick(now)
		if dead || woke {
			t.Fatalf("an answering link: dead=%v woke=%v", dead, woke)
		}
		if ping {
			pings++
		}
	}
	if pings != 6 {
		t.Errorf("%d pings in 30 s, want 6", pings)
	}
	for i := 1; i <= 15; i++ { // now silent
		_, dead, _ := m.Tick(c.run(time.Second))
		if dead != (i >= 15) {
			t.Fatalf("silent for %d s: dead=%v", i, dead)
		}
	}
}

// TestLinkMonitorWake: after a sleep (the wall clock jumped, the
// monotonic one did not) the link is checked at once: a ping now, and it
// is gone 5 s later without an answer — not 15 s.
func TestLinkMonitorWake(t *testing.T) {
	c := newFakeClock()
	m := newLinkMonitor(5*time.Second, 15*time.Second, c.r)
	m.Tick(c.run(time.Second))
	m.Received(c.r)
	c.sleep(40 * time.Minute)
	ping, dead, woke := m.Tick(c.run(time.Second))
	if !woke || !ping || dead {
		t.Fatalf("first tick after the sleep: ping=%v dead=%v woke=%v", ping, dead, woke)
	}
	for i := 1; i <= 5; i++ {
		_, dead, woke := m.Tick(c.run(time.Second))
		if woke {
			t.Fatalf("woke again %d s later", i)
		}
		if dead != (i >= 5) {
			t.Fatalf("%d s after the wake: dead=%v", i, dead)
		}
	}

	// An answer after the wake keeps the link.
	c = newFakeClock()
	m = newLinkMonitor(5*time.Second, 15*time.Second, c.r)
	c.sleep(time.Hour)
	if _, _, woke := m.Tick(c.run(time.Second)); !woke {
		t.Fatal("no wake")
	}
	m.Received(c.run(200 * time.Millisecond))
	for range 10 {
		if _, dead, _ := m.Tick(c.run(time.Second)); dead {
			t.Fatal("a link that answered after the wake is gone")
		}
		m.Received(c.r)
	}

	// A small clock step (NTP) is no wake.
	c = newFakeClock()
	m = newLinkMonitor(5*time.Second, 15*time.Second, c.r)
	c.sleep(2 * time.Second)
	if _, _, woke := m.Tick(c.run(time.Second)); woke {
		t.Error("a 2 s clock step counted as a wake")
	}
}
