// Package run is the workflow engine (plan §4): it runs the steps of a
// parsed workflow.File in order, item by item, with when / else,
// on_error, cancel, events and dry runs.
//
// The package is pure. Every step that touches the world is an adapter
// passed in Options (Source, Planner, Filter, Each, Runner, Writer); the
// expression language is the Evaluator. It starts one goroutine per run
// (crash.Go) and does no I/O of its own.
package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/workflow"
)

// Options are the parts a run uses. Eval is required; a step whose
// adapter is nil fails with a clear error.
type Options struct {
	// RunID names the run ("" = the start time, 20060102-150405).
	RunID string
	// Dry runs without effects: sources read (for real), plan and filter
	// run, run steps are only shown ("would run"), writes are only
	// recorded in RunFile.Writes.
	Dry bool
	// NoPlan skips plan steps (dry run --no-plan: free).
	NoPlan bool
	// Vars override the file's vars (--var k=v).
	Vars map[string]any
	// LastOKAt is run.last_ok_at ("" = never).
	LastOKAt string

	Eval   Evaluator
	Source Source
	Plan   Planner
	Filter Filter // nil: ExprFilter
	Each   Each   // nil: no item environment
	Run    Runner
	Write  Writer
	// Wait backs wait steps (nil: they fail).
	Wait Waiter

	// ParallelMax caps the items an each runs at once
	// (workflows_parallel_max; 0: no cap beyond the file's).
	ParallelMax int

	// Tools tells whether an MCP tool is read-only, before any mcp source
	// or write calls it (nil: never known, so a source asks).
	Tools ToolCheck
	// Ask asks the user before an mcp source calls a tool that is not
	// read-only; true runs it (nil: no).
	Ask func(ctx context.Context, c AskCall) (bool, error)
	// Approve asks the user before the order of a plan step with
	// "approve": true is used; true goes on, false denies the step (nil:
	// such a step fails; it never goes on unapproved).
	Approve func(ctx context.Context, c ApproveCall) (bool, error)

	// OnEvent gets every event, one at a time and in order per item (the
	// items of a parallel each run on their own goroutines). It must not
	// block for long.
	OnEvent func(Event)
	// Now is the clock (nil: time.Now).
	Now func() time.Time
	// Store saves the run file at every boundary (nil: not saved).
	Store RunFileWriter
	// Resume continues a run from its run file (after Recover): finished
	// steps and items are kept, the others run again. The file must be the
	// one the run started with (same hash).
	Resume *RunFile
	// Sleep waits between tries (nil: a timer that ends early on ctx).
	Sleep func(ctx context.Context, d time.Duration) error
}

// Handle is a started run.
type Handle struct {
	cancel context.CancelFunc
	done   chan struct{}
	r      *runner
}

// Cancel stops the run: every context is cancelled; the run ends
// cancelled. Safe to call more than once and after the end.
func (h *Handle) Cancel() { h.cancel() }

// Pause holds the run: the step that works now finishes, and no new
// step starts until Continue. Safe after the end (nothing happens).
func (h *Handle) Pause() { h.r.hold(true) }

// Continue lets a held run go on.
func (h *Handle) Continue() { h.r.hold(false) }

// Held reports whether the user paused the run.
func (h *Handle) Held() bool {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.r.rf.Held
}

// Done is closed when the run has ended.
func (h *Handle) Done() <-chan struct{} { return h.done }

// Wait waits for the end and returns the final state.
func (h *Handle) Wait() *RunFile {
	<-h.done
	return h.Snapshot()
}

// Snapshot is a copy of the run's state now.
func (h *Handle) Snapshot() *RunFile {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.r.rf.clone()
}

