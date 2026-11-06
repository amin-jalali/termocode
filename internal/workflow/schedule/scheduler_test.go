package schedule

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// recorder collects the scheduler's events.
type recorder struct {
	mu  sync.Mutex
	evs []Event
}

func (r *recorder) add(e Event) {
	r.mu.Lock()
	r.evs = append(r.evs, e)
	r.mu.Unlock()
}

func (r *recorder) all() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.evs...)
}

// waitFor waits until n events arrived.
func (r *recorder) waitFor(t *testing.T, n int) []Event {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if evs := r.all(); len(evs) >= n {
			return evs
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("want %d events, got %+v", n, r.all())
	return nil
}

func compile(t *testing.T, text string) *Schedule {
	t.Helper()
	sp, err := ParseText(text)
	if err != nil {
		t.Fatal(err)
	}
	sp.TZ = "UTC"
	s, err := Compile(sp)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func at(t *testing.T, hhmmss string) time.Time {
	t.Helper()
	return mustTime(t, "2026-11-06T"+hhmmss+"Z")
}

// newTest builds a Scheduler whose Fire counts calls and returns fire().
func newTest(t *testing.T, grace time.Duration, fire func(context.Context, Due) error) (*Scheduler, *recorder) {
	t.Helper()
	rec := &recorder{}
	s := New(Options{Grace: grace, Fire: fire, OnEvent: rec.add})
	t.Cleanup(s.Stop)
	return s, rec
}

func TestSchedulerFiresDueSlotOnce(t *testing.T) {
	var fired []Due
	var mu sync.Mutex
	s, rec := newTest(t, 0, func(_ context.Context, d Due) error {
		mu.Lock()
		fired = append(fired, d)
		mu.Unlock()
		return nil
	})
	s.now = func() time.Time { return at(t, "08:00:00") }
	if err := s.Add(Job{ID: "bugs", Schedule: compile(t, "daily 09:00")}); err != nil {
		t.Fatal(err)
	}
	if n, ok := s.NextRun("bugs"); !ok || !n.Equal(at(t, "09:00:00")) {
		t.Fatalf("NextRun = %v %v", n, ok)
	}
	s.Tick(at(t, "08:59:59"))
	s.Tick(at(t, "09:00:01"))
	s.Tick(at(t, "09:00:30"))
	evs := rec.waitFor(t, 1)
	s.wg.Wait()
	if len(evs) != 1 || evs[0].Outcome != Ran || !evs[0].Slot.Equal(at(t, "09:00:00")) || evs[0].Late || evs[0].ID != "bugs" {
		t.Fatalf("events = %+v", evs)
	}
	if len(fired) != 1 || fired[0].Late || !fired[0].Slot.Equal(at(t, "09:00:00")) {
		t.Fatalf("fired = %+v", fired)
	}
	if n, _ := s.NextRun("bugs"); !n.Equal(mustTime(t, "2026-11-07T09:00:00Z")) {
		t.Fatalf("NextRun after run = %v", n)
	}
}

// TestSchedulerMissedGrace: on start (and on wake) a slot due within the
// grace runs once, late; older slots are recorded as missed.
func TestSchedulerMissedGrace(t *testing.T) {
	type want struct {
		outcome Outcome
		slot    string
		late    bool
	}
	cases := []struct {
		name    string
		text    string
		grace   time.Duration
		lastRun string // "" = a new job
		now     string
		want    []want
	}{
		{"within grace runs late", "daily 09:00", 0, "2026-11-05T09:00:00Z", "09:10:00",
			[]want{{Ran, "09:00:00", true}}},
		{"past grace is missed", "daily 09:00", 0, "2026-11-05T09:00:00Z", "09:45:00",
			[]want{{Missed, "09:00:00", false}}},
		{"days offline: one missed, nothing runs", "daily 09:00", 0, "2026-11-01T09:00:00Z", "08:00:00",
			[]want{{Missed, "2026-11-02T09:00:00Z", false}}},
		{"older missed, latest runs once", "hourly", 0, "2026-11-06T06:00:00Z", "09:10:00",
			[]want{{Missed, "07:00:00", false}, {Ran, "09:00:00", true}}},
		{"in-grace slots run once (latest)", "*/10 * * * *", 0, "2026-11-06T08:40:00Z", "09:05:00",
			[]want{{Ran, "09:00:00", true}}},
		{"custom grace", "daily 09:00", 2 * time.Hour, "2026-11-05T09:00:00Z", "10:30:00",
			[]want{{Ran, "09:00:00", true}}},
		{"no grace: on time still runs", "daily 09:00", -1, "2026-11-05T09:00:00Z", "09:00:30",
			[]want{{Ran, "09:00:00", false}}},
		{"no grace: late is missed", "daily 09:00", -1, "2026-11-05T09:00:00Z", "09:05:00",
			[]want{{Missed, "09:00:00", false}}},
		{"new job does not catch up", "daily 09:00", 0, "", "09:10:00", nil},
		{"last run in the future (clock moved back)", "daily 09:00", 0, "2026-11-07T09:00:00Z", "09:10:00", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, rec := newTest(t, c.grace, func(context.Context, Due) error { return nil })
			now := at(t, c.now)
			s.now = func() time.Time { return now }
			j := Job{ID: "w", Schedule: compile(t, c.text)}
			if c.lastRun != "" {
				j.LastRun = mustTime(t, c.lastRun)
			}
			if err := s.Add(j); err != nil {
				t.Fatal(err)
			}
			s.Tick(now)
			s.wg.Wait()
			evs := rec.all()
			if len(evs) != len(c.want) {
				t.Fatalf("events = %+v, want %+v", evs, c.want)
			}
			for i, w := range c.want {
				slot := w.slot
				if len(slot) == 8 {
					slot = "2026-11-06T" + slot + "Z"
				}
				e := evs[i]
				if e.Outcome != w.outcome || !e.Slot.Equal(mustTime(t, slot)) || e.Late != w.late {
					t.Fatalf("event %d = %+v, want %+v", i, e, w)
				}
			}
			if n, ok := s.NextRun("w"); !ok || !n.After(now) {
				t.Fatalf("NextRun = %v %v, want after %v", n, ok, now)
			}
		})
	}
}

func TestSchedulerPrecheckSkip(t *testing.T) {
	var fires atomic.Int32
	s, rec := newTest(t, 0, func(context.Context, Due) error { fires.Add(1); return nil })
	s.now = func() time.Time { return at(t, "08:00:00") }
	hadDeadline := false
	bad := errors.New("gh auth status: exit 1")
	err := s.Add(Job{ID: "w", Schedule: compile(t, "daily 09:00"), Precheck: func(ctx context.Context) error {
		_, hadDeadline = ctx.Deadline()
		return bad
	}})
	if err != nil {
		t.Fatal(err)
	}
	s.Tick(at(t, "09:00:00"))
	evs := rec.waitFor(t, 1)
	s.wg.Wait()
	if evs[0].Outcome != SkippedPrecheck || !errors.Is(evs[0].Err, bad) {
		t.Fatalf("event = %+v", evs[0])
	}
	if fires.Load() != 0 {
		t.Fatal("Fire ran after a failed precheck")
	}
	if !hadDeadline {
		t.Fatal("precheck context has no deadline")
	}

	// A passing precheck lets the run go.
	s2, rec2 := newTest(t, 0, func(context.Context, Due) error { fires.Add(1); return nil })
	s2.now = func() time.Time { return at(t, "08:00:00") }
	_ = s2.Add(Job{ID: "w", Schedule: compile(t, "daily 09:00"), Precheck: func(context.Context) error { return nil }})
	s2.Tick(at(t, "09:00:00"))
	if evs := rec2.waitFor(t, 1); evs[0].Outcome != Ran || fires.Load() != 1 {
		t.Fatalf("events = %+v, fires = %d", evs, fires.Load())
	}
}

// TestSchedulerOverlapSkip: concurrency 1 per workflow.
func TestSchedulerOverlapSkip(t *testing.T) {
	release := make(chan struct{})
	started := make(chan Due, 4)
	s, rec := newTest(t, 0, func(_ context.Context, d Due) error {
		started <- d
		<-release
		return errors.New("tests failed")
	})
	s.now = func() time.Time { return at(t, "08:30:00") }
	_ = s.Add(Job{ID: "w", Schedule: compile(t, "hourly")})
	s.Tick(at(t, "09:00:00"))
	<-started
	s.Tick(at(t, "10:00:00"))
	evs := rec.waitFor(t, 1)
	if evs[0].Outcome != SkippedRunning || !evs[0].Slot.Equal(at(t, "10:00:00")) {
		t.Fatalf("event = %+v", evs[0])
	}
	close(release)
	evs = rec.waitFor(t, 2)
	if evs[1].Outcome != Failed || !evs[1].Slot.Equal(at(t, "09:00:00")) || evs[1].Err == nil {
		t.Fatalf("event = %+v", evs[1])
	}
	s.wg.Wait()
	s.Tick(at(t, "11:00:00")) // free again
	if evs := rec.waitFor(t, 3); evs[2].Outcome != Failed || !evs[2].Slot.Equal(at(t, "11:00:00")) {
		t.Fatalf("event = %+v", evs[2])
	}
}

// TestSchedulerNoDoubleFire: many ticks at once fire one slot once.
func TestSchedulerNoDoubleFire(t *testing.T) {
	var fires atomic.Int32
	s, rec := newTest(t, 0, func(context.Context, Due) error { fires.Add(1); return nil })
	s.now = func() time.Time { return at(t, "08:00:00") }
	for _, id := range []string{"a", "b", "c"} {
		_ = s.Add(Job{ID: id, Schedule: compile(t, "daily 09:00")})
	}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.Tick(at(t, "09:00:00").Add(time.Duration(i) * time.Second / 2))
		}(i)
	}
	wg.Wait()
	s.wg.Wait()
	if n := fires.Load(); n != 3 {
		t.Fatalf("fired %d times, want 3 (one per workflow); events %+v", n, rec.all())
	}
	if evs := rec.all(); len(evs) != 3 {
		t.Fatalf("events = %+v", evs)
	}
}

