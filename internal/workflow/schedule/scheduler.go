package schedule

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/crash"
)

// Outcome is what happened to one slot of a workflow.
type Outcome string

const (
	Ran             Outcome = "ran"
	Failed          Outcome = "failed" // Fire returned an error (or was cancelled)
	SkippedPrecheck Outcome = "skipped (precheck)"
	SkippedRunning  Outcome = "skipped (still running)"
	Missed          Outcome = "missed"
)

const (
	// DefaultGrace is workflows_missed_grace_min (30).
	DefaultGrace = 30 * time.Minute
	// OnTime: a slot fired within this of its time is not late (tick jitter).
	OnTime = time.Minute
	// DefaultPrecheckTimeout caps a precheck.
	DefaultPrecheckTimeout = 30 * time.Second
	// MaxSleep caps the loop's wait: timers stop during a suspend, so the
	// loop looks again at least this often (the 30 s idle tick).
	MaxSleep = 30 * time.Second
	// maxCatchUp bounds the walk over in-grace slots.
	maxCatchUp = 10000
)

// Job is one scheduled workflow.
type Job struct {
	ID       string
	Schedule *Schedule
	// LastRun is the last slot already handled (from the state file). Zero
	// is a new job: nothing before now is caught up.
	LastRun time.Time
	// Precheck runs before each fire; an error records the slot as
	// skipped (precheck). The context has a deadline. Nil: no precheck.
	Precheck func(ctx context.Context) error
}

// Due is one fire of a workflow.
type Due struct {
	ID   string
	Slot time.Time
	// Late: fired after OnTime past the slot (on start or after a wake).
	Late bool
}

// Event is one handled slot. The caller records it (run history, the
// state file's last_run).
type Event struct {
	ID      string
	Slot    time.Time
	Outcome Outcome
	Late    bool
	Err     error
}

// Clock is the time source of the loop (tests inject a fake one).
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return clock.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return clock.After(d) }

// Options configure a Scheduler.
type Options struct {
	// Clock: nil is the real clock.
	Clock Clock
	// Grace: a slot missed by less than this still runs once, late.
	// 0 is DefaultGrace; negative: only slots within OnTime run.
	Grace time.Duration
	// Fire runs the workflow and returns when the run ends (so a slot
	// that comes while it runs is skipped). Its context ends on Remove or
	// Stop.
	Fire func(ctx context.Context, d Due) error
	// OnEvent gets every handled slot; it is called from several
	// goroutines. Nil: events are dropped.
	OnEvent func(Event)
	// PrecheckTimeout: 0 is DefaultPrecheckTimeout.
	PrecheckTimeout time.Duration
}

type job struct {
	Job
	next    time.Time // the first slot not handled yet; zero: none
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
}

