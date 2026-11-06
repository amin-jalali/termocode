package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/workflow"
)

// exec runs one try of a step and returns its final state.
func (r *runner) exec(ctx context.Context, s *workflow.Step, f *frame, st *StepState, attempt int) (string, error) {
	switch s.Type {
	case workflow.TypeSource:
		return r.source(ctx, s, f, st, attempt)
	case workflow.TypePlan:
		return r.plan(ctx, s, f, st, attempt)
	case workflow.TypeFilter:
		return r.filter(ctx, s, f, st)
	case workflow.TypeEach:
		return r.each(ctx, s, f, st)
	case workflow.TypeAI, workflow.TypeAgent, workflow.TypePipeline, workflow.TypeTests, workflow.TypeShell:
		return r.runStepCall(ctx, s, f, st, attempt)
	case workflow.TypeWrite:
		return r.write(ctx, s, f, st, attempt)
	}
	return "", fmt.Errorf("unknown step type %q", s.Type)
}

func (r *runner) setList(f *frame, s *workflow.Step, st *StepState, items []Item) {
	f.setCurrent(items)
	if s.ID != "" {
		f.lists[s.ID] = items
	}
	r.edit(func() { st.Items = items })
	r.setOutput(f, s, map[string]any{"ok": true, "items": listScope(items), "count": float64(len(items))})
}

func (r *runner) renderMap(m map[string]any, sc map[string]any) (map[string]any, error) {
	if m == nil {
		return nil, nil
	}
	v, err := r.opts.Eval.RenderJSON(m, sc)
	if err != nil {
		return nil, err
	}
	out, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("args did not stay an object")
	}
	return out, nil
}

func (r *runner) source(ctx context.Context, s *workflow.Step, f *frame, st *StepState, attempt int) (string, error) {
	if r.opts.Source == nil {
		return "", Permanent(errors.New("no source adapter"))
	}
	sc := r.scope(f)
	spec := s.Source
	if spec == nil {
		spec = &workflow.SourceSpec{}
	}
	args, err := r.renderMap(spec.Args, sc)
	if err != nil {
		return "", Permanent(fmt.Errorf("args: %w", err))
	}
	since, err := r.opts.Eval.Render(spec.ChangedSince, sc)
	if err != nil {
		return "", Permanent(fmt.Errorf("changed_since: %w", err))
	}
	ro := false
	if spec.From == "mcp" {
		if ro, err = r.readOnly(ctx, spec.Server, spec.Tool); err != nil {
			return "", err
		}
		if !ro {
			yes := false
			if r.opts.Ask != nil {
				if yes, err = r.opts.Ask(ctx, AskCall{Step: s, Server: spec.Server, Tool: spec.Tool}); err != nil {
					return "", err
				}
			}
			if !yes {
				return "", fmt.Errorf("%s/%s is not marked read-only: %w", spec.Server, spec.Tool, ErrDenied)
			}
		}
	}
	res, err := r.opts.Source.Read(ctx, SourceCall{Step: s, Args: args, ChangedSince: since,
		Item: f.nearestItem(), Dry: r.opts.Dry, Attempt: attempt, ReadOnly: ro})
	if err != nil {
		return "", err
	}
	items := res.Items
	if max := spec.MaxItems; max > 0 && len(items) > max {
		items = items[:max]
	}
	r.setList(f, s, st, items)
	return StateDone, nil
}

func (r *runner) plan(ctx context.Context, s *workflow.Step, f *frame, st *StepState, attempt int) (string, error) {
	items := f.current()
	if r.opts.NoPlan {
		r.setList(f, s, st, items)
		return StateSkipped, nil
	}
	if r.opts.Plan == nil {
		return "", Permanent(errors.New("no plan adapter"))
	}
	prompt := ""
	if s.Plan != nil {
		var err error
		if prompt, err = r.opts.Eval.Render(s.Plan.Prompt, r.scope(f)); err != nil {
			return "", Permanent(fmt.Errorf("prompt: %w", err))
		}
	}
	res, err := r.opts.Plan.Plan(ctx, PlanCall{Step: s, Items: items, Prompt: prompt, Dry: r.opts.Dry, Attempt: attempt})
	if err != nil {
		return "", err
	}
	rank := map[string]int{}
	for i, k := range res.Order {
		if _, dup := rank[k]; !dup {
			rank[k] = i + 1
		}
	}
	out := make([]Item, len(items))
	for i, it := range items {
		it.Plan = &PlanMark{Rank: rank[it.Key], Skip: res.Skip[it.Key], Labels: res.Labels[it.Key]}
		out[i] = it
	}
	r.setList(f, s, st, out)
	return StateDone, nil
}

