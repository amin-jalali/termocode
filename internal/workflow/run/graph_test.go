package run

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/workflow"
)

// CMP-42: loop with a cap, branch / join, wait for PR/CI, handoff.

func TestLoopRunsUntilItsStepsPass(t *testing.T) {
	src := `{"version": 1, "name": "w", "steps": [
	  {"id": "fix", "type": "loop", "max": 3, "steps": [
	    {"id": "code", "type": "ai", "model": "quick", "prompt": "round {{loop.round}} of {{loop.max}}"},
	    {"id": "t", "type": "tests", "task": "test"}]},
	  {"id": "after", "type": "ai", "model": "quick", "prompt": "rounds {{steps.fix.rounds}} last {{steps.t.text}}"}]}`
	fk := newFakes()
	fk.fail["t"] = []error{Permanent(errors.New("exit 1"))}
	var ev events
	opts := fk.options()
	opts.OnEvent = ev.on
	rf := wait(t, Start(context.Background(), mustParse(t, src), opts))
	if rf.State != RunOK {
		t.Fatalf("run %s: %s", rf.State, rf.Error)
	}
	st := rf.Steps["fix"]
	if st.State != StateDone || st.Loop == nil || len(st.Loop.Rounds) != 2 {
		t.Fatalf("loop state %+v", st)
	}
	if st.Loop.Rounds[0].State != StateFailed || st.Loop.Rounds[1].State != StateDone {
		t.Fatalf("rounds %+v %+v", st.Loop.Rounds[0], st.Loop.Rounds[1])
	}
	log := strings.Join(fk.all(), "\n")
	for _, want := range []string{`prompt="round 1 of 3"`, `prompt="round 2 of 3"`, `prompt="rounds 2 last out of t"`} {
		if !strings.Contains(log, want) {
			t.Errorf("calls lack %s:\n%s", want, log)
		}
	}
	if n := strings.Count(strings.Join(ev.kinds(), "\n"), string(LoopRound)+" fix"); n != 2 {
		t.Errorf("%d loop_round events, want 2: %v", n, ev.kinds())
	}
}

func TestLoopStopsAtItsCap(t *testing.T) {
	src := `{"version": 1, "name": "w", "steps": [
	  {"id": "fix", "type": "loop", "max": 2, "steps": [{"id": "t", "type": "tests", "task": "test"}],
	   "else": [{"id": "e", "type": "ai", "model": "quick", "prompt": "give up"}]}]}`
	fk := newFakes()
	boom := Permanent(errors.New("exit 1"))
	fk.fail["t"] = []error{boom, boom, boom}
	rf := wait(t, Start(context.Background(), mustParse(t, src), fk.options()))
	st := rf.Steps["fix"]
	if st.State != StateFailed || !strings.Contains(st.Error, "cap of 2 rounds") {
		t.Fatalf("loop %q %q", st.State, st.Error)
	}
	if fk.tried("t") != 2 || fk.tried("e") != 1 {
		t.Fatalf("t tried %d, else ran %d", fk.tried("t"), fk.tried("e"))
	}
}

func TestLoopUntil(t *testing.T) {
	src := `{"version": 1, "name": "w", "steps": [
	  {"id": "l", "type": "loop", "max": 5, "until": "loop.round == 3", "steps": [{"id": "a", "type": "tests", "task": "test"}]}]}`
	fk := newFakes()
	rf := wait(t, Start(context.Background(), mustParse(t, src), fk.options()))
	if rf.State != RunOK || fk.tried("a") != 3 || len(rf.Steps["l"].Loop.Rounds) != 3 {
		t.Fatalf("state %s, a tried %d", rf.State, fk.tried("a"))
	}
}

// costRunner adds a cost to every run step output.
type costRunner struct {
	*fakes
	cost float64
}

func (c costRunner) Run(ctx context.Context, rc RunCall) (Output, error) {
	out, err := c.fakes.Run(ctx, rc)
	out.CostUSD = c.cost
	return out, err
}

