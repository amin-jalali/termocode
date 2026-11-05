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

	// OnEvent gets every event, in order, on the run's goroutine. It must
	// not block for long.
	OnEvent func(Event)
	// Now is the clock (nil: time.Now).
	Now func() time.Time
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
	now := opts.Now()
	id := opts.RunID
	if id == "" {
		id = now.UTC().Format("20060102-150405")
	}
	vars := map[string]any{}
	maps.Copy(vars, file.Vars)
	maps.Copy(vars, opts.Vars)

	userCtx, cancel := context.WithCancel(ctx)
	r := &runner{
		file: file,
		opts: opts,
		user: userCtx,
		vars: vars,
		rf: &RunFile{
			Version: RunFileVersion, ID: id, Workflow: file.Name, Hash: file.Hash,
			Dry: opts.Dry, State: RunRunning, StartedAt: now, Vars: vars,
			Steps: map[string]*StepState{},
		},
		runInfo: map[string]any{
			"id": id, "date": now.Format("2006-01-02"), "started_at": now.Format(time.RFC3339),
			"dry": opts.Dry, "last_ok_at": opts.LastOKAt,
		},
	}
	h := &Handle{cancel: cancel, done: make(chan struct{}), r: r}
	var once sync.Once
	finish := func() { once.Do(func() { cancel(); close(h.done) }) }
	crash.GoWith("workflow.run", func() {
		defer finish()
		r.main(userCtx)
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
	vars    map[string]any
	runInfo map[string]any

	mu sync.Mutex
	rf *RunFile
}

func (r *runner) main(ctx context.Context) {
	if r.opts.Eval == nil {
		r.end(RunFail, errors.New("no expression evaluator"))
		return
	}
	top := newFrame(nil, nil, r.rf.Steps)
	err := r.runList(ctx, r.file.Steps, top)
	switch {
	case err == nil:
		r.end(RunOK, nil)
	case r.user.Err() != nil:
		r.end(RunCancel, nil)
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
	if err != nil {
		r.rf.Error = err.Error()
	}
	r.mu.Unlock()
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
	e.At = r.opts.Now()
	if r.opts.OnEvent != nil {
		r.opts.OnEvent(e)
	}
}

// edit changes the run state under the lock.
func (r *runner) edit(fn func()) {
	r.mu.Lock()
	fn()
	r.mu.Unlock()
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
	if err := ctx.Err(); err != nil {
		return err
	}
	key := stepKey(s)
	item := f.itemPath()
	st := r.state(f, key)

	if s.When != "" && s.Type != workflow.TypeFilter {
		ok, err := r.opts.Eval.Test(s.When, r.scope(f))
		if err != nil {
			return r.failed(ctx, s, f, st, fmt.Errorf("when: %w", err))
		}
		if !ok {
			r.edit(func() { st.State = StateSkipped })
			r.setOutput(f, s, map[string]any{"ok": false, "skipped": true})
			r.emit(Event{Kind: StepSkipped, Step: key, Item: item})
			return r.runList(ctx, s.Else, f)
		}
	}

	r.edit(func() { st.State = StateRunning; st.Attempts = 0; st.Error = "" })
	r.emit(Event{Kind: StepStart, Step: key, Item: item})
	state, err := r.attempts(ctx, s, f, st)
	if err != nil {
		return r.failed(ctx, s, f, st, err)
	}
	r.edit(func() { st.State = state })
	kind := StepDone
	if state == StateSkipped || state == StateWouldRun {
		kind = StepSkipped
	}
	r.emit(Event{Kind: kind, Step: key, Item: item, Attempt: st.Attempts})
	return nil
}

// attempts runs one step (retries come with the step's retries).
func (r *runner) attempts(ctx context.Context, s *workflow.Step, f *frame, st *StepState) (string, error) {
	r.edit(func() { st.Attempts++ })
	return r.exec(ctx, s, f, st, st.Attempts)
}

// failed records a failed step, runs its else (on_error continue) and
// returns the error that ends the list.
func (r *runner) failed(ctx context.Context, s *workflow.Step, f *frame, st *StepState, err error) error {
	key := stepKey(s)
	if r.user.Err() != nil { // cancelled: not a failure of the step
		r.edit(func() { st.State = StateCancelled })
		return r.user.Err()
	}
	state := StateFailed
	if errors.Is(err, ErrDenied) {
		state = StateDenied
	}
	r.edit(func() { st.State = state; st.Error = err.Error() })
	r.setOutput(f, s, map[string]any{"ok": false, "error": err.Error()})
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
