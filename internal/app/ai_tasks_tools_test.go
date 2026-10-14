package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/tests"
)

// Spec: the waits of tasks_status / tests_results are bounded and Stop
// cancels them at once.
func TestAIPollUntilBoundedAndCancellable(t *testing.T) {
	never := func() (bool, error) { return false, nil }
	start := time.Now()
	timedOut, err := aiPollUntil(context.Background(), 200*time.Millisecond, 20*time.Millisecond, never)
	if err != nil || !timedOut || time.Since(start) > 2*time.Second {
		t.Errorf("timeout: timedOut %v err %v after %s", timedOut, err, time.Since(start))
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	start = time.Now()
	_, err = aiPollUntil(ctx, time.Hour, 20*time.Millisecond, never)
	if err != errAIChatStopped || time.Since(start) > 2*time.Second {
		t.Errorf("cancel: err %v after %s", err, time.Since(start))
	}
	n := 0
	timedOut, err = aiPollUntil(context.Background(), 0, time.Millisecond, func() (bool, error) { n++; return true, nil })
	if timedOut || err != nil || n != 1 {
		t.Errorf("done at once: timedOut %v err %v calls %d", timedOut, err, n)
	}
}

func k6Tree(root string, failed int) *tests.Tree {
	f := &tests.File{Path: root + "/calc_test.go", Rel: "calc_test.go"}
	f.Cases = append(f.Cases, &tests.Case{Name: "TestGood", File: f.Path, Line: 5, Status: tests.StatusPassed})
	for i := 0; i < failed; i++ {
		f.Cases = append(f.Cases, &tests.Case{Name: fmt.Sprintf("TestBad%d", i), File: f.Path, Line: 9,
			Status: tests.StatusFailed, Message: "want 5\ngot 4\n" + strings.Repeat("x", 500),
			FailFile: f.Path, FailLine: 10 + i})
	}
	return &tests.Tree{Root: root, FW: tests.FrameworkGo, Packages: []*tests.Package{{Name: "k6", Dir: root, Files: []*tests.File{f}}}}
}

// Spec: tests_results lists the failures with a workspace-relative
// file:line and a short message, points to open_file, and caps the list.
func TestTestsResultsReport(t *testing.T) {
	root := "/w"
	st := newTestsState()
	if got := testsResultsReport(st, root, false, false, 0); !strings.Contains(got, "No test run yet") {
		t.Errorf("no run: %q", got)
	}
	st.tree, st.hasRun, st.elapsed = k6Tree(root, 1), true, time.Second
	got := testsResultsReport(st, root, false, false, 0)
	for _, want := range []string{"finished in 1s: 1 passed, 1 failed, 0 skipped", "1 failed:\n- TestBad0 (calc_test.go:10): want 5\n    got 4",
		"open_file", "Passed: TestGood"} {
		if !strings.Contains(got, want) {
			t.Errorf("report has no %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, strings.Repeat("x", aiTestsMsgWidth+1)) {
		t.Errorf("message not cut:\n%s", got)
	}
	if got := testsResultsReport(st, root, true, false, 0); strings.Contains(got, "TestGood") {
		t.Errorf("only_failed lists passed tests:\n%s", got)
	}
	st.tree = k6Tree(root, aiTestsMaxFailures+5)
	if got := testsResultsReport(st, root, true, false, 0); !strings.Contains(got, "… and 5 more failed tests") {
		t.Errorf("not capped:\n%s", got)
	}
	st.running = true
	if got := testsResultsReport(st, root, false, true, 2*time.Second); !strings.Contains(got, "still running after 2s") {
		t.Errorf("busy:\n%s", got)
	}
}