func (r *runner) filter(ctx context.Context, s *workflow.Step, f *frame, st *StepState) (string, error) {
	base := r.scope(f)
	scope := func(it Item) map[string]any {
		m := make(map[string]any, len(base)+1)
		for k, v := range base {
			m[k] = v
		}
		m["item"] = itemScope(it)
		return m
	}
	out, err := r.opts.Filter.Filter(ctx, FilterCall{Step: s, Items: f.current(), Scope: scope, Eval: r.opts.Eval})
	if err != nil {
		return "", err
	}
	r.setList(f, s, st, out)
	return StateDone, nil
}

// eachItems finds the list of an each step: steps.<id>.items of an
// earlier list step as it is, any other expression through Eval.
func (r *runner) eachItems(s *workflow.Step, f *frame) ([]Item, error) {
	expr := strings.TrimSpace(s.Each.Items)
	if id, ok := strings.CutPrefix(expr, "steps."); ok {
		if id, ok = strings.CutSuffix(id, ".items"); ok {
			if l, ok := f.list(id); ok {
				return l, nil
			}
		}
	}
	v, err := r.opts.Eval.Eval(expr, r.scope(f))
	if err != nil {
		return nil, fmt.Errorf("items: %w", err)
	}
	return itemsFrom(v)
}

func (r *runner) each(ctx context.Context, s *workflow.Step, f *frame, st *StepState) (string, error) {
	if s.Each == nil {
		return "", Permanent(errors.New("each without steps"))
	}
	items, err := r.eachItems(s, f)
	if err != nil {
		return "", err
	}
	es := st.Each
	if r.resume && es != nil { // the items of the first start, not a new list
		items = items[:0:0]
		for _, is := range es.Items {
			items = append(items, is.item())
		}
	} else {
		es = &EachState{}
		for _, it := range items {
			es.Items = append(es.Items, &ItemState{Key: it.Key, Data: it.Data, Plan: it.Plan, State: StatePending, Steps: map[string]*StepState{}})
		}
		r.edit(func() { st.Each = es })
	}

	errs := r.runItems(ctx, s, f, es.Items)
	var done, failed []any
	for i, is := range es.Items {
		err := errs[i]
		switch {
		case err == errNotRun:
			continue
		case err == nil:
			done = append(done, is.Key)
		default:
			failed = append(failed, is.Key)
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	for _, err := range errs {
		if err != nil && err != errNotRun && (r.user.Err() != nil || isStop(err)) {
			return "", err
		}
	}
	r.setOutput(f, s, map[string]any{"ok": len(failed) == 0, "items": listScope(items), "done": nonNil(done), "failed": nonNil(failed)})
	return StateDone, nil
}

// errNotRun marks an item a stopped each did not start.
var errNotRun = errors.New("not run")

// parallel is how many items of an each run at once: the step's
// parallel, else limits.parallel, capped by Options.ParallelMax.
func (r *runner) parallel(s *workflow.Step) int {
	n := s.Each.Parallel
	if n <= 0 {
		n = r.file.Limits.Parallel
	}
	if m := r.opts.ParallelMax; m > 0 && n > m {
		n = m
	}
	return max(n, 1)
}

// runItems runs the items of an each, parallel at once, and returns the
// error of each (errNotRun: not started, because the run was cancelled
// or an item stopped the run). Items that a resumed run finished before
// are not run again (nil, or their old failure).
func (r *runner) runItems(ctx context.Context, s *workflow.Step, f *frame, list []*ItemState) []error {
	errs := make([]error, len(list))
	for i := range errs {
		errs[i] = errNotRun
	}
	var mu sync.Mutex
	next, stopped := 0, false
	take := func() int {
		mu.Lock()
		defer mu.Unlock()
		for next < len(list) && !stopped && ctx.Err() == nil {
			i := next
			next++
			if is := list[i]; r.resume && itemFinished(is.State) {
				if is.State == StateDone {
					errs[i] = nil
				} else {
					errs[i] = errors.New(is.Error)
				}
				continue
			}
			return i
		}
		return -1
	}
	work := func() {
		for i := take(); i >= 0; i = take() {
			err := r.item(ctx, s, f, list[i])
			mu.Lock()
			errs[i] = err
			if err != nil && (r.user.Err() != nil || isStop(err)) {
				stopped = true // no new items; the running ones finish
			}
			mu.Unlock()
		}
	}
	n := min(r.parallel(s), len(list))
	if n <= 1 {
		work()
		return errs
	}
	var wg sync.WaitGroup
	for w := 0; w < n; w++ {
		wg.Add(1)
		crash.Go("workflow.each", func() {
			defer wg.Done()
			work()
		})
	}
	wg.Wait()
	return errs
}

func nonNil(l []any) []any {
	if l == nil {
		return []any{}
	}
	return l
}

// item runs the steps of one each item.
func (r *runner) item(ctx context.Context, s *workflow.Step, f *frame, is *ItemState) error {
	key := stepKey(s)
	it := is.item()
	if is.Steps == nil {
		r.edit(func() { is.Steps = map[string]*StepState{} })
	}
	child := newFrame(f, &it, is.Steps)
	path := child.itemPath()
	r.edit(func() { is.State = StateRunning; is.Error = "" })
	r.save()
	r.emit(Event{Kind: ItemStart, Step: key, Item: path})

	call := EachCall{Step: s, Item: it}
	entered := false
	var err error
	if r.opts.Each != nil && !r.opts.Dry {
		call.Base, err = r.opts.Eval.Render(s.Each.Base, r.scope(f))
		if err == nil {
			child.env, err = r.opts.Each.Enter(ctx, call)
			entered = err == nil
		}
		if err != nil {
			err = &stepFailure{Step: key, Err: fmt.Errorf("item %s: %w", it.Key, err)}
		} else {
			env := child.env
			r.edit(func() { is.Env = env })
			if s.ID != "" { // steps.<each id>.worktree inside the item
				child.steps[s.ID] = map[string]any{"worktree": env.Worktree, "branch": env.Branch}
			}
		}
	}
	if err == nil {
		ictx := ctx
		if d := r.file.Limits.ItemTimeout; d > 0 {
			var cancel context.CancelFunc
			ictx, cancel = context.WithTimeout(ctx, d)
			defer cancel()
		}
		err = r.runList(ictx, s.Each.Steps, child)
	}
	if entered {
		if lerr := r.opts.Each.Leave(context.WithoutCancel(ctx), call, child.env, err == nil); lerr != nil && err == nil {
			err = &stepFailure{Step: key, Err: fmt.Errorf("item %s: %w", it.Key, lerr)}
		}
	}
	state, msg := StateDone, ""
	switch {
	case err == nil:
	case r.user.Err() != nil:
		state = StateCancelled
	case errors.Is(err, ErrPaused):
		state = StatePaused
	default:
		state, msg = StateFailed, err.Error()
	}
	r.edit(func() { is.State = state; is.Error = msg })
	r.save()
	r.emit(Event{Kind: ItemDone, Step: key, Item: path, Err: msg})
	return err
}

func (r *runner) prompt(s *workflow.Step) string {
	switch {
	case s.AI != nil:
		return s.AI.Prompt
	case s.Agent != nil:
		return s.Agent.Prompt
	case s.Pipeline != nil:
		return s.Pipeline.Prompt
	}
	return ""
}

func (r *runner) runStepCall(ctx context.Context, s *workflow.Step, f *frame, st *StepState, attempt int) (string, error) {
	if r.opts.Dry {
		r.setOutput(f, s, map[string]any{"ok": true, "dry": true})
		return StateWouldRun, nil
	}
	if r.opts.Run == nil {
		return "", Permanent(errors.New("no run adapter"))
	}
	sc := r.scope(f)
	prompt, err := r.opts.Eval.Render(r.prompt(s), sc)
	if err != nil {
		return "", Permanent(fmt.Errorf("prompt: %w", err))
	}
	render := func(t string) (string, error) { return r.opts.Eval.Render(t, sc) }
	out, err := r.opts.Run.Run(ctx, RunCall{Step: s, Item: f.nearestItem(), Env: f.nearestEnv(),
		Prompt: prompt, Render: render, Attempt: attempt})
	if err != nil {
		return "", err
	}
	r.keepOutput(f, s, st, out)
	return StateDone, nil
}

func (r *runner) keepOutput(f *frame, s *workflow.Step, st *StepState, out Output) {
	m := map[string]any{"ok": true, "text": out.Text, "json": out.JSON, "url": out.URL,
		"path": out.Path, "exit": float64(out.Exit), "worktree": out.Worktree}
	r.setOutput(f, s, m)
	c := capOutput(out)
	r.edit(func() { st.Output = &c })
}

// renderWrite fills every template of a write spec.
func (r *runner) renderWrite(w *workflow.WriteSpec, sc map[string]any) (workflow.WriteSpec, error) {
	out := *w
	for _, p := range []*string{&out.Title, &out.Body, &out.Base, &out.Message, &out.PR, &out.Path, &out.Content, &out.Text} {
		v, err := r.opts.Eval.Render(*p, sc)
		if err != nil {
			return out, err
		}
		*p = v
	}
	var err error
	if out.Args, err = r.renderMap(w.Args, sc); err != nil {
		return out, err
	}
	if w.Undo != nil {
		u := *w.Undo
		if u.Args, err = r.renderMap(w.Undo.Args, sc); err != nil {
			return out, err
		}
		out.Undo = &u
	}
	return out, nil
}

func (r *runner) marker(s *workflow.Step, f *frame) string {
	m := "termocode-run:" + r.rf.ID + "/" + stepKey(s)
	if p := f.itemPath(); p != "" {
		m += "/" + p
	}
	return m
}

func (r *runner) write(ctx context.Context, s *workflow.Step, f *frame, st *StepState, attempt int) (string, error) {
	if s.Write == nil {
		return "", Permanent(errors.New("write without a target"))
	}
	sc := r.scope(f)
	spec, err := r.renderWrite(s.Write, sc)
	if err != nil {
		return "", Permanent(err)
	}
	key, item := stepKey(s), f.itemPath()
	rec := WriteRecord{Step: key, Item: item, To: spec.To, Marker: r.marker(s, f), Spec: spec}
	if r.opts.Dry || spec.Gate == workflow.GateDry {
		rec.State = StateRecorded
		r.edit(func() { r.rf.Writes = append(r.rf.Writes, rec) })
		r.setOutput(f, s, map[string]any{"ok": true, "dry": true})
		return StateRecorded, nil
	}
	if r.opts.Write == nil {
		return "", Permanent(errors.New("no write adapter"))
	}
	ro := false
	if spec.To == "mcp" || spec.To == "status" {
		if ro, err = r.readOnly(ctx, spec.Server, spec.Tool); err != nil {
			return "", err
		}
	}
	rec.State = WriteSending
	idx, resumed := -1, false
	r.edit(func() {
		// An earlier try that crashed or failed may have been sent.
		for i, w := range r.rf.Writes {
			if w.Marker == rec.Marker && (w.State == WriteSending || w.State == StateFailed) {
				idx, resumed = i, true
				r.rf.Writes[i] = rec
			}
		}
		if idx < 0 {
			r.rf.Writes = append(r.rf.Writes, rec)
			idx = len(r.rf.Writes) - 1
		}
	})
	r.save() // "sending" is on disk before the write goes out
	wait := func() { r.emit(Event{Kind: WriteWait, Step: key, Item: item, Attempt: attempt}) }
	out, err := r.opts.Write.Write(ctx, WriteCall{Step: s, Item: f.nearestItem(), Env: f.nearestEnv(), Spec: spec, Marker: rec.Marker,
		Attempt: attempt, Resumed: resumed, ReadOnly: ro, Wait: wait,
		Render: func(t string) (string, error) { return r.opts.Eval.Render(t, sc) }})
	switch {
	case errors.Is(err, ErrDenied):
		r.edit(func() { r.rf.Writes[idx].State = StateDenied })
		r.emit(Event{Kind: WriteDenied, Step: key, Item: item, Attempt: attempt})
		return "", err
	case err != nil:
		msg := err.Error()
		r.edit(func() { r.rf.Writes[idx].State = StateFailed; r.rf.Writes[idx].Error = msg })
		return "", err
	}
	r.edit(func() { r.rf.Writes[idx].State = StateDone; r.rf.Writes[idx].URL = out.URL })
	r.emit(Event{Kind: WriteAllowed, Step: key, Item: item, Attempt: attempt})
	r.keepOutput(f, s, st, out)
	return StateDone, nil
}

// restore puts a kept step's output back into scope (resume).
func (r *runner) restore(f *frame, s *workflow.Step, st *StepState) {
	switch {
	case st.Items != nil || s.Type == workflow.TypeSource || s.Type == workflow.TypePlan || s.Type == workflow.TypeFilter:
		r.setList(f, s, st, st.Items)
	case st.Each != nil:
		var all []Item
		done, failed := []any{}, []any{}
		for _, is := range st.Each.Items {
			all = append(all, is.item())
			if is.State == StateDone {
				done = append(done, is.Key)
			} else {
				failed = append(failed, is.Key)
			}
		}
		r.setOutput(f, s, map[string]any{"ok": len(failed) == 0, "items": listScope(all), "done": done, "failed": failed})
	case st.State == StateSkipped:
		r.setOutput(f, s, map[string]any{"ok": false, "skipped": true})
	case st.State == StateWouldRun || st.State == StateRecorded:
		r.setOutput(f, s, map[string]any{"ok": true, "dry": true})
	case st.Output != nil:
		o := *st.Output
		r.setOutput(f, s, map[string]any{"ok": true, "text": o.Text, "json": o.JSON, "url": o.URL,
			"path": o.Path, "exit": float64(o.Exit), "worktree": o.Worktree})
	default:
		r.setOutput(f, s, map[string]any{"ok": true})
	}
}

// readOnly asks Options.Tools whether an MCP tool is read-only (false
// when there is no check).
func (r *runner) readOnly(ctx context.Context, server, tool string) (bool, error) {
	if r.opts.Tools == nil {
		return false, nil
	}
	ro, err := r.opts.Tools.ReadOnly(ctx, server, tool)
	if err != nil {
		return false, fmt.Errorf("%s/%s: %w", server, tool, err)
	}
	return ro, nil
}
