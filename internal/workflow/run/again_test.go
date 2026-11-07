package run

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/workflow"
)

// Retry (CMP-45): a run that ended with a failed, denied, stopped or
// interrupted part runs again from that part. Finished items and the
// steps before the first bad one are kept; the rest runs again.
func TestRetryRunsOnlyTheFailedItemAgain(t *testing.T) {
	src := strings.Replace(flow, `"when": "item.ready"`, `"when": "true"`, 1)
	fk := newFakes(items("a", "c")...)
	fk.fail["code/a"] = []error{Permanent(errors.New("exit 1"))}
	file := mustParse(t, src)
	rf := wait(t, Start(context.Background(), file, fk.options()))
	its := rf.Steps["fix"].Each.Items
	if its[0].State != StateFailed || its[1].State != StateDone {
		t.Fatalf("first run items %q %q", its[0].State, its[1].State)
	}

	again, err := Retry(rf, file)
	if err != nil {
		t.Fatal(err)
	}
	if again.Steps["bugs"].State != StateDone || again.Steps["fix"].State != StatePending {
		t.Fatalf("retry file: bugs %q fix %q", again.Steps["bugs"].State, again.Steps["fix"].State)
	}
	if rf.Steps["fix"].Each.Items[0].State != StateFailed {
		t.Fatal("Retry changed the run file it was given")
	}
	opts := fk.options()
	opts.Resume = again
	end := wait(t, Start(context.Background(), file, opts))
	if end.State != RunOK || end.Resumed != 1 {
		t.Fatalf("after retry %q resumed %d (%s)", end.State, end.Resumed, end.Error)
	}
	got := end.Steps["fix"].Each.Items
	if got[0].State != StateDone || got[1].State != StateDone {
		t.Fatalf("items after retry %q %q", got[0].State, got[1].State)
	}
	if fk.tried("code/a") != 2 || fk.tried("code/c") != 1 {
		t.Fatalf("tries a=%d c=%d, want 2 and 1 (a done item is not run again)", fk.tried("code/a"), fk.tried("code/c"))
	}
	n := 0
	for _, c := range fk.all() {
		if strings.HasPrefix(c, "source bugs") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the source ran %d times; a kept step is not read again", n)
	}
}

// A failed top-level step runs again, and so does every step after it
// (they may have used its missing answer).
func TestRetryFromTheFirstFailedStep(t *testing.T) {
	src := `{"version": 1, "name": "w", "on_error": "stop", "steps": [
	  {"id": "a", "type": "tests", "task": "test"},
	  {"id": "b", "type": "tests", "task": "test"},
	  {"id": "c", "type": "tests", "task": "test"}]}`
	fk := newFakes()
	fk.fail["b"] = []error{Permanent(errors.New("exit 1"))}
	file := mustParse(t, src)
	rf := wait(t, Start(context.Background(), file, fk.options()))
	if rf.State != RunFail || rf.Steps["b"].State != StateFailed {
		t.Fatalf("first run %q b %q", rf.State, rf.Steps["b"].State)
	}
	again, err := Retry(rf, file)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Steps["b"]; ok && again.Steps["b"].State != StatePending {
		t.Fatalf("b kept %q", again.Steps["b"].State)
	}
	opts := fk.options()
	opts.Resume = again
	end := wait(t, Start(context.Background(), file, opts))
	if end.State != RunOK || fk.tried("a") != 1 || fk.tried("b") != 2 || fk.tried("c") != 1 {
		t.Fatalf("after retry %q tries a=%d b=%d c=%d", end.State, fk.tried("a"), fk.tried("b"), fk.tried("c"))
	}
}

