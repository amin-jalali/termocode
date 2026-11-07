package app

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// wtShipFakes swaps the git calls of the worktree ship: dirty counts
// (one per call, the last one repeats), the diff, the commit and the
// folder removal of the archive step.
type wtShipFakes struct {
	mu      sync.Mutex // the fakes run on the commands' goroutines
	dirty   []int
	commits []string
	removed []string
}

func wtShipSwap(t *testing.T, dirty ...int) *wtShipFakes {
	t.Helper()
	f := &wtShipFakes{dirty: dirty}
	oldDirty, oldDiff, oldCommit, oldRemove := wtDirtyRun, wtShipDiffRun, wtShipCommitRun, wtRemoveRun
	wtDirtyRun = func(string) (int, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		n := f.dirty[0]
		if len(f.dirty) > 1 {
			f.dirty = f.dirty[1:]
		}
		return n, nil
	}
	wtShipDiffRun = func(string, bool, int) (string, bool, error) {
		return "diff --git a/x.go b/x.go\n+package x\n", false, nil
	}
	wtShipCommitRun = func(_, msg string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.commits = append(f.commits, msg)
		return nil
	}
	wtRemoveRun = func(_, path string, _ bool) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.removed = append(f.removed, path)
		return nil
	}
	t.Cleanup(func() {
		wtDirtyRun, wtShipDiffRun, wtShipCommitRun, wtRemoveRun = oldDirty, oldDiff, oldCommit, oldRemove
	})
	return f
}

// commitList and removedList copy what the fakes saw so far.
func (f *wtShipFakes) commitList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.commits...)
}

func (f *wtShipFakes) removedList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.removed...)
}

func (f *wtShipFakes) setDirty(d ...int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dirty = d
}

// wtShipModel is the ship model with one user worktree on feat/x.
func wtShipModel(t *testing.T, f *aiShipFake, replies ...ai.Chunk) (*Model, *recordingAIProvider, string) {
	t.Helper()
	m, rec := aiShipModel(t, f, replies...)
	wt := t.TempDir()
	m.wt = newWTState()
	m.wt.entries = []worktrees.Entry{{Worktree: git.Worktree{Path: wt, Branch: "feat/x"}, Kind: worktrees.KindUser}}
	m.wt.cursorPath = wt
	return m, rec, wt
}

// wtShipDrive is aiShipDrive that also gives the WORKTREES view its
// messages (the archive step).
func wtShipDrive(t *testing.T, m *Model, cmd tea.Cmd, done func() bool) {
	t.Helper()
	msgs := make(chan tea.Msg, 64)
	start := func(c tea.Cmd) {
		if c != nil {
			go func() { msgs <- c() }()
		}
	}
	start(cmd)
	deadline := time.After(10 * time.Second)
	for !done() {
		var msg tea.Msg
		select {
		case msg = <-msgs:
		case <-deadline:
			t.Fatal("the ship did not get there in 10s")
		}
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				start(c)
			}
			continue
		}
		if next, ok := m.handleAIShipMsg(msg); ok {
			start(next)
			continue
		}
		if next, ok := m.handleWTMsg(msg); ok {
			start(next)
			continue
		}
		if next, ok := m.handleAIMsg(msg); ok {
			start(next)
		}
	}
}

func (f *aiShipFake) mergeCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.merges { // "N method sha"
		if fs := strings.Fields(c); len(fs) >= 2 {
			out = append(out, "merge "+fs[0]+" "+fs[1])
		}
	}
	return out
}

