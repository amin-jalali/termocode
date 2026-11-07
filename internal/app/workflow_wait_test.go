package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/workflow"
)

// CMP-42: a workflow wait step reads the pull request with gh and
// decides: done and good, done and bad, or still waiting.

func TestWFXWaitStatus(t *testing.T) {
	pass := ghci.Check{Name: "test", State: ghci.StatePass}
	fail := ghci.Check{Name: "lint", State: ghci.StateFail}
	pend := ghci.Check{Name: "build", State: ghci.StatePending}
	cases := []struct {
		name, for_ string
		pr         *ghci.PR
		done, ok   bool
		detail     string
	}{
		{"ci pass", workflow.WaitCI, &ghci.PR{State: "OPEN", Checks: []ghci.Check{pass, pass}}, true, true, "2 checks passed"},
		{"ci fail", workflow.WaitCI, &ghci.PR{State: "OPEN", Checks: []ghci.Check{pass, fail, pend}}, true, false, "CI failed: lint"},
		{"ci pending", workflow.WaitCI, &ghci.PR{State: "OPEN", Checks: []ghci.Check{pass, pend}}, false, false, "1 of 2 checks still running"},
		{"ci none yet", workflow.WaitCI, &ghci.PR{State: "OPEN"}, false, false, "no checks yet"},
		{"merged", workflow.WaitMerged, &ghci.PR{State: "MERGED"}, true, true, "merged"},
		{"closed", workflow.WaitMerged, &ghci.PR{State: "CLOSED"}, true, false, "closed without a merge"},
		{"open", workflow.WaitMerged, &ghci.PR{State: "OPEN"}, false, false, "still open"},
		{"approved", workflow.WaitApproved, &ghci.PR{State: "OPEN", Review: "APPROVED"}, true, true, "approved"},
		{"changes", workflow.WaitApproved, &ghci.PR{State: "OPEN", Review: "CHANGES_REQUESTED"}, true, false, "changes requested"},
		{"review wait", workflow.WaitApproved, &ghci.PR{State: "OPEN", Review: "REVIEW_REQUIRED"}, false, false, "waiting for a review"},
		{"no pr", workflow.WaitCI, nil, true, false, "no pull request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := wfxWaitStatus(tc.pr, tc.for_)
			if s.Done != tc.done || s.OK != tc.ok || !strings.Contains(s.Detail, tc.detail) {
				t.Fatalf("got %+v, want done %v ok %v detail %q", s, tc.done, tc.ok, tc.detail)
			}
		})
	}
}

func TestWFXWaitAsksGH(t *testing.T) {
	f := &fakeGH{reply: map[string]string{"pr view": `{"number":7,"state":"MERGED","url":"https://github.test/o/r/pull/7"}`}}
	g := &wfxGitHub{dir: "/ws", gh: f.run}
	s, err := g.Wait(context.Background(), "https://github.test/o/r/pull/7", workflow.WaitMerged)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Done || !s.OK || s.URL != "https://github.test/o/r/pull/7" {
		t.Fatalf("status %+v", s)
	}
	if len(f.calls) != 1 || !strings.HasPrefix(f.calls[0], "/ws: pr view https://github.test/o/r/pull/7 --json") {
		t.Fatalf("calls %v", f.calls)
	}
}