func TestSchedulerRemoveCancels(t *testing.T) {
	started := make(chan struct{})
	s, rec := newTest(t, 0, func(ctx context.Context, _ Due) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	s.now = func() time.Time { return at(t, "08:00:00") }
	_ = s.Add(Job{ID: "w", Schedule: compile(t, "daily 09:00")})
	s.Tick(at(t, "09:00:00"))
	<-started
	s.Remove("w")
	evs := rec.waitFor(t, 1)
	if evs[0].Outcome != Failed || !errors.Is(evs[0].Err, context.Canceled) {
		t.Fatalf("event = %+v", evs[0])
	}
	if _, ok := s.NextRun("w"); ok {
		t.Fatal("removed job still has a next run")
	}
	s.Tick(mustTime(t, "2026-11-07T09:00:00Z"))
	s.wg.Wait()
	if len(rec.all()) != 1 {
		t.Fatalf("removed job fired: %+v", rec.all())
	}
	if err := s.Add(Job{ID: "w", Schedule: compile(t, "daily 09:00")}); err != nil {
		t.Fatalf("re-add after remove: %v", err)
	}
}

func TestSchedulerStopCancelsAndWaits(t *testing.T) {
	started := make(chan struct{})
	var done atomic.Bool
	s, rec := newTest(t, 0, func(ctx context.Context, _ Due) error {
		close(started)
		<-ctx.Done()
		time.Sleep(10 * time.Millisecond)
		done.Store(true)
		return ctx.Err()
	})
	s.now = func() time.Time { return at(t, "08:00:00") }
	_ = s.Add(Job{ID: "w", Schedule: compile(t, "daily 09:00")})
	s.Tick(at(t, "09:00:00"))
	<-started
	s.Stop()
	if !done.Load() {
		t.Fatal("Stop returned before the run ended")
	}
	if evs := rec.all(); len(evs) != 1 || evs[0].Outcome != Failed {
		t.Fatalf("events = %+v", evs)
	}
	if err := s.Add(Job{ID: "x", Schedule: compile(t, "daily 09:00")}); err == nil {
		t.Fatal("Add after Stop: want an error")
	}
	s.Tick(mustTime(t, "2026-11-07T09:00:00Z")) // no panic, no fire
	s.Stop()                                    // twice is fine
}

func TestSchedulerAddErrors(t *testing.T) {
	s, _ := newTest(t, 0, func(context.Context, Due) error { return nil })
	if err := s.Add(Job{Schedule: compile(t, "hourly")}); err == nil {
		t.Error("empty id: want an error")
	}
	if err := s.Add(Job{ID: "w"}); err == nil {
		t.Error("nil schedule: want an error")
	}
	if err := s.Add(Job{ID: "w", Schedule: compile(t, "hourly")}); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(Job{ID: "w", Schedule: compile(t, "hourly")}); err == nil {
		t.Error("duplicate id: want an error")
	}
}