// Nothing to retry: a good run, a dry run, a run that still works, or a
// changed file.
func TestRetryRefuses(t *testing.T) {
	src := `{"version": 1, "name": "w", "steps": [{"id": "a", "type": "tests", "task": "test"}]}`
	file := mustParse(t, src)
	fk := newFakes()
	ok := wait(t, Start(context.Background(), file, fk.options()))
	if _, err := Retry(ok, file); !errors.Is(err, ErrNothingToRetry) {
		t.Fatalf("good run: %v", err)
	}
	bad := ok.clone()
	bad.Steps["a"].State = StateFailed
	for name, tc := range map[string]struct {
		edit func(rf *RunFile)
		file *workflow.File
	}{
		"dry":     {edit: func(rf *RunFile) { rf.Dry = true }, file: file},
		"running": {edit: func(rf *RunFile) { rf.State = RunRunning }, file: file},
		"changed": {edit: func(rf *RunFile) { rf.Hash = "other" }, file: file},
	} {
		rf := bad.clone()
		tc.edit(rf)
		if _, err := Retry(rf, tc.file); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := Retry(bad, file); err != nil {
		t.Fatalf("a failed step: %v", err)
	}
}

// Plan approval (CMP-45): a plan step with "approve": true asks before
// its order is used. No: the step is denied and nothing after it runs
// on its answer. A dry run does not ask. Without anyone to ask, the step
// fails (never goes on unapproved).
func TestPlanApproval(t *testing.T) {
	src := `{"version": 1, "name": "w", "on_error": "stop", "steps": [
	  {"id": "bugs", "type": "source", "from": "github", "what": "issues"},
	  {"id": "order", "type": "plan", "model": "quick", "approve": true},
	  {"id": "t", "type": "tests", "task": "test"}]}`
	file := mustParse(t, src)
	if !file.Steps[1].Plan.Approve {
		t.Fatal(`"approve" is not read`)
	}
	cases := []struct {
		name   string
		dry    bool
		ask    func(ctx context.Context, c ApproveCall) (bool, error)
		state  string
		asked  int
		testsN int
	}{
		{name: "yes", ask: func(context.Context, ApproveCall) (bool, error) { return true, nil }, state: StateDone, asked: 1, testsN: 1},
		{name: "no", ask: func(context.Context, ApproveCall) (bool, error) { return false, nil }, state: StateDenied, asked: 1},
		{name: "nobody", state: StateFailed},
		{name: "dry", dry: true, ask: func(context.Context, ApproveCall) (bool, error) { return false, nil }, state: StateDone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fk := newFakes(items("a", "b")...)
			fk.plan = PlanResult{Order: []string{"b", "a"}}
			opts := fk.options()
			opts.Dry = tc.dry
			var seen []ApproveCall
			if tc.ask != nil {
				opts.Approve = func(ctx context.Context, c ApproveCall) (bool, error) {
					seen = append(seen, c)
					return tc.ask(ctx, c)
				}
			}
			var ev events
			opts.OnEvent = ev.on
			rf := wait(t, Start(context.Background(), file, opts))
			if got := rf.Steps["order"].State; got != tc.state {
				t.Fatalf("plan state %q, want %q (%s)", got, tc.state, rf.Steps["order"].Error)
			}
			if len(seen) != tc.asked {
				t.Fatalf("asked %d times, want %d", len(seen), tc.asked)
			}
			if tc.asked > 0 {
				c := seen[0]
				if c.Step == nil || stepKey(c.Step) != "order" || len(c.Items) != 2 || c.Items[0].Plan == nil || c.Items[1].Plan.Rank != 1 {
					t.Fatalf("ask %+v", c)
				}
				if !slices.Contains(ev.kinds(), "plan_wait order") {
					t.Fatalf("no plan_wait event: %q", ev.kinds())
				}
			}
			ran := fk.tried("t")
			if !tc.dry && ran != tc.testsN {
				t.Fatalf("tests ran %d times, want %d", ran, tc.testsN)
			}
		})
	}
}

// A denied plan is not tried again by the step's retries.
func TestPlanDenyIsNotRetried(t *testing.T) {
	src := `{"version": 1, "name": "w", "retries": {"max": 3}, "steps": [
	  {"id": "bugs", "type": "source", "from": "github", "what": "issues"},
	  {"id": "order", "type": "plan", "approve": true}]}`
	fk := newFakes(items("a")...)
	opts := fk.options()
	n := 0
	opts.Approve = func(context.Context, ApproveCall) (bool, error) { n++; return false, nil }
	rf := wait(t, Start(context.Background(), mustParse(t, src), opts))
	if n != 1 || rf.Steps["order"].State != StateDenied {
		t.Fatal(fmt.Sprintf("asked %d, state %q", n, rf.Steps["order"].State))
	}
}
