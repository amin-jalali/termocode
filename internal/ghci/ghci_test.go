package ghci

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// fakeGH answers gh calls from a table keyed by the first two args; it
// records every call. It never starts a process.
type fakeGH struct {
	answers map[string]fakeAnswer
	calls   [][]string
	dirs    []string
}

type fakeAnswer struct {
	out, errOut string
	err         error
}

func (f *fakeGH) run(_ context.Context, dir string, args ...string) (string, string, error) {
	f.calls = append(f.calls, args)
	f.dirs = append(f.dirs, dir)
	key := strings.Join(args[:min(2, len(args))], " ")
	a, ok := f.answers[key]
	if !ok {
		return "", "unknown command " + key, errors.New("exit status 1")
	}
	return a.out, a.errOut, a.err
}

const runsJSON = `[
 {"databaseId":30,"workflowName":"CI","displayTitle":"fix","status":"completed","conclusion":"failure","headSha":"bbb","url":"https://github.com/o/r/actions/runs/30"},
 {"databaseId":29,"workflowName":"Lint","displayTitle":"fix","status":"in_progress","conclusion":"","headSha":"bbb","url":"u29"},
 {"databaseId":28,"workflowName":"CI","displayTitle":"fix","status":"completed","conclusion":"success","headSha":"bbb","url":"u28"},
 {"databaseId":20,"workflowName":"CI","displayTitle":"old","status":"completed","conclusion":"success","headSha":"aaa","url":"u20"}
]`

const prJSONText = `{"number":12,"title":"Add parser","state":"OPEN","isDraft":false,"url":"https://github.com/o/r/pull/12",
 "reviewDecision":"CHANGES_REQUESTED","statusCheckRollup":[
  {"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"FAILURE","detailsUrl":"d1"},
  {"__typename":"CheckRun","name":"lint","status":"IN_PROGRESS","conclusion":"","detailsUrl":"d2"},
  {"__typename":"CheckRun","name":"build","status":"COMPLETED","conclusion":"SUCCESS","detailsUrl":"d3"},
  {"__typename":"StatusContext","context":"deploy","state":"PENDING","targetUrl":"t1"}]}`

// Fetch keeps the newest run per workflow of the newest commit, reads
// the PR with its checks, and asks gh with fixed args in the repo dir.
func TestFetch(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{
		"run list": {out: runsJSON},
		"pr view":  {out: prJSONText},
	}}
	s, err := Fetch(context.Background(), f.run, "/repo", "feat/x")
	if err != nil {
		t.Fatal(err)
	}
	if s.HeadSHA != "bbb" || len(s.Runs) != 2 || s.Runs[0].ID != 30 || s.Runs[1].ID != 29 {
		t.Fatalf("runs %+v sha %q", s.Runs, s.HeadSHA)
	}
	if s.State() != StateFail || len(s.FailedRuns()) != 1 {
		t.Errorf("state %v failed %d", s.State(), len(s.FailedRuns()))
	}
	if s.PR == nil || s.PR.Number != 12 || s.PR.Review != "CHANGES_REQUESTED" || len(s.PR.Checks) != 4 {
		t.Fatalf("pr %+v", s.PR)
	}
	want := []State{StateFail, StatePending, StatePass, StatePending}
	for i, c := range s.PR.Checks {
		if c.State != want[i] {
			t.Errorf("check %s state %v want %v", c.Name, c.State, want[i])
		}
	}
	if s.PR.Checks[3].Name != "deploy" || s.PR.Checks[3].URL != "t1" {
		t.Errorf("status context %+v", s.PR.Checks[3])
	}
	if got := strings.Join(f.calls[0], " "); !strings.HasPrefix(got, "run list --branch feat/x ") {
		t.Errorf("run list args %q", got)
	}
	if got := strings.Join(f.calls[1], " "); !strings.HasPrefix(got, "pr view feat/x --json ") {
		t.Errorf("pr view args %q", got)
	}
	if f.dirs[0] != "/repo" {
		t.Errorf("dir %q", f.dirs[0])
	}
}

func TestSnapshotState(t *testing.T) {
	pass := Run{Status: "completed", Conclusion: "success"}
	pend := Run{Status: "queued"}
	fail := Run{Status: "completed", Conclusion: "timed_out"}
	skip := Run{Status: "completed", Conclusion: "cancelled"}
	for _, c := range []struct {
		runs []Run
		want State
	}{
		{nil, StateNone},
		{[]Run{pass, skip}, StatePass},
		{[]Run{pass, pend}, StatePending},
		{[]Run{pend, fail}, StateFail},
	} {
		if got := (Snapshot{Runs: c.runs}).State(); got != c.want {
			t.Errorf("%+v: %v want %v", c.runs, got, c.want)
		}
	}
}

// No PR is not an error; no gh, no login and no GitHub remote are quiet.
func TestFetchErrors(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{
		"run list": {out: "[]"},
		"pr view":  {errOut: "no pull requests found for branch \"x\"", err: errors.New("exit status 1")},
	}}
	s, err := Fetch(context.Background(), f.run, "/r", "x")
	if err != nil || s.PR != nil || s.State() != StateNone {
		t.Fatalf("no PR: %+v %v", s, err)
	}
	for _, c := range []struct {
		a    fakeAnswer
		want error
	}{
		{fakeAnswer{err: ErrNoGH}, ErrNoGH},
		{fakeAnswer{errOut: "You are not logged into any GitHub hosts. To log in, run: gh auth login", err: errors.New("exit 4")}, ErrNotLoggedIn},
		{fakeAnswer{errOut: "none of the git remotes configured for this repository point to a known GitHub host", err: errors.New("exit 1")}, ErrNoGitHub},
	} {
		f := &fakeGH{answers: map[string]fakeAnswer{"run list": c.a}}
		_, err := Fetch(context.Background(), f.run, "/r", "x")
		if !errors.Is(err, c.want) || !Quiet(err) {
			t.Errorf("%q: err %v want %v", c.a.errOut, err, c.want)
		}
	}
	f = &fakeGH{answers: map[string]fakeAnswer{"run list": {errOut: "error connecting to api.github.com\ncheck your internet connection", err: errors.New("exit 1")}}}
	_, err = Fetch(context.Background(), f.run, "/r", "x")
	if err == nil || Quiet(err) || err.Error() != "gh: error connecting to api.github.com" {
		t.Errorf("offline err %v", err)
	}
}

// A detached HEAD or a name that looks like a flag never reaches gh.
func TestFetchSkipsOddBranch(t *testing.T) {
	for _, b := range []string{"", "(detached abc123)", "--help"} {
		f := &fakeGH{}
		if _, err := Fetch(context.Background(), f.run, "/r", b); err != nil || len(f.calls) != 0 {
			t.Errorf("%q: err %v calls %v", b, err, f.calls)
		}
	}
}

func TestNextPoll(t *testing.T) {
	pend := Snapshot{Runs: []Run{{Status: "in_progress"}}}
	done := Snapshot{Runs: []Run{{Status: "completed", Conclusion: "success"}}}
	never := time.Duration(-1)
	if NextPoll(pend, nil, never) != FastPoll {
		t.Error("pending run: fast")
	}
	if NextPoll(done, nil, never) != SlowPoll {
		t.Error("finished: slow")
	}
	if NextPoll(done, nil, time.Minute) != FastPoll {
		t.Error("right after a push: fast")
	}
	if NextPoll(done, nil, PushGrace+time.Second) != SlowPoll {
		t.Error("long after a push: slow")
	}
	if NextPoll(pend, errors.New("offline"), time.Second) != SlowPoll {
		t.Error("error: slow")
	}
}

// DefaultRunner without gh on PATH says so and starts nothing.
func TestDefaultRunnerNoGH(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, _, err := DefaultRunner(time.Second)(context.Background(), t.TempDir(), "run", "list")
	if !errors.Is(err, ErrNoGH) {
		t.Fatalf("err %v", err)
	}
}

func ExampleSnapshot_State() {
	s := Snapshot{Runs: []Run{{Status: "completed", Conclusion: "failure"}}}
	fmt.Println(s.State() == StateFail)
	// Output: true
}
