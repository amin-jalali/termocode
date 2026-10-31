package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/ghci"
)

// aiShipFake plays the branch: each Probe returns the next state.
type aiShipFake struct {
	mu     sync.Mutex
	probes []aiShipProbe
	calls  []string
	prs    []ghci.NewPR
}

func (f *aiShipFake) Probe(context.Context, string) (aiShipProbe, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "probe")
	p := f.probes[0]
	if len(f.probes) > 1 {
		f.probes = f.probes[1:]
	}
	return p, nil
}

func (f *aiShipFake) Push(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "push")
	return nil
}

func (f *aiShipFake) CreatePR(_ context.Context, p ghci.NewPR) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "pr")
	f.prs = append(f.prs, p)
	return "https://github.com/o/r/pull/7", nil
}

func (f *aiShipFake) FailedLog(context.Context, ghci.Run) (ghci.FailedLog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "log")
	return ghci.FailedLog{Lines: []string{"--- FAIL: TestX", "x_test.go:9: want 1"}}, nil
}

// aiShipDrive runs cmd and every command it gives (each in its own
// goroutine, so a toast timer never blocks) and routes ship and chat
// messages until done() holds.
func aiShipDrive(t *testing.T, m *Model, cmd tea.Cmd, done func() bool) {
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
			t.Fatal("ship did not get there in 10s")
		}
		switch msg := msg.(type) {
		case tea.BatchMsg:
			for _, c := range msg {
				start(c)
			}
			continue
		case aiChatConfirmMsg:
			m.handleAIMsg(msg)
			m.answerAIToolConfirm("deny")
			m.confirmOpen = false
			start(waitAIChat(m.ai.chatCh))
			continue
		}
		if next, ok := m.handleAIShipMsg(msg); ok {
			start(next)
			continue
		}
		if next, ok := m.handleAIMsg(msg); ok {
			start(next)
		}
	}
}

func aiShipModel(t *testing.T, f *aiShipFake, replies ...ai.Chunk) (*Model, *recordingAIProvider) {
	t.Helper()
	m, rec, _, _ := aiSkillsModel(t, replies...)
	m.gitIsRepo, m.gitBranch.Name, m.gitLoaded = true, "feat/x", true
	old, oldPoll := aiShipOpsFor, aiShipPoll
	aiShipOpsFor, aiShipPoll = func(*Model) aiShipOps { return f }, time.Millisecond
	t.Cleanup(func() { aiShipOpsFor, aiShipPoll = old, oldPoll })
	return m, rec
}

// Spec (OMX-15): the loop pushes (card: Not now first), opens the pull
// request (card: Cancel first), waits for CI, sends a red log to the AI
// as a fix turn, pushes the fix (card again) and stops at merge-ready
// without merging.
func TestAIShipLoop(t *testing.T) {
	base := aiShipProbe{branch: "feat/x", base: "main", subjects: []string{"feat: x"}, head: "h1"}
	unpushed := base
	pushed := base
	pushed.pushed = true
	withPR := pushed
	withPR.snap = ghci.Snapshot{HeadSHA: "h1", PR: &ghci.PR{Number: 7, State: "OPEN"},
		Runs: []ghci.Run{{ID: 1, Workflow: "ci", Status: "completed", Conclusion: "failure", HeadSHA: "h1"}}}
	afterFix := withPR
	afterFix.unpushed, afterFix.head = 1, "h2"
	pending := withPR
	pending.head = "h2" // CI still shows h1: not started for h2
	green := withPR
	green.head = "h2"
	green.snap.HeadSHA = "h2"
	green.snap.Runs = []ghci.Run{{ID: 2, Workflow: "ci", Status: "completed", Conclusion: "success", HeadSHA: "h2"}}
	noPR := pushed
	f := &aiShipFake{probes: []aiShipProbe{unpushed, noPR, withPR, afterFix, pending, green}}
	m, rec := aiShipModel(t, f, ai.Chunk{Delta: "fixed and committed"})

	card := func(k confirmKindEnum) func() bool { return func() bool { return m.confirmOpen && m.confirmKind == k } }
	aiShipDrive(t, m, m.toggleAIShip(), card(confirmKindAIShipPush))
	if m.confirm.SafeID() != "cancel" {
		t.Fatalf("no Not-now-first push card (calls %v)", f.calls)
	}
	aiShipDrive(t, m, m.answerAIRulesForTest("push"), card(confirmKindAIShipPR))
	if m.confirm.SafeID() != "cancel" {
		t.Fatalf("no Cancel-first PR card (calls %v)", f.calls)
	}
	aiShipDrive(t, m, m.answerAIRulesForTest("draft"), card(confirmKindAIShipPush))
	if len(f.prs) != 1 || !f.prs[0].Draft || f.prs[0].Base != "main" || f.prs[0].Title != "feat: x" {
		t.Fatalf("pr %+v", f.prs)
	}
	if len(rec.reqs) != 1 || !strings.Contains(aiReqText(rec.reqs[0]), "--- FAIL: TestX") || !strings.Contains(aiReqText(rec.reqs[0]), "git_commit") {
		t.Fatalf("no fix turn with the log: %d reqs, calls %v", len(rec.reqs), f.calls)
	}
	if m.confirmKind != confirmKindAIShipPush || !m.confirmOpen {
		t.Fatalf("the fix was not offered for push (calls %v)", f.calls)
	}
	aiShipDrive(t, m, m.answerAIRulesForTest("push"), func() bool { return m.aiS().ship == nil })
	if m.aiS().ship != nil {
		t.Fatalf("ship did not stop at merge-ready (calls %v)", f.calls)
	}
	want := "probe push probe pr probe log probe push probe probe"
	if got := strings.Join(f.calls, " "); got != want {
		t.Errorf("calls\n got %s\nwant %s", got, want)
	}
}

// Spec (OMX-15): Not now (or Esc) on a card stops the ship and nothing
// goes out; the palette command again stops a running ship.
func TestAIShipStops(t *testing.T) {
	f := &aiShipFake{probes: []aiShipProbe{{branch: "feat/x", base: "main", subjects: []string{"a"}}}}
	m, _ := aiShipModel(t, f)
	aiShipDrive(t, m, m.toggleAIShip(), func() bool { return m.confirmOpen })
	m.confirmOpen = false
	m.onAIConfirmClosed() // Esc
	if m.aiS().ship != nil || strings.Contains(strings.Join(f.calls, " "), "push") {
		t.Fatalf("Esc did not stop: calls %v", f.calls)
	}
	m.toggleAIShip()
	if m.aiS().ship == nil {
		t.Fatal("not started")
	}
	m.toggleAIShip()
	if m.aiS().ship != nil {
		t.Fatal("second run of the command did not stop")
	}
	f2 := &aiShipFake{probes: []aiShipProbe{{branch: "main", base: "main", subjects: []string{"a"}}}}
	m2, _ := aiShipModel(t, f2)
	aiShipDrive(t, m2, m2.toggleAIShip(), func() bool { return m2.aiS().ship == nil })
	if m2.aiS().ship != nil || m2.confirmOpen {
		t.Error("shipping the default branch")
	}
}