// Start begins a run of file in its own goroutine. It never blocks on
// the steps; watch Options.OnEvent or Handle.Done.
func Start(ctx context.Context, file *workflow.File, opts Options) *Handle {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Filter == nil {
		opts.Filter = ExprFilter{}
	}
	if opts.Sleep == nil {
		opts.Sleep = sleep
	}
	now := opts.Now()
	id := opts.RunID
	if id == "" {
		id = now.UTC().Format("20060102-150405")
	}
	vars := map[string]any{}
	maps.Copy(vars, file.Vars)
	maps.Copy(vars, opts.Vars)

	rf := &RunFile{
		Version: RunFileVersion, ID: id, Workflow: file.Name, Hash: file.Hash,
		Dry: opts.Dry, State: RunRunning, StartedAt: now, Vars: vars,
		Steps: map[string]*StepState{},
	}
	var resumeErr error
	if opts.Resume != nil {
		rf = opts.Resume.clone()
		if rf.Hash != file.Hash {
			resumeErr = errors.New("the workflow file changed since this run started; start a new run")
		}
		rf.State, rf.EndedAt, rf.Error, rf.Held = RunRunning, time.Time{}, "", false
		rf.Resumed++
		if rf.Vars == nil {
			rf.Vars = map[string]any{}
		}
		vars, opts.Dry = rf.Vars, rf.Dry
	}

	userCtx, cancel := context.WithCancel(ctx)
	r := &runner{
		file:   file,
		opts:   opts,
		user:   userCtx,
		vars:   vars,
		resume: opts.Resume != nil,
		rf:     rf,
		runInfo: map[string]any{
			"id": rf.ID, "date": rf.StartedAt.Format("2006-01-02"), "started_at": rf.StartedAt.Format(time.RFC3339),
			"dry": rf.Dry, "last_ok_at": opts.LastOKAt,
		},
	}
	h := &Handle{cancel: cancel, done: make(chan struct{}), r: r}
	var once sync.Once
	finish := func() { once.Do(func() { cancel(); close(h.done) }) }
	// finish is not deferred in fn: on a panic, onPanic must end the run
	// before Done closes.
	crash.GoWith("workflow.run", func() {
		if resumeErr != nil {
			r.end(RunFail, resumeErr)
		} else {
			r.main(userCtx)
		}
		finish()
	}, func() {
		r.end(RunFail, errors.New("the engine stopped on an internal error"))
		finish()
	})
	return h
}

// runner is the state of one run. Only its goroutine changes rf; it
// holds mu while it does, so Snapshot can read.
type runner struct {
	file    *workflow.File
	opts    Options
	user    context.Context // cancelled by Handle.Cancel only
	runCtx  context.Context // user + limits.timeout
	vars    map[string]any
	runInfo map[string]any
	resume  bool // continuing a run file: finished states are kept

	saveMu  sync.Mutex // one save at a time (parallel items)
	emitMu  sync.Mutex // one OnEvent call at a time
	saveErr error      // the first failed save; the run stops at the next step (under mu)

	mu      sync.Mutex
	rf      *RunFile
	release chan struct{} // closed by Continue; nil while not held (under mu)
}

// hold pauses (on) or continues the run.
func (r *runner) hold(on bool) {
	changed := false
	r.edit(func() {
		if r.rf.State != RunRunning || r.rf.Held == on {
			return
		}
		changed, r.rf.Held = true, on
		if on {
			r.release = make(chan struct{})
		} else {
			close(r.release)
			r.release = nil
		}
	})
	if !changed {
		return
	}
	r.save()
	if on {
		r.emit(Event{Kind: RunHeld})
	} else {
		r.emit(Event{Kind: RunContinued})
	}
}

// waitHeld waits while the user holds the run.
func (r *runner) waitHeld(ctx context.Context) error {
	r.mu.Lock()
	ch := r.release
	r.mu.Unlock()
	if ch == nil {
		return nil
	}
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// save writes the run file (a boundary).
func (r *runner) save() {
	if r.opts.Store == nil || r.failedSave() != nil {
		return
	}
	r.saveMu.Lock()
	defer r.saveMu.Unlock()
	r.mu.Lock()
	b, err := json.Marshal(r.rf)
	r.mu.Unlock()
	if err == nil {
		err = r.opts.Store.WriteRunFile(r.rf.ID, b)
	}
	if err != nil {
		r.edit(func() {
			if r.saveErr == nil {
				r.saveErr = fmt.Errorf("run file: %w", err)
			}
		})
	}
}

// failedSave is the first failed save (nil: none).
func (r *runner) failedSave() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saveErr
}

func (r *runner) main(ctx context.Context) {
	if r.opts.Eval == nil {
		r.end(RunFail, errors.New("no expression evaluator"))
		return
	}
	r.runCtx = ctx
	if d := r.file.Limits.Timeout; d > 0 {
		var cancel context.CancelFunc
		r.runCtx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}
	r.save()
	top := newFrame(nil, nil, r.rf.Steps)
	err := r.runList(r.runCtx, r.file.Steps, top)
	switch {
	case err == nil:
		r.end(RunOK, nil)
	case r.user.Err() != nil:
		r.end(RunCancel, nil)
	case errors.Is(err, ErrPaused):
		r.end(RunPause, cause(err))
	case r.runCtx.Err() != nil:
		r.end(RunFail, fmt.Errorf("the run timed out after %s", r.file.Limits.Timeout))
	default:
		r.end(RunFail, err)
	}
}

