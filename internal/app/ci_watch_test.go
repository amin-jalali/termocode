package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/statusbar"
)

// ciFakeGH answers gh like the real CLI from fixed JSON; it never starts
// a process. runs is the `gh run list` answer.
type ciFakeGH struct {
	mu    sync.Mutex
	runs  string
	pr    string // "" = no pull request
	log   string // `gh run view --log-failed`
	err   error  // every call fails with it
	calls []string
}

func (f *ciFakeGH) run(_ context.Context, _ string, args ...string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, strings.Join(args, " "))
	if f.err != nil {
		return "", "", f.err
	}
	switch {
	case len(args) > 1 && args[0] == "run" && args[1] == "list":
		return f.runs, "", nil
	case len(args) > 1 && args[0] == "pr":
		if f.pr == "" {
			return "", "no pull requests found for branch", errors.New("exit status 1")
		}
		return f.pr, "", nil
	case len(args) > 1 && args[0] == "run" && args[1] == "view":
		return f.log, "", nil
	}
	return "", "unknown", errors.New("exit status 1")
}

func (f *ciFakeGH) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

const ciRedRuns = `[{"databaseId":30,"workflowName":"CI","status":"completed","conclusion":"failure","headSha":"bbb","url":"https://github.com/o/r/actions/runs/30"},
 {"databaseId":31,"workflowName":"Lint","status":"completed","conclusion":"failure","headSha":"bbb","url":"u31"}]`

const ciPendingRuns = `[{"databaseId":30,"workflowName":"CI","status":"in_progress","conclusion":"","headSha":"bbb","url":"u30"}]`

// ciModel is a repo model on branch with a fake gh.
func ciModel(branch string, f *ciFakeGH) *Model {
	m := &Model{w: 160, h: 40, gitIsRepo: true, gitLoaded: true, gitBranch: git.Branch{Name: branch}}
	m.ciW().run = f.run
	return m
}

// ciRun runs a watcher command and feeds its message back (one step).
func ciRun(t *testing.T, m *Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command")
	}
	msg := cmd()
	next, ok := m.handleCIMsg(msg)
	if !ok {
		t.Fatalf("message %T not handled", msg)
	}
	return next
}

// A new branch asks gh at once and the chip shows its red runs.
func TestCIWatchNewBranchShowsChip(t *testing.T) {
	f := &ciFakeGH{runs: ciRedRuns}
	m := ciModel("feat/x", f)
	next := ciRun(t, m, m.ciOnGitSnapshot(git.Branch{Name: "main"}))
	if next == nil {
		t.Error("no next poll scheduled")
	}
	var st statusbar.State
	m.fillCIStatus(&st)
	if st.CI != statusbar.CIFail || st.CIFailed != 2 {
		t.Errorf("chip %v failed %d", st.CI, st.CIFailed)
	}
	if !strings.HasPrefix(f.calls[0], "run list --branch feat/x") {
		t.Errorf("calls %v", f.calls)
	}
}

// The same branch with nothing pushed does not ask gh; a push (ahead
// went to 0) does, and so does the next tick.
func TestCIWatchPushTriggers(t *testing.T) {
	f := &ciFakeGH{runs: ciPendingRuns}
	m := ciModel("feat/x", f)
	ciRun(t, m, m.ciOnGitSnapshot(git.Branch{}))
	n := f.count()
	m.gitBranch.Ahead = 1
	if cmd := m.ciOnGitSnapshot(git.Branch{Name: "feat/x"}); cmd != nil {
		t.Error("a commit (ahead 1) asked gh")
	}
	m.gitBranch.Ahead = 0
	ciRun(t, m, m.ciOnGitSnapshot(git.Branch{Name: "feat/x", Ahead: 1}))
	if f.count() == n || m.ci.pushAt.IsZero() {
		t.Fatalf("push did not ask gh (%d calls)", f.count())
	}
	var st statusbar.State
	m.fillCIStatus(&st)
	if st.CI != statusbar.CIPending {
		t.Errorf("chip %v, want pending", st.CI)
	}
}

