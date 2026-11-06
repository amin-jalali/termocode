package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// Edit Description… writes the text to a file in the git folder and
// opens it; a save shows a card (Cancel first) that sends the file with
// gh pr edit --body-file; Cancel sends nothing.
func TestPREditDescription(t *testing.T) {
	f := &prFakeGH{answers: map[string]string{"pr view": prViewText14, "pr edit": ""}}
	m, dir := prRepoModel(t, f)
	prDo(t, m, m.prViewCmd(14))
	m.onPRActionPicked("edit-description")
	e := m.prS().editBody
	if e == nil || !strings.HasPrefix(e.path, filepath.Join(dir, ".git")) {
		t.Fatalf("edit file %+v", e)
	}
	b, err := os.ReadFile(e.path)
	if err != nil || !strings.HasPrefix(string(b), "Adds a parser.") {
		t.Fatalf("file %q %v", b, err)
	}
	m.syncPRBodyFile() // not saved yet: nothing
	if m.confirmOpen && m.confirmKind == confirmKindPREditBody {
		t.Fatal("card before a save")
	}
	if err := os.WriteFile(e.path, []byte("New text.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(e.path, later, later)
	m.syncPRBodyFile()
	if !m.confirmOpen || m.confirmKind != confirmKindPREditBody {
		t.Fatal("no card after the save")
	}
	bs := m.confirm.Buttons()
	if len(bs) != 2 || bs[0].ID != "cancel" || bs[1].ID != "update-body" {
		t.Fatalf("buttons %+v", bs)
	}
	if !strings.Contains(m.confirm.Box(), "New text.") {
		t.Errorf("card does not show the text:\n%s", m.confirm.Box())
	}
	if cmd, _ := m.handlePRConfirm("cancel"); cmd != nil || f.called("pr edit") {
		t.Fatal("Cancel sent the text")
	}
	later = later.Add(2 * time.Second)
	_ = os.Chtimes(e.path, later, later)
	m.syncPRBodyFile()
	cmd, _ := m.handlePRConfirm("update-body")
	done := cmd().(prDoneMsg)
	if done.err != nil || !done.reopen || !f.called("pr edit 14 --body-file "+e.path) {
		t.Errorf("done %+v calls %v", done, f.calls)
	}
}