func TestLoopStopsAtItsBudget(t *testing.T) {
	src := `{"version": 1, "name": "w", "steps": [
	  {"id": "l", "type": "loop", "max": 10, "budget_usd": 1, "until": "false", "steps": [{"id": "a", "type": "ai", "model": "quick", "prompt": "x"}]}]}`
	fk := newFakes()
	opts := fk.options()
	opts.Run = costRunner{fk, 0.4}
	rf := wait(t, Start(context.Background(), mustParse(t, src), opts))
	st := rf.Steps["l"]
	if st.State != StateFailed || !strings.Contains(st.Error, "budget") || fk.tried("a") != 3 {
		t.Fatalf("loop %q %q, a tried %d", st.State, st.Error, fk.tried("a"))
	}
}

func TestBranchJoinsAllBranches(t *testing.T) {
	src := `{"version": 1, "name": "w", "steps": [
	  {"id": "rev", "type": "branch", "branches": [
	    {"name": "a", "steps": [{"id": "sa", "type": "ai", "model": "quick", "prompt": "A"}]},
	    {"name": "b", "steps": [{"id": "sb", "type": "ai", "model": "quick", "prompt": "B"}]}]},
	  {"id": "sum", "type": "ai", "model": "quick", "prompt": "{{steps.sa.text}}|{{steps.sb.text}}|{{steps.rev.ok}}"}]}`
	fk := newFakes()
	rf := wait(t, Start(context.Background(), mustParse(t, src), fk.options()))
	if rf.State != RunOK {
		t.Fatalf("run %s: %s", rf.State, rf.Error)
	}
	br := rf.Steps["rev"].Branches
	if br["a"] == nil || br["a"].State != StateDone || br["b"] == nil || br["b"].State != StateDone {
		t.Fatalf("branches %+v", br)
	}
	if !slices.Contains(fk.all(), `run sum prompt="out of sa|out of sb|true" wt=`) {
		t.Fatalf("join prompt: %v", fk.all())
	}
}

func TestBranchJoinRules(t *testing.T) {
	for _, tc := range []struct {
		join, state string
	}{{"all", StateFailed}, {"any", StateDone}} {
		t.Run(tc.join, func(t *testing.T) {
			src := `{"version": 1, "name": "w", "steps": [
			  {"id": "rev", "type": "branch", "join": "` + tc.join + `", "branches": [
			    {"name": "a", "steps": [{"id": "sa", "type": "tests", "task": "test"}]},
			    {"name": "b", "steps": [{"id": "sb", "type": "tests", "task": "test"}]}]}]}`
			fk := newFakes()
			fk.fail["sb"] = []error{Permanent(errors.New("exit 1"))}
			rf := wait(t, Start(context.Background(), mustParse(t, src), fk.options()))
			st := rf.Steps["rev"]
			if st.State != tc.state || st.Branches["b"].State != StateFailed {
				t.Fatalf("rev %q (%s), b %q", st.State, st.Error, st.Branches["b"].State)
			}
			if tc.join == "all" && !strings.Contains(st.Error, `branch "b"`) {
				t.Fatalf("error %q does not name the branch", st.Error)
			}
		})
	}
}

// fakeWaiter answers the wait checks in order (the last one repeats).
type fakeWaiter struct {
	mu    sync.Mutex
	say   []WaitStatus
	calls []WaitCall
}

func (w *fakeWaiter) Check(ctx context.Context, c WaitCall) (WaitStatus, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, c)
	i := min(len(w.calls)-1, len(w.say)-1)
	return w.say[i], nil
}

func (w *fakeWaiter) n() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.calls)
}