// An old tick chain stops; a tick while the window is in the background
// waits without asking gh; a current tick fetches.
func TestCIWatchTicks(t *testing.T) {
	f := &ciFakeGH{runs: ciPendingRuns}
	m := ciModel("feat/x", f)
	ciRun(t, m, m.ciOnGitSnapshot(git.Branch{}))
	n := f.count()
	if cmd, _ := m.handleCIMsg(ciTickMsg{gen: m.ci.gen - 1}); cmd != nil {
		t.Error("stale tick fetched")
	}
	m.termBlurred = true
	if cmd, _ := m.handleCIMsg(ciTickMsg{gen: m.ci.gen}); cmd == nil || m.ci.busy {
		t.Error("blurred tick: want a retry timer and no fetch")
	}
	m.termBlurred = false
	cmd, _ := m.handleCIMsg(ciTickMsg{gen: m.ci.gen})
	ciRun(t, m, cmd)
	if f.count() == n {
		t.Error("current tick did not fetch")
	}
}

// No gh, no login or offline: no chip, polling goes on slowly.
func TestCIWatchQuietErrors(t *testing.T) {
	f := &ciFakeGH{err: ghci.ErrNoGH}
	m := ciModel("feat/x", f)
	if next := ciRun(t, m, m.ciOnGitSnapshot(git.Branch{})); next == nil {
		t.Error("no next poll after an error")
	}
	var st statusbar.State
	m.fillCIStatus(&st)
	if st.CI != statusbar.CIOff || !errors.Is(m.ci.err, ghci.ErrNoGH) {
		t.Errorf("chip %v err %v", st.CI, m.ci.err)
	}
}

// Leaving the repo or the branch hides the chip.
func TestCIWatchBranchGone(t *testing.T) {
	f := &ciFakeGH{runs: ciRedRuns}
	m := ciModel("feat/x", f)
	ciRun(t, m, m.ciOnGitSnapshot(git.Branch{}))
	m.gitBranch.Name = "other"
	var st statusbar.State
	m.fillCIStatus(&st)
	if st.CI != statusbar.CIOff {
		t.Error("chip of the old branch shows")
	}
	m.gitIsRepo = false
	m.ciOnGitSnapshot(git.Branch{Name: "other"})
	if m.ci.branch != "" {
		t.Error("watcher kept a branch outside a repo")
	}
}

// Tests and other headless runs never start the real gh.
func TestCIWatchHeadlessNeverRunsGH(t *testing.T) {
	w := &ciWatch{}
	if _, _, err := w.runner()(context.Background(), t.TempDir(), "run", "list"); !errors.Is(err, ghci.ErrNoGH) {
		t.Fatalf("headless runner err %v", err)
	}
}

// The toast comes once when the branch turns red (with the key and a
// click for Fix with AI), not again for the same commit, and not when the
// branch was already red at the first look.
func TestCIRedToastOnce(t *testing.T) {
	f := &ciFakeGH{runs: ciPendingRuns}
	m := ciModel("feat/x", f)
	ciRun(t, m, m.ciOnGitSnapshot(git.Branch{}))
	if !m.toast.Empty() {
		t.Fatal("toast for a pending run")
	}
	f.runs = ciRedRuns
	ciRun(t, m, m.ciTrigger())
	cards := m.toast.Cards(80)
	if len(cards) != 1 {
		t.Fatalf("toasts %d, want 1", len(cards))
	}
	if got := ansi.Strip(strings.Join(cards[0].Lines, "\n")); !strings.Contains(got, "CI failed on feat/x") || !strings.Contains(got, "Alt+C Fix with AI") {
		t.Errorf("toast %q", got)
	}
	ciRun(t, m, m.ciTrigger())
	if cards[0].Click == nil {
		t.Error("toast is not clickable")
	}
	if n := len(m.toast.Cards(80)); n != 1 {
		t.Errorf("toasts %d after the same red commit", n)
	}
	// Already red at the first look: the chip, no toast.
	m2 := ciModel("feat/y", &ciFakeGH{runs: ciRedRuns})
	ciRun(t, m2, m2.ciOnGitSnapshot(git.Branch{}))
	if !m2.toast.Empty() {
		t.Error("toast for a branch that was red before")
	}
	// A click on the toast is Fix with AI.
	if cmd, ok := m.handleCIMsg(ciFixClickMsg{}); !ok || cmd == nil {
		t.Error("toast click did nothing")
	}
}
