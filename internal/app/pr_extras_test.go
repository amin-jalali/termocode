package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ghci"
)

func prActionIDs(d ghci.PRDetail) string {
	var out []string
	for _, it := range prActionItems(d) {
		out = append(out, it.ID)
	}
	return strings.Join(out, ",")
}

// An open PR can be closed and gets its description edited; failed
// checks with a workflow run can be re-run; a closed PR can be reopened.
func TestPRExtraActionItems(t *testing.T) {
	open := ghci.PRDetail{State: "OPEN", PRItem: ghci.PRItem{Checks: []ghci.Check{{State: ghci.StateFail, URL: "https://github.com/o/r/actions/runs/5/job/1"}}}}
	got := prActionIDs(open)
	for _, want := range []string{"edit-description", "rerun-failed", "close"} {
		if !strings.Contains(got, want) {
			t.Errorf("open PR misses %s: %s", want, got)
		}
	}
	if strings.Contains(prActionIDs(ghci.PRDetail{State: "OPEN"}), "rerun-failed") {
		t.Error("re-run without failed checks")
	}
	if got := prActionIDs(ghci.PRDetail{State: "CLOSED"}); got != "browser,description,reopen" {
		t.Errorf("closed PR offers %s", got)
	}
}

// Close asks first with Cancel focused; Close Pull Request runs gh pr
// close and the view comes back. Reopen runs at once.
func TestPRCloseAndReopen(t *testing.T) {
	m, f := prViewModel(t)
	f.answers["pr close"] = ""
	f.answers["pr reopen"] = ""
	m.onPRActionPicked("close")
	if !m.confirmOpen || m.confirmKind != confirmKindPRClose {
		t.Fatal("close card not open")
	}
	b := m.confirm.Buttons()
	if len(b) != 2 || b[0].ID != "cancel" || b[1].ID != "close-pr" {
		t.Fatalf("buttons %+v", b)
	}
	if cmd, ok := m.handlePRConfirm("cancel"); !ok || cmd != nil || f.called("pr close") {
		t.Fatal("Cancel closed the pull request")
	}
	m.onPRActionPicked("close")
	cmd, _ := m.handlePRConfirm("close-pr")
	done := cmd().(prDoneMsg)
	if done.err != nil || !done.reopen || !f.called("pr close 14") {
		t.Fatalf("done %+v calls %v", done, f.calls)
	}
	m.onPRDone(done)
	if !toastHas(m, "Closed #14") {
		t.Error("no toast")
	}
	m.prS().detail.State = "CLOSED"
	done = m.onPRActionPicked("reopen")().(prDoneMsg)
	if done.err != nil || !f.called("pr reopen 14") {
		t.Errorf("reopen: %+v %v", done, f.calls)
	}
}

// Re-run Failed Checks re-runs each failed workflow run once.
func TestPRRerunFailed(t *testing.T) {
	m, f := prViewModel(t)
	f.answers["run rerun"] = ""
	m.prS().detail.Checks = []ghci.Check{{Name: "test", State: ghci.StateFail, URL: "https://github.com/o/r/actions/runs/77/job/1"}}
	done := m.onPRActionPicked("rerun-failed")().(prDoneMsg)
	if done.err != nil || !f.called("run rerun 77 --failed") || !strings.Contains(done.ok, "Re-running") {
		t.Errorf("done %+v calls %v", done, f.calls)
	}
}