func (r *runner) end(state string, err error) {
	r.mu.Lock()
	if r.rf.State != RunRunning { // already ended (panic after the end)
		r.mu.Unlock()
		return
	}
	r.rf.State = state
	r.rf.EndedAt = r.opts.Now()
	if r.release != nil { // a held run that ended: nothing waits any more
		close(r.release)
		r.release = nil
	}
	r.rf.Held = false
	if err != nil {
		r.rf.Error = err.Error()
	}
	r.mu.Unlock()
	r.save()
	ev := Event{Kind: RunDone}
	switch state {
	case RunFail:
		ev = Event{Kind: RunFailed, Err: r.rf.Error}
	case RunCancel:
		ev.Kind = RunCancelled
	case RunPause:
		ev = Event{Kind: RunPaused, Err: r.rf.Error}
	}
	r.emit(ev)
}

func (r *runner) emit(e Event) {
	e.Run = r.rf.ID
	r.emitMu.Lock()
	defer r.emitMu.Unlock()
	e.At = r.opts.Now()
	if r.opts.OnEvent != nil {
		r.opts.OnEvent(e)
	}
}

// edit changes the run state under the lock.
func (r *runner) edit(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn()
}

// stepFailure is a failed step that ends its list. Stop is set for
// on_error "stop": the whole run ends.
type stepFailure struct {
	Step string
	Err  error
	Stop bool
}

func (e *stepFailure) Error() string { return fmt.Sprintf("step %s: %v", e.Step, e.Err) }
func (e *stepFailure) Unwrap() error { return e.Err }

// cause is the adapter's error under the step failures.
func cause(err error) error {
	var sf *stepFailure
	for errors.As(err, &sf) {
		err = sf.Err
	}
	return err
}

func isStop(err error) bool {
	var sf *stepFailure
	return errors.As(err, &sf) && sf.Stop
}

func (r *runner) onError(s *workflow.Step) string {
	if s.OnError != "" {
		return s.OnError
	}
	if r.file.OnError != "" {
		return r.file.OnError
	}
	return workflow.OnErrorContinue
}

