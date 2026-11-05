package run

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/workflow"
)

// fakeEval is a tiny stand-in for the expr package: paths (a.b[0]),
// literals, "not X", "A == B", len(X); templates {{X}}.
type fakeEval struct{}

func (fakeEval) Eval(expr string, scope map[string]any) (any, error) { return evalFake(expr, scope) }

func (fakeEval) Test(expr string, scope map[string]any) (bool, error) {
	v, err := evalFake(expr, scope)
	return truthyFake(v), err
}

func (fakeEval) Render(tmpl string, scope map[string]any) (string, error) {
	var b strings.Builder
	for {
		i := strings.Index(tmpl, "{{")
		if i < 0 {
			b.WriteString(tmpl)
			return b.String(), nil
		}
		j := strings.Index(tmpl[i:], "}}")
		if j < 0 {
			return "", errors.New("unclosed {{")
		}
		b.WriteString(tmpl[:i])
		v, err := evalFake(tmpl[i+2:i+j], scope)
		if err != nil {
			return "", err
		}
		if v != nil {
			b.WriteString(textFake(v))
		}
		tmpl = tmpl[i+j+2:]
	}
}

func (e fakeEval) RenderJSON(v any, scope map[string]any) (any, error) {
	switch x := v.(type) {
	case string:
		t := strings.TrimSpace(x)
		if strings.HasPrefix(t, "{{") && strings.HasSuffix(t, "}}") && strings.Count(t, "{{") == 1 {
			return evalFake(t[2:len(t)-2], scope)
		}
		return e.Render(x, scope)
	case map[string]any:
		out := map[string]any{}
		for k, val := range x {
			r, err := e.RenderJSON(val, scope)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			r, err := e.RenderJSON(val, scope)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	}
	return v, nil
}

func evalFake(expr string, scope map[string]any) (any, error) {
	expr = strings.TrimSpace(expr)
	if a, b, ok := strings.Cut(expr, " == "); ok {
		x, err := evalFake(a, scope)
		if err != nil {
			return nil, err
		}
		y, err := evalFake(b, scope)
		return fmt.Sprint(x) == fmt.Sprint(y), err
	}
	if rest, ok := strings.CutPrefix(expr, "not "); ok {
		v, err := evalFake(rest, scope)
		return !truthyFake(v), err
	}
	if inner, ok := strings.CutPrefix(expr, "len("); ok {
		v, err := evalFake(strings.TrimSuffix(inner, ")"), scope)
		if l, ok := v.([]any); ok {
			return float64(len(l)), err
		}
		return float64(0), err
	}
	switch expr {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	case "boom":
		return nil, errors.New("boom")
	}
	if strings.HasPrefix(expr, `"`) {
		return strings.Trim(expr, `"`), nil
	}
	if f, err := strconv.ParseFloat(expr, 64); err == nil {
		return f, nil
	}
	var cur any = scope
	for _, part := range strings.Split(expr, ".") {
		name, idx, hasIdx := strings.Cut(part, "[")
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, nil
		}
		cur = m[name]
		if hasIdx {
			n, _ := strconv.Atoi(strings.TrimSuffix(idx, "]"))
			l, ok := cur.([]any)
			if !ok || n >= len(l) {
				return nil, nil
			}
			cur = l[n]
		}
	}
	return cur, nil
}

func truthyFake(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	case []any:
		return len(x) > 0
	}
	return true
}

func textFake(v any) string {
	if f, ok := v.(float64); ok {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

// calls records adapter calls, in order, safe for -race.
type calls struct {
	mu  sync.Mutex
	log []string
}

func (c *calls) add(format string, a ...any) {
	c.mu.Lock()
	c.log = append(c.log, fmt.Sprintf(format, a...))
	c.mu.Unlock()
}

func (c *calls) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.log...)
}

// fakes is one set of fake adapters.
type fakes struct {
	calls
	items []Item
	// fail maps "step" or "step/item" to errors returned on each try, in
	// order (nil = success); past the end the step succeeds.
	fail  map[string][]error
	tries map[string]int
	// block makes the run step of this "step/item" wait for ctx.
	block   string
	blocked chan struct{}
	deny    map[string]bool // write step keys that the user denies
	plan    PlanResult
}