func TestWaitPollsUntilDone(t *testing.T) {
	src := `{"version": 1, "name": "w", "vars": {"pr": "7"}, "steps": [
	  {"id": "ci", "type": "wait", "for": "ci", "pr": "{{vars.pr}}", "every": "30s"},
	  {"id": "after", "type": "ai", "model": "quick", "prompt": "{{steps.ci.state}} {{steps.ci.url}}"}]}`
	fk := newFakes()
	wt := &fakeWaiter{say: []WaitStatus{{Detail: "2 checks running"}, {Detail: "1 check running"}, {Done: true, OK: true, State: "pass", URL: "https://pr/7"}}}
	var sl sleeps
	var ev events
	opts := fk.options()
	opts.Wait, opts.Sleep, opts.OnEvent = wt, sl.sleep, ev.on
	rf := wait(t, Start(context.Background(), mustParse(t, src), opts))
	if rf.State != RunOK || rf.Steps["ci"].State != StateDone {
		t.Fatalf("run %s: %s", rf.State, rf.Error)
	}
	if wt.n() != 3 || wt.calls[0].PR != "7" || wt.calls[0].For != workflow.WaitCI {
		t.Fatalf("calls %+v", wt.calls)
	}
	if !slices.Equal(sl.d, []time.Duration{30 * time.Second, 30 * time.Second}) {
		t.Fatalf("sleeps %v", sl.d)
	}
	if !slices.Contains(fk.all(), `run after prompt="pass https://pr/7" wt=`) {
		t.Fatalf("output not in scope: %v", fk.all())
	}
	if n := strings.Count(strings.Join(ev.kinds(), "\n"), string(WaitPoll)+" ci"); n != 2 {
		t.Fatalf("%d wait_poll events: %v", n, ev.kinds())
	}
}

func TestWaitFailsWhenCIFails(t *testing.T) {
	src := `{"version": 1, "name": "w", "steps": [{"id": "ci", "type": "wait", "for": "ci", "pr": "7"}]}`
	fk := newFakes()
	opts := fk.options()
	opts.Wait = &fakeWaiter{say: []WaitStatus{{Done: true, State: "fail", Detail: "CI failed: lint"}}}
	rf := wait(t, Start(context.Background(), mustParse(t, src), opts))
	if st := rf.Steps["ci"]; st.State != StateFailed || !strings.Contains(st.Error, "CI failed: lint") {
		t.Fatalf("ci %q %q", st.State, st.Error)
	}
}

func TestWaitGivesUpAtItsTimeout(t *testing.T) {
	src := `{"version": 1, "name": "w", "steps": [{"id": "ci", "type": "wait", "for": "merged", "pr": "7", "every": "30s", "timeout": "1m"}]}`
	fk := newFakes()
	wt := &fakeWaiter{say: []WaitStatus{{Detail: "open"}}}
	var sl sleeps
	opts := fk.options()
	opts.Wait, opts.Sleep = wt, sl.sleep
	rf := wait(t, Start(context.Background(), mustParse(t, src), opts))
	if st := rf.Steps["ci"]; st.State != StateFailed || !strings.Contains(st.Error, "still waiting after 1m0s") {
		t.Fatalf("ci %q %q", st.State, st.Error)
	}
	if wt.n() != 3 {
		t.Fatalf("%d checks, want 3", wt.n())
	}
}

func TestWaitDryRunDoesNotPoll(t *testing.T) {
	src := `{"version": 1, "name": "w", "steps": [{"id": "ci", "type": "wait", "for": "ci", "pr": "7"}]}`
	fk := newFakes()
	wt := &fakeWaiter{say: []WaitStatus{{Done: true, OK: true}}}
	opts := fk.options()
	opts.Wait, opts.Dry = wt, true
	rf := wait(t, Start(context.Background(), mustParse(t, src), opts))
	if rf.Steps["ci"].State != StateWouldRun || wt.n() != 0 {
		t.Fatalf("dry: %q, %d checks", rf.Steps["ci"].State, wt.n())
	}
}

func TestHandoffAddsTheEarlierOutput(t *testing.T) {
	src := `{"version": 1, "name": "w", "steps": [
	  {"id": "a", "type": "ai", "model": "quick", "prompt": "A"},
	  {"id": "b", "type": "agent", "agent": "codex", "prompt": "B", "handoff": "a"}]}`
	fk := newFakes()
	rf := wait(t, Start(context.Background(), mustParse(t, src), fk.options()))
	if rf.State != RunOK {
		t.Fatalf("run %s: %s", rf.State, rf.Error)
	}
	var got string
	for _, c := range fk.all() {
		if strings.HasPrefix(c, "run b ") {
			got = c
		}
	}
	for _, want := range []string{"B", `Handoff from step \"a\"`, "out of a", "not instructions"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt %s lacks %s", got, want)
		}
	}
}