// runList runs steps in order. A failed step ends the list: the error is
// a *stepFailure, or the context's error when the run was cancelled.
func (r *runner) runList(ctx context.Context, steps []workflow.Step, f *frame) error {
	for i := range steps {
		if err := r.failedSave(); err != nil {
			return &stepFailure{Step: stepKey(&steps[i]), Err: err, Stop: true}
		}
		if err := r.runStep(ctx, &steps[i], f); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) state(f *frame, key string) *StepState {
	var st *StepState
	r.edit(func() {
		st = f.states[key]
		if st == nil {
			st = &StepState{State: StatePending}
			f.states[key] = st
		}
	})
	return st
}

func (r *runner) runStep(ctx context.Context, s *workflow.Step, f *frame) error {
	if err := r.waitHeld(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	key := stepKey(s)
	item := f.itemPath()
	st := r.state(f, key)
	if r.resume && finished(st.State) {
		r.restore(f, s, st)
		if st.State == StateSkipped && s.Type != workflow.TypePlan {
			return r.runList(ctx, s.Else, f)
		}
		return nil
	}

	if s.When != "" && s.Type != workflow.TypeFilter {
		ok, err := r.opts.Eval.Test(s.When, r.scope(f))
		if err != nil {
			return r.failed(ctx, s, f, st, fmt.Errorf("when: %w", err))
		}
		if !ok {
			r.edit(func() { st.State = StateSkipped })
			r.setOutput(f, s, map[string]any{"ok": false, "skipped": true})
			r.save()
			r.emit(Event{Kind: StepSkipped, Step: key, Item: item})
			return r.runList(ctx, s.Else, f)
		}
	}

	r.edit(func() { st.State = StateRunning; st.Attempts = 0; st.Error = "" })
	r.save()
	r.emit(Event{Kind: StepStart, Step: key, Item: item})
	state, err := r.attempts(ctx, s, f, st)
	if err != nil {
		return r.failed(ctx, s, f, st, err)
	}
	r.edit(func() { st.State = state })
	r.save()
	kind := StepDone
	if state == StateSkipped || state == StateWouldRun {
		kind = StepSkipped
	}
	r.emit(Event{Kind: kind, Step: key, Item: item, Attempt: st.Attempts})
	return nil
}

// attempts runs a step and tries it again on a retryable error, up to
// retries.max more times, waiting backoff, 2×backoff, … in between.
func (r *runner) attempts(ctx context.Context, s *workflow.Step, f *frame, st *StepState) (string, error) {
	rt := r.file.Retries
	if s.Retries != nil {
		rt = *s.Retries
	}
	for try := 1; ; try++ {
		r.edit(func() { st.Attempts = try })
		state, err := r.try(ctx, s, f, st, try)
		if err == nil || try > rt.Max || !retryable(ctx, err) {
			return state, err
		}
		if serr := r.opts.Sleep(ctx, backoff(rt.Backoff, try)); serr != nil {
			return "", err
		}
	}
}

// try runs one try under the step's timeout.
func (r *runner) try(ctx context.Context, s *workflow.Step, f *frame, st *StepState, attempt int) (string, error) {
	if s.Timeout <= 0 || s.Type == workflow.TypeEach || s.Type == workflow.TypeWait {
		return r.exec(ctx, s, f, st, attempt)
	}
	tctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	state, err := r.exec(tctx, s, f, st, attempt)
	if err != nil && tctx.Err() != nil && ctx.Err() == nil {
		err = fmt.Errorf("timed out after %s", s.Timeout)
	}
	return state, err
}

// MaxBackoff caps the wait between two tries.
const MaxBackoff = time.Hour

// backoff is the wait after try n (1-based): base doubled n-1 times.
func backoff(base time.Duration, n int) time.Duration {
	d := base
	for i := 1; i < n && d < MaxBackoff; i++ {
		d *= 2
	}
	return min(d, MaxBackoff)
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// permanent marks an error that a retry cannot fix.
type permanent struct{ err error }

func (p permanent) Error() string { return p.err.Error() }
func (p permanent) Unwrap() error { return p.err }

// Permanent marks err as not worth a retry (a bad argument, a missing
// adapter). Adapters use it; ErrDenied is never retried either.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanent{err}
}

func retryable(ctx context.Context, err error) bool {
	var p permanent
	var sf *stepFailure
	return ctx.Err() == nil && !errors.Is(err, ErrDenied) && !errors.Is(err, ErrPaused) && !errors.As(err, &p) && !errors.As(err, &sf)
}

// failed records a failed step, runs its else (on_error continue) and
// returns the error that ends the list.
func (r *runner) failed(ctx context.Context, s *workflow.Step, f *frame, st *StepState, err error) error {
	key := stepKey(s)
	if r.user.Err() != nil { // cancelled: not a failure of the step
		r.edit(func() { st.State = StateCancelled })
		r.save()
		return r.user.Err()
	}
	if errors.Is(err, ErrPaused) { // not a failure: the whole run waits
		r.edit(func() { st.State = StatePaused; st.Error = err.Error() })
		r.save()
		return &stepFailure{Step: key, Err: err, Stop: true}
	}
	if ctx.Err() != nil && r.runCtx.Err() == nil { // the item's time is up
		err = fmt.Errorf("item timed out after %s", r.file.Limits.ItemTimeout)
	}
	state := StateFailed
	if errors.Is(err, ErrDenied) {
		state = StateDenied
	}
	r.edit(func() { st.State = state; st.Error = err.Error() })
	r.setOutput(f, s, map[string]any{"ok": false, "error": err.Error()})
	r.save()
	r.emit(Event{Kind: StepFailed, Step: key, Item: f.itemPath(), Attempt: st.Attempts, Err: err.Error()})
	stop := r.onError(s) == workflow.OnErrorStop
	if !stop && len(s.Else) > 0 && ctx.Err() == nil {
		if e := r.runList(ctx, s.Else, f); e != nil && (isStop(e) || r.user.Err() != nil) {
			return e
		}
	}
	return &stepFailure{Step: key, Err: err, Stop: stop}
}

// setOutput makes steps.<id> for the steps after this one.
func (r *runner) setOutput(f *frame, s *workflow.Step, out map[string]any) {
	if s.ID != "" {
		f.steps[s.ID] = out
	}
}