func newFakes(items ...Item) *fakes {
	return &fakes{items: items, fail: map[string][]error{}, tries: map[string]int{}, deny: map[string]bool{}, blocked: make(chan struct{}, 1)}
}

func (f *fakes) try(key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.tries[key]
	f.tries[key]++
	if errs := f.fail[key]; n < len(errs) {
		return errs[n]
	}
	return nil
}

func (f *fakes) tried(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tries[key]
}

func callKey(s *workflow.Step, it *Item) string {
	if it == nil {
		return stepKey(s)
	}
	return stepKey(s) + "/" + it.Key
}

func (f *fakes) Read(ctx context.Context, c SourceCall) (SourceResult, error) {
	f.add("source %s args=%v", stepKey(c.Step), c.Args)
	if err := f.try(stepKey(c.Step)); err != nil {
		return SourceResult{}, err
	}
	return SourceResult{Items: f.items, Total: len(f.items)}, nil
}

func (f *fakes) Plan(ctx context.Context, c PlanCall) (PlanResult, error) {
	f.add("plan %s n=%d", stepKey(c.Step), len(c.Items))
	return f.plan, f.try(stepKey(c.Step))
}

func (f *fakes) Run(ctx context.Context, c RunCall) (Output, error) {
	k := callKey(c.Step, c.Item)
	f.add("run %s prompt=%q wt=%s", k, c.Prompt, c.Env.Worktree)
	if k == f.block {
		f.blocked <- struct{}{}
		<-ctx.Done()
		return Output{}, ctx.Err()
	}
	if err := f.try(k); err != nil {
		return Output{}, err
	}
	return Output{Text: "out of " + k}, nil
}

func (f *fakes) Write(ctx context.Context, c WriteCall) (Output, error) {
	k := callKey(c.Step, c.Item)
	f.add("write %s to=%s title=%q marker=%s resumed=%v", k, c.Spec.To, c.Spec.Title, c.Marker, c.Resumed)
	c.Wait()
	if f.deny[stepKey(c.Step)] {
		return Output{}, fmt.Errorf("write %s: %w", k, ErrDenied)
	}
	if err := f.try(k); err != nil {
		return Output{}, err
	}
	return Output{URL: "https://example.test/" + k}, nil
}

func (f *fakes) Enter(ctx context.Context, c EachCall) (ItemEnv, error) {
	f.add("enter %s base=%s", c.Item.Key, c.Base)
	return ItemEnv{Worktree: "/wt/" + c.Item.Key}, nil
}

func (f *fakes) Leave(ctx context.Context, c EachCall, env ItemEnv, ok bool) error {
	f.add("leave %s ok=%v", c.Item.Key, ok)
	return nil
}

func (f *fakes) options() Options {
	return Options{RunID: "r1", Eval: fakeEval{}, Source: f, Plan: f, Each: f, Run: f, Write: f,
		Now: func() time.Time { return time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC) }}
}

// mustParse parses a workflow file body for a test.
func mustParse(t *testing.T, src string) *workflow.File {
	t.Helper()
	f, _, err := workflow.Parse("", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return f
}

// events collects events, safe for -race.
type events struct {
	mu   sync.Mutex
	list []Event
}

func (e *events) on(ev Event) {
	e.mu.Lock()
	e.list = append(e.list, ev)
	e.mu.Unlock()
}

// kinds lists "kind step item" for each event.
func (e *events) kinds() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, ev := range e.list {
		s := string(ev.Kind)
		if ev.Step != "" {
			s += " " + ev.Step
		}
		if ev.Item != "" {
			s += " " + ev.Item
		}
		out = append(out, s)
	}
	return out
}

func wait(t *testing.T, h *Handle) *RunFile {
	t.Helper()
	select {
	case <-h.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("run did not end")
	}
	return h.Snapshot()
}

func items(keys ...string) []Item {
	out := make([]Item, len(keys))
	for i, k := range keys {
		out[i] = Item{Key: k, Data: map[string]any{"id": k, "title": "T " + k, "ready": k != "b"}}
	}
	return out
}