// Spec (CMP-33): S on a worktree card ships it in one flow. A dirty
// worktree first shows a Cancel-first card with an AI commit message;
// Commit and ship commits everything, then the ship loop pushes (card),
// opens the pull request with an AI title and description (card), waits
// for green CI, asks to merge (Cancel first) and, after the merge, opens
// the archive card of the worktree (Cancel first).
func TestWorktreeShipFullFlow(t *testing.T) {
	base := aiShipProbe{branch: "feat/x", base: "main", subjects: []string{"feat: ship it"}, head: "h1"}
	pushed := base
	pushed.pushed = true
	green := pushed
	green.snap = ghci.Snapshot{HeadSHA: "h1", PR: &ghci.PR{Number: 7, State: "OPEN"},
		Runs: []ghci.Run{{ID: 1, Workflow: "ci", Status: "completed", Conclusion: "success", HeadSHA: "h1"}}}
	f := &aiShipFake{probes: []aiShipProbe{base, pushed, green}}
	m, rec, wt := wtShipModel(t, f, ai.Chunk{Delta: "feat: ship it"}, ai.Chunk{Delta: "Ship the x feature\n\nWhat: adds x.\nTest: go test ./..."})
	g := wtShipSwap(t, 2, 0)

	if _, ok := m.dispatchWorktreePalette("worktree-ship"); !ok {
		t.Fatal("the palette id is not dispatched")
	}
	card := func(k confirmKindEnum) func() bool { return func() bool { return m.confirmOpen && m.confirmKind == k } }
	it, ok := m.wtItemByPath(wt)
	if !ok {
		t.Fatal("no card for the worktree")
	}
	wtShipDrive(t, m, m.wtShipItem(it), card(confirmKindWTShipCommit))
	if m.confirm.SafeID() != "cancel" {
		t.Fatalf("the commit card is not Cancel first: %q", m.confirm.SafeID())
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"feat: ship it", "2 changed files", "Commit and ship", "Edit message", "merge", "archive"} {
		if !strings.Contains(box, want) {
			t.Errorf("commit card lacks %q:\n%s", want, box)
		}
	}
	wtShipDrive(t, m, m.answerAIRulesForTest("commit"), card(confirmKindAIShipPush))
	if len(g.commitList()) != 1 || g.commitList()[0] != "feat: ship it" {
		t.Fatalf("commits %q", g.commitList())
	}
	if sh := m.aiS().ship; sh == nil || !sh.full || sh.wtPath != wt {
		t.Fatalf("not a full ship of the worktree: %+v", sh)
	}
	wtShipDrive(t, m, m.answerAIRulesForTest("push"), card(confirmKindAIShipPR))
	box = ansi.Strip(m.confirm.Box())
	if !strings.Contains(box, "Ship the x feature") || !strings.Contains(box, "What: adds x.") {
		t.Errorf("the PR card has no AI text:\n%s", box)
	}
	if len(rec.reqs) != 2 || !strings.Contains(aiReqText(rec.reqs[1]), "feat: ship it") {
		t.Fatalf("no AI request for the PR text: %d reqs", len(rec.reqs))
	}
	wtShipDrive(t, m, m.answerAIRulesForTest("create"), card(confirmKindWTShipMerge))
	if len(f.prs) != 1 || f.prs[0].Title != "Ship the x feature" || !strings.Contains(f.prs[0].Body, "Test: go test") {
		t.Fatalf("pr %+v", f.prs)
	}
	if m.confirm.SafeID() != "cancel" {
		t.Fatalf("the merge card is not Cancel first: %q", m.confirm.SafeID())
	}
	box = ansi.Strip(m.confirm.Box())
	for _, want := range []string{"#7", "feat/x", "main", "Squash and merge"} {
		if !strings.Contains(box, want) {
			t.Errorf("merge card lacks %q:\n%s", want, box)
		}
	}
	wtShipDrive(t, m, m.answerAIRulesForTest("merge"), card(confirmKindWTArchive))
	if got := f.mergeCalls(); len(got) != 1 || got[0] != "merge 7 merge" {
		t.Fatalf("merges %v", got)
	}
	if m.aiS().ship != nil {
		t.Fatal("the ship still runs after the merge")
	}
	if m.confirm.SafeID() != "cancel" {
		t.Fatalf("the archive card is not Cancel first: %q", m.confirm.SafeID())
	}
	wtShipDrive(t, m, m.answerAIRulesForTest("archive"), func() bool { return len(g.removedList()) == 1 })
	if g.removedList()[0] != wt {
		t.Fatalf("removed %v", g.removedList())
	}
	want := "probe push probe pr probe merge"
	if got := strings.Join(f.calls, " "); got != want {
		t.Errorf("calls\n got %s\nwant %s", got, want)
	}
}

