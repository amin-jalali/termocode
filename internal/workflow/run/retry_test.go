package run

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// sleeps records the backoff waits instead of sleeping.
type sleeps struct {
	mu sync.Mutex
	d  []time.Duration
}

func (s *sleeps) sleep(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	s.d = append(s.d, d)
	s.mu.Unlock()
	return ctx.Err()
}

func TestRetriesWithBackoff(t *testing.T) {
	boom := errors.New("exit 1")
	cases := []struct {
		name    string
		step    string // the step's extra keys
		errs    []error
		tries   int
		state   string
		waits   []time.Duration
		attempt int
	}{
		{name: "ok after two fails", errs: []error{boom, boom}, tries: 3, state: StateDone,
			waits: []time.Duration{time.Minute, 2 * time.Minute}, attempt: 3},
		{name: "fails after all tries", errs: []error{boom, boom, boom, boom}, tries: 3, state: StateFailed,
			waits: []time.Duration{time.Minute, 2 * time.Minute}, attempt: 3},
		{name: "step override", step: `, "retries": {"max": 0}`, errs: []error{boom}, tries: 1, state: StateFailed, attempt: 1},
		{name: "step backoff", step: `, "retries": {"max": 1, "backoff": "10s"}`, errs: []error{boom}, tries: 2, state: StateDone,
			waits: []time.Duration{10 * time.Second}, attempt: 2},
		{name: "permanent is not retried", errs: []error{Permanent(boom)}, tries: 1, state: StateFailed, attempt: 1},
		{name: "denied is not retried", errs: []error{fmt.Errorf("card: %w", ErrDenied)}, tries: 1, state: StateDenied, attempt: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := `{"version": 1, "name": "w", "retries": {"max": 2, "backoff": "1m"}, "steps": [
			  {"id": "t", "type": "tests", "task": "test"` + tc.step + `}]}`
			fk := newFakes()
			fk.fail["t"] = tc.errs
			var sl sleeps
			opts := fk.options()
			opts.Sleep = sl.sleep
			rf := wait(t, Start(context.Background(), mustParse(t, src), opts))
			st := rf.Steps["t"]
			if fk.tried("t") != tc.tries || st.State != tc.state || st.Attempts != tc.attempt {
				t.Fatalf("tries %d state %q attempts %d, want %d %q %d (%s)", fk.tried("t"), st.State, st.Attempts, tc.tries, tc.state, tc.attempt, st.Error)
			}
			if !slices.Equal(sl.d, tc.waits) {
				t.Fatalf("waits %v, want %v", sl.d, tc.waits)
			}
		})
	}
}

func TestBackoffIsCapped(t *testing.T) {
	if got := backoff(time.Hour, 9); got != MaxBackoff {
		t.Fatalf("backoff %v, want %v", got, MaxBackoff)
	}
	if got := backoff(0, 3); got != 0 {
		t.Fatalf("no backoff set: %v", got)
	}
}

func TestStepTimeoutIsRetried(t *testing.T) {
	src := `{"version": 1, "name": "w", "steps": [
	  {"id": "t", "type": "tests", "task": "test", "timeout": "20ms", "retries": {"max": 1}}]}`
	fk := newFakes()
	fk.slow["t"] = true
	rf := wait(t, Start(context.Background(), mustParse(t, src), fk.options()))
	st := rf.Steps["t"]
	if rf.State != RunFail || st.State != StateFailed || fk.tried("t") != 2 || st.Error != "timed out after 20ms" {
		t.Fatalf("run %q step %q tries %d err %q", rf.State, st.State, fk.tried("t"), st.Error)
	}
}

func TestItemTimeoutFailsOnlyThatItem(t *testing.T) {
	src := `{"version": 1, "name": "w", "limits": {"item_timeout": "30ms"}, "retries": {"max": 3}, "steps": [
	  {"id": "bugs", "type": "source", "from": "github", "what": "issues"},
	  {"id": "fix", "type": "each", "items": "steps.bugs.items", "steps": [{"id": "code", "type": "tests", "task": "test"}]}]}`
	fk := newFakes(items("a", "c")...)
	fk.slow["code/a"] = true
	rf := wait(t, Start(context.Background(), mustParse(t, src), fk.options()))
	its := rf.Steps["fix"].Each.Items
	if rf.State != RunOK || its[0].State != StateFailed || its[1].State != StateDone {
		t.Fatalf("run %q items %q %q (%s)", rf.State, its[0].State, its[1].State, rf.Error)
	}
	if e := its[0].Steps["code"].Error; e != "item timed out after 30ms" || fk.tried("code/a") != 1 {
		t.Fatalf("code error %q tries %d", e, fk.tried("code/a"))
	}
}

func TestRunTimeout(t *testing.T) {
	src := `{"version": 1, "name": "w", "limits": {"timeout": "30ms"}, "steps": [
	  {"id": "bugs", "type": "source", "from": "github", "what": "issues"},
	  {"id": "fix", "type": "each", "items": "steps.bugs.items", "steps": [{"id": "code", "type": "tests", "task": "test"}]}]}`
	fk := newFakes(items("a", "c")...)
	fk.slow["code/a"] = true
	rf := wait(t, Start(context.Background(), mustParse(t, src), fk.options()))
	its := rf.Steps["fix"].Each.Items
	if rf.State != RunFail || !strings.Contains(rf.Error, "the run timed out after 30ms") || its[1].State != StatePending {
		t.Fatalf("run %q %q items %q %q", rf.State, rf.Error, its[0].State, its[1].State)
	}
}
