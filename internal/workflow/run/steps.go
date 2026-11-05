package run

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
		return "", errors.New("no source adapter")
	}
	sc := r.scope(f)
	spec := s.Source
	if spec == nil {
		spec = &workflow.SourceSpec{}
	}
	args, err := r.renderMap(spec.Args, sc)
	if err != nil {
		return "", fmt.Errorf("args: %w", err)
	}
	since, err := r.opts.Eval.Render(spec.ChangedSince, sc)
	if err != nil {
		return "", fmt.Errorf("changed_since: %w", err)
	}
	res, err := r.opts.Source.Read(ctx, SourceCall{Step: s, Args: args, ChangedSince: since,
		Item: f.nearestItem(), Dry: r.opts.Dry, Attempt: attempt})
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
		return "", errors.New("no plan adapter")
	}
	prompt := ""
	if s.Plan != nil {
		var err error
		if prompt, err = r.opts.Eval.Render(s.Plan.Prompt, r.scope(f)); err != nil {
			return "", fmt.Errorf("prompt: %w", err)
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
		return "", errors.New("each without steps")
	}
	items, err := r.eachItems(s, f)
	if err != nil {
		return "", err
	}
	es := &EachState{}
	for _, it := range items {
		es.Items = append(es.Items, &ItemState{Key: it.Key, Data: it.Data, Plan: it.Plan, State: StatePending, Steps: map[string]*StepState{}})
	}
	r.edit(func() { st.Each = es })

	var done, failed []any
	for _, is := range es.Items {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		err := r.item(ctx, s, f, is)
		switch {
		case err == nil:
			done = append(done, is.Key)
		case r.user.Err() != nil || isStop(err):
			return "", err
		default:
			failed = append(failed, is.Key)
		}
	}
	r.setOutput(f, s, map[string]any{"ok": len(failed) == 0, "items": listScope(items), "done": nonNil(done), "failed": nonNil(failed)})
	return StateDone, nil
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
	child := newFrame(f, &it, is.Steps)
	path := child.itemPath()
	r.edit(func() { is.State = StateRunning; is.Error = "" })
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
		}
	}
	if err == nil {
		err = r.runList(ctx, s.Each.Steps, child)
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
	default:
		state, msg = StateFailed, err.Error()
	}
	r.edit(func() { is.State = state; is.Error = msg })
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
		return "", errors.New("no run adapter")
	}
	sc := r.scope(f)
	prompt, err := r.opts.Eval.Render(r.prompt(s), sc)
	if err != nil {
		return "", fmt.Errorf("prompt: %w", err)
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
		return "", errors.New("write without a target")
	}
	spec, err := r.renderWrite(s.Write, r.scope(f))
	if err != nil {
		return "", err
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
		return "", errors.New("no write adapter")
	}
	rec.State = "sending"
	var idx int
	r.edit(func() { r.rf.Writes = append(r.rf.Writes, rec); idx = len(r.rf.Writes) - 1 })
	wait := func() { r.emit(Event{Kind: WriteWait, Step: key, Item: item, Attempt: attempt}) }
	out, err := r.opts.Write.Write(ctx, WriteCall{Step: s, Item: f.nearestItem(), Spec: spec, Marker: rec.Marker,
		Attempt: attempt, Wait: wait})
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