// Spec (CMP-33): Cancel and Esc on the commit card commit nothing and
// start no ship; Edit message… opens a prompt whose text is committed;
// a clean worktree skips the commit card.
func TestWorktreeShipCommitCard(t *testing.T) {
	f := &aiShipFake{probes: []aiShipProbe{{branch: "feat/x", base: "main"}}}
	m, _, wt := wtShipModel(t, f, ai.Chunk{Delta: "feat: x"}, ai.Chunk{Delta: "feat: x"}, ai.Chunk{Delta: "feat: x"})
	g := wtShipSwap(t, 1)
	it, _ := m.wtItemByPath(wt)
	card := func() bool { return m.confirmOpen && m.confirmKind == confirmKindWTShipCommit }

	wtShipDrive(t, m, m.wtShipItem(it), card)
	if c := m.answerAIRulesForTest("cancel"); c != nil {
		t.Error("Cancel started work")
	}
	wtShipDrive(t, m, m.wtShipItem(it), card)
	m.confirmOpen = false
	if c := m.onAIConfirmClosed(); c != nil {
		t.Error("Esc started work")
	}
	if len(g.commitList()) != 0 || m.aiS().ship != nil {
		t.Fatalf("committed %q, ship %v", g.commitList(), m.aiS().ship != nil)
	}

	wtShipDrive(t, m, m.wtShipItem(it), card)
	m.answerAIRulesForTest("edit")
	if !m.promptOpen || m.promptKind != promptKindWTShipMessage {
		t.Fatal("Edit message… opened no prompt")
	}
	cmd := m.handlePromptSubmit("fix: my own words")
	wtShipDrive(t, m, cmd, func() bool { return len(g.commitList()) == 1 && m.aiS().ship != nil })
	if g.commitList()[0] != "fix: my own words" {
		t.Fatalf("commits %q", g.commitList())
	}
	m.aiShipStop("stop")

	g.setDirty(0)
	cmd = m.wtShipItem(it)
	wtShipDrive(t, m, cmd, func() bool { return m.aiS().ship != nil })
	if len(g.commitList()) != 1 {
		t.Fatal("a clean worktree was committed")
	}
}

// Spec (CMP-33): Cancel on the merge card stops at merge-ready and
// merges nothing; an agent lane, an archived row and a detached
// worktree are refused without a card.
func TestWorktreeShipMergeCancelAndRefusals(t *testing.T) {
	green := aiShipProbe{branch: "feat/x", base: "main", subjects: []string{"a"}, head: "h1", pushed: true}
	green.snap = ghci.Snapshot{HeadSHA: "h1", PR: &ghci.PR{Number: 7, State: "OPEN"},
		Runs: []ghci.Run{{ID: 1, Workflow: "ci", Status: "completed", Conclusion: "success", HeadSHA: "h1"}}}
	f := &aiShipFake{probes: []aiShipProbe{green}}
	m, _, wt := wtShipModel(t, f)
	wtShipSwap(t, 0)
	it, _ := m.wtItemByPath(wt)
	wtShipDrive(t, m, m.wtShipItem(it), func() bool { return m.confirmOpen && m.confirmKind == confirmKindWTShipMerge })
	m.answerAIRulesForTest("cancel")
	if m.aiS().ship != nil || len(f.mergeCalls()) != 0 {
		t.Fatalf("Cancel merged or kept the ship: calls %v", f.calls)
	}

	for name, e := range map[string]worktrees.Entry{
		"agent":    {Worktree: git.Worktree{Path: wt, Branch: "termocode/agent-1"}, Kind: worktrees.KindAgent},
		"archived": {Worktree: git.Worktree{Path: wt, Branch: "feat/x"}, Kind: worktrees.KindUser, Meta: worktrees.Meta{State: worktrees.StateArchived}},
		"detached": {Worktree: git.Worktree{Path: wt, Detached: true}, Kind: worktrees.KindUser},
	} {
		m.confirmOpen = false
		m.wtShipItem(wtItem{e: e})
		if m.confirmOpen || m.aiS().ship != nil {
			t.Errorf("%s: not refused", name)
		}
	}
}