// Scheduler fires due workflows. Tick does the work; it is safe to call
// from several goroutines: one slot fires at most once.
type Scheduler struct {
	opts   Options
	window time.Duration
	clock  Clock
	now    func() time.Time
	wake   chan struct{}

	mu      sync.Mutex
	jobs    map[string]*job
	stopped bool
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// New makes a Scheduler.
func New(o Options) *Scheduler {
	if o.PrecheckTimeout <= 0 {
		o.PrecheckTimeout = DefaultPrecheckTimeout
	}
	window := o.Grace
	switch {
	case window == 0:
		window = DefaultGrace
	case window < OnTime:
		window = OnTime
	}
	c := o.Clock
	if c == nil {
		c = realClock{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{opts: o, window: window, clock: c, now: c.Now,
		wake: make(chan struct{}, 1), jobs: map[string]*job{}, ctx: ctx, cancel: cancel}
}

// Start runs the loop: Tick, then sleep until the next slot (at most
// MaxSleep), or with no job until Add. Later calls do nothing.
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.started || s.stopped {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.wg.Add(1)
	s.mu.Unlock()
	select { // the loop's first Tick sees every Add before Start
	case <-s.wake:
	default:
	}
	crash.Go("workflow/schedule.loop", func() {
		defer s.wg.Done()
		s.loop()
	})
}

func (s *Scheduler) loop() {
	for {
		now := s.clock.Now()
		s.Tick(now)
		var timer <-chan time.Time
		if d, ok := s.sleepFor(now); ok {
			timer = s.clock.After(d)
		}
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
		case <-timer:
		}
	}
}

// sleepFor is the wait until the earliest next slot, capped at MaxSleep;
// false: no job, so no timer (no idle wakeups).
func (s *Scheduler) sleepFor(now time.Time) (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var first time.Time
	for _, j := range s.jobs {
		if !j.next.IsZero() && (first.IsZero() || j.next.Before(first)) {
			first = j.next
		}
	}
	if first.IsZero() {
		return 0, false
	}
	d := first.Sub(now)
	if d > MaxSleep {
		d = MaxSleep
	}
	if d < time.Millisecond {
		d = time.Millisecond
	}
	return d, true
}

func (s *Scheduler) poke() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Add schedules a job. Its first slot is the first after LastRun (or
// after now for a new job); the next Tick catches up by the grace rule.
func (s *Scheduler) Add(j Job) error {
	if j.ID == "" {
		return errors.New("schedule: job has no id")
	}
	if j.Schedule == nil {
		return errors.New("schedule: job " + j.ID + " has no schedule")
	}
	now := s.now()
	from := j.LastRun
	if from.IsZero() || from.After(now) {
		from = now
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return errors.New("schedule: scheduler is stopped")
	}
	if _, dup := s.jobs[j.ID]; dup {
		return errors.New("schedule: job " + j.ID + " is already scheduled")
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.jobs[j.ID] = &job{Job: j, next: j.Schedule.Next(from), ctx: ctx, cancel: cancel}
	s.poke()
	return nil
}

// Remove unschedules a job and cancels its running fire.
func (s *Scheduler) Remove(id string) {
	s.mu.Lock()
	j := s.jobs[id]
	delete(s.jobs, id)
	s.mu.Unlock()
	if j != nil {
		j.cancel()
		s.poke()
	}
}

// NextRun is the job's next slot (for the list's "next run").
func (s *Scheduler) NextRun(id string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.jobs[id]
	if j == nil || j.next.IsZero() {
		return time.Time{}, false
	}
	return j.next, true
}

// Stop cancels every running fire and waits for them to end. Later
// calls do nothing.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	s.stopped = true
	s.jobs = map[string]*job{}
	s.mu.Unlock()
	s.cancel()
	s.wg.Wait()
}

// Tick handles every slot due at now: the latest slot within the grace
// fires (or is skipped), older ones are recorded as one missed event.
func (s *Scheduler) Tick(now time.Time) {
	var evs []Event
	type launch struct {
		j *job
		d Due
	}
	var runs []launch

	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	ids := make([]string, 0, len(s.jobs))
	for id := range s.jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		j := s.jobs[id]
		if j.next.IsZero() || j.next.After(now) {
			continue
		}
		slot, missed := s.catchUp(j, now)
		j.next = j.Schedule.Next(now) // every slot up to now is handled
		if !missed.IsZero() {
			evs = append(evs, Event{ID: id, Slot: missed, Outcome: Missed})
		}
		if slot.IsZero() {
			continue
		}
		late := now.Sub(slot) > OnTime
		if j.running {
			evs = append(evs, Event{ID: id, Slot: slot, Outcome: SkippedRunning, Late: late})
			continue
		}
		j.running = true
		s.wg.Add(1)
		runs = append(runs, launch{j, Due{ID: id, Slot: slot, Late: late}})
	}
	s.mu.Unlock()

	for _, e := range evs {
		s.emit(e)
	}
	for _, r := range runs {
		r := r
		crash.Go("workflow/schedule.fire", func() { s.fire(r.j, r.d) })
	}
}

// catchUp finds the slot to fire (the latest within the window, zero if
// none) and the first missed slot (zero if none) up to now.
func (s *Scheduler) catchUp(j *job, now time.Time) (slot, missed time.Time) {
	start := now.Add(-s.window)
	slot = j.next
	if slot.Before(start) {
		missed = slot
		slot = j.Schedule.Next(start.Add(-time.Nanosecond))
		if slot.IsZero() || slot.After(now) {
			return time.Time{}, missed
		}
	}
	for i := 0; i < maxCatchUp; i++ {
		n := j.Schedule.Next(slot)
		if n.IsZero() || n.After(now) {
			break
		}
		slot = n
	}
	return slot, missed
}

func (s *Scheduler) fire(j *job, d Due) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		j.running = false
		s.mu.Unlock()
	}()
	e := Event{ID: d.ID, Slot: d.Slot, Late: d.Late}
	if j.Precheck != nil {
		ctx, cancel := context.WithTimeout(j.ctx, s.opts.PrecheckTimeout)
		err := j.Precheck(ctx)
		cancel()
		if err != nil {
			e.Outcome, e.Err = SkippedPrecheck, err
			s.emit(e)
			return
		}
	}
	var err error
	if s.opts.Fire != nil {
		err = s.opts.Fire(j.ctx, d)
	}
	e.Outcome = Ran
	if err != nil {
		e.Outcome, e.Err = Failed, err
	}
	s.emit(e)
}

func (s *Scheduler) emit(e Event) {
	if s.opts.OnEvent != nil {
		s.opts.OnEvent(e)
	}
}
