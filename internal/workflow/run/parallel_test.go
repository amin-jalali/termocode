package run

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// WFX-10: each runs its items in parallel, at most `parallel` at once
// (the step's, else limits.parallel), and never more than
// Options.ParallelMax (workflows_parallel_max).

// gauge is a Runner that counts the run steps working at once. The
// first `meet` calls wait until that many are in (or 5 s pass), so a
// parallel run proves itself without timing luck.
type gauge struct {
	*fakes
	meet int

	mu      sync.Mutex
	active  int
	max     int
	started int
	all     chan struct{}
	once    sync.Once
}

func newGauge(f *fakes, meet int) *gauge { return &gauge{fakes: f, meet: meet, all: make(chan struct{})} }

func (g *gauge) Run(ctx context.Context, c RunCall) (Output, error) {
	g.mu.Lock()
	g.active++
	g.started++
	g.max = max(g.max, g.active)
	n := g.started
	if g.active >= g.meet {
		g.once.Do(func() { close(g.all) })
	}
	g.mu.Unlock()
	if n <= g.meet {
		select {
		case <-g.all:
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
		}
	}
	out, err := g.fakes.Run(ctx, c)
	g.mu.Lock()
	g.active--
	g.mu.Unlock()
	return out, err
}

func (g *gauge) peak() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.max
}

const parallelFile = `{ "version": 1, "name": "par", "limits": { "parallel": %LIMIT% },
  "steps": [
    { "id": "src", "type": "source", "from": "shell", "argv": ["x"] },
    { "id": "fix", "type": "each", "items": "steps.src.items" %PAR%, "worktree": true,
      "steps": [
        { "id": "code", "type": "shell", "argv": ["x"] },
        { "id": "pr", "type": "write", "to": "file", "path": "a", "content": "wt={{steps.fix.worktree}}" }
      ] },
    { "id": "after", "type": "shell", "argv": ["x"] }
  ] }`

func parFile(t *testing.T, limit, par string) string {
	t.Helper()
	s := parallelFile
	s = strings.ReplaceAll(s, "%LIMIT%", limit)
	return strings.ReplaceAll(s, "%PAR%", par)
}

func TestEachParallelRunsTwoAtOnce(t *testing.T) {
	f := newFakes(items("a", "b", "c")...)
	g := newGauge(f, 2)
	opts := f.options()
	opts.Run = g
	rf := wait(t, Start(context.Background(), mustParse(t, parFile(t, "1", `, "parallel": 2`)), opts))
	if rf.State != RunOK {
		t.Fatalf("state %s: %s", rf.State, rf.Error)
	}
	if p := g.peak(); p != 2 {
		t.Errorf("%d run steps at once, want 2", p)
	}
	es := rf.Steps["fix"].Each
	for _, is := range es.Items {
		if is.State != StateDone {
			t.Errorf("item %s: %s", is.Key, is.State)
		}
	}
	// The step after the each runs once, after every item.
	log := f.all()
	if last := log[len(log)-1]; last != `run after prompt="" wt=` {
		t.Errorf("last call %q", last)
	}
}

func TestEachParallelFromLimitsAndCappedByParallelMax(t *testing.T) {
	t.Run("limits.parallel", func(t *testing.T) {
		f := newFakes(items("a", "b", "c")...)
		g := newGauge(f, 3)
		opts := f.options()
		opts.Run = g
		wait(t, Start(context.Background(), mustParse(t, parFile(t, "3", "")), opts))
		if p := g.peak(); p != 3 {
			t.Errorf("%d at once, want 3 (limits.parallel)", p)
		}
	})
	t.Run("workflows_parallel_max", func(t *testing.T) {
		f := newFakes(items("a", "b", "c")...)
		g := newGauge(f, 1)
		opts := f.options()
		opts.Run = g
		opts.ParallelMax = 1
		wait(t, Start(context.Background(), mustParse(t, parFile(t, "1", `, "parallel": 3`)), opts))
		if p := g.peak(); p != 1 {
			t.Errorf("%d at once, want 1 (capped by ParallelMax)", p)
		}
	})
}

func TestEachParallelKeepsFileOrderAndWorktreeScope(t *testing.T) {
	f := newFakes(items("a", "b", "c")...)
	f.fail["code/b"] = []error{errFake("red")}
	opts := f.options()
	h := Start(context.Background(), mustParse(t, parFile(t, "1", `, "parallel": 3`)), opts)
	rf := wait(t, h)
	var keys []string
	for _, is := range rf.Steps["fix"].Each.Items {
		keys = append(keys, is.Key+"="+is.State)
	}
	if want := []string{"a=done", "b=failed", "c=done"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("items %v, want %v", keys, want)
	}
	// steps.<each id>.worktree is the item's worktree inside the item, and
	// the write gets the item's env.
	got := map[string]bool{}
	for _, w := range rf.Writes {
		got[w.Item+" "+w.Spec.Content] = true
	}
	for _, want := range []string{"a wt=/wt/a", "c wt=/wt/c"} {
		if !got[want] {
			t.Errorf("no write %q in %v", want, got)
		}
	}
}

func TestEachParallelWriteGetsItemEnv(t *testing.T) {
	f := newFakes(items("a", "b")...)
	env := &envSpy{fakes: f}
	opts := f.options()
	opts.Write = env
	wait(t, Start(context.Background(), mustParse(t, parFile(t, "1", `, "parallel": 2`)), opts))
	env.mu.Lock()
	defer env.mu.Unlock()
	if !env.seen["a"] || !env.seen["b"] {
		t.Errorf("writes saw envs %v", env.seen)
	}
}

type envSpy struct {
	*fakes
	mu   sync.Mutex
	seen map[string]bool
}

func (e *envSpy) Write(ctx context.Context, c WriteCall) (Output, error) {
	e.mu.Lock()
	if e.seen == nil {
		e.seen = map[string]bool{}
	}
	e.seen[c.Item.Key] = c.Env.Worktree == "/wt/"+c.Item.Key
	e.mu.Unlock()
	return e.fakes.Write(ctx, c)
}

func TestEachParallelCancelLeavesEveryItemNotOK(t *testing.T) {
	f := newFakes(items("a", "b", "c")...)
	f.slow["code/a"], f.slow["code/b"] = true, true
	opts := f.options()
	h := Start(context.Background(), mustParse(t, parFile(t, "1", `, "parallel": 2`)), opts)
	deadline := time.Now().Add(5 * time.Second)
	for f.tried("code/a") == 0 || f.tried("code/b") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the two items did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.Cancel()
	rf := wait(t, h)
	if rf.State != RunCancel {
		t.Fatalf("state %s", rf.State)
	}
	leaves := 0
	for _, c := range f.all() {
		switch c {
		case "leave a ok=false", "leave b ok=false":
			leaves++
		case "leave a ok=true", "leave b ok=true", "enter c base=":
			t.Errorf("call %q after cancel", c)
		}
	}
	if leaves != 2 {
		t.Errorf("%d leaves with ok=false: %v", leaves, f.all())
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }
