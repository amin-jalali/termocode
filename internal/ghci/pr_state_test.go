package ghci

import (
	"context"
	"strings"
	"testing"
)

// SetClosed closes (`gh pr close N`) or reopens (`gh pr reopen N`).
func TestSetClosed(t *testing.T) {
	for _, c := range []struct {
		closed bool
		want   string
	}{{true, "pr close 7"}, {false, "pr reopen 7"}} {
		f := &fakeGH{answers: map[string]fakeAnswer{"pr close": {}, "pr reopen": {}}}
		if err := SetClosed(context.Background(), f.run, "/r", 7, c.closed); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(f.calls[0], " "); got != c.want {
			t.Errorf("args %q want %q", got, c.want)
		}
	}
}

// FailedRunIDs reads the workflow runs of the failed checks from their
// links, once each; passing checks and status links are skipped.
func TestFailedRunIDs(t *testing.T) {
	cs := []Check{
		{Name: "test", State: StateFail, URL: "https://github.com/o/r/actions/runs/123/job/9"},
		{Name: "lint", State: StateFail, URL: "https://github.com/o/r/actions/runs/123/job/10"},
		{Name: "e2e", State: StateFail, URL: "https://github.com/o/r/actions/runs/456/job/1"},
		{Name: "build", State: StatePass, URL: "https://github.com/o/r/actions/runs/789/job/2"},
		{Name: "deploy", State: StateFail, URL: "https://ci.example.com/x"},
	}
	if got := FailedRunIDs(cs); len(got) != 2 || got[0] != 123 || got[1] != 456 {
		t.Errorf("ids %v", got)
	}
}

// RerunFailed runs only the failed jobs of each run.
func TestRerunFailed(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"run rerun": {}}}
	if err := RerunFailed(context.Background(), f.run, "/r", []int64{123, 456}); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 || strings.Join(f.calls[0], " ") != "run rerun 123 --failed" || strings.Join(f.calls[1], " ") != "run rerun 456 --failed" {
		t.Errorf("calls %v", f.calls)
	}
	if err := RerunFailed(context.Background(), (&fakeGH{}).run, "/r", nil); err == nil {
		t.Error("nothing to re-run accepted")
	}
}
