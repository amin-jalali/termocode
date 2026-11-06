package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/workflow/run"
)

// Notifications (WFX-18, plan §3.2 notify): when a scheduled run ends the
// user hears it on the OS (while the terminal is in the background) and
// every browser gets an attention event; the text is a short summary
// with no step output. A run by hand that the user watches notifies only
// when the file asks ("notify": ["os"]).
func TestWorkflowsNotifyScheduledRun(t *testing.T) {
	m, _ := wfxRig(t)
	var got []string
	prev := deliverOSNotification
	deliverOSNotification = func(body string) { got = append(got, body) }
	t.Cleanup(func() { deliverOSNotification = prev })
	m.termBlurred = true
	webTestHub(t, m)
	c := webTestSub(t, m)
	m.webAfterUpdate(nil)

	wfxAddScheduled(t, m, "tick", "", false)
	m.wfxSchedBoot(&wfxClock{now: wfxTestNow})
	t.Cleanup(m.wfxSchedStop)
	m.wfxS().sched.s.Tick(time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC))
	id, ok := wfxNextDue(t, m, 5*time.Second)
	if !ok {
		t.Fatal("no run")
	}
	var cmds []func()
	rf := wfxPumpCmds(t, m, id, &cmds)
	if rf.State != run.RunOK {
		t.Fatalf("run %s", rf.State)
	}
	for _, f := range cmds {
		f()
	}
	if len(got) != 1 || !strings.Contains(got[0], "tick") || !strings.Contains(got[0], "done") {
		t.Fatalf("OS notification %q", got)
	}
	m.webAfterUpdate(nil)
	found := false
	for _, ev := range webTestEvents(t, c) {
		if ev.Kind == "run_done" && ev.ID == id {
			found = true
		}
	}
	if !found {
		t.Fatal("no run_done attention for the browsers")
	}

	// By hand, without "notify": no OS notification.
	got = nil
	m.wfxS().sel = "mine"
	m.answerWorkflowCardKind(confirmKindWorkflowRun, "run")
	cmds = nil
	wfxPumpCmds(t, m, wfxOnlyLive(t, m), &cmds, func(m *Model) { m.answerWorkflowGate("deny"); m.confirmOpen = false })
	for _, f := range cmds {
		f()
	}
	if len(got) != 0 {
		t.Fatalf("a watched run by hand notified: %q", got)
	}
}

// wfxPumpCmds is wfxPump that also runs the commands of the done message
// (the OS notification is one) through fns.
func wfxPumpCmds(t *testing.T, m *Model, id string, fns *[]func(), answer ...func(*Model)) *run.RunFile {
	t.Helper()
	l := m.wfxS().live[id]
	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg := <-l.ch:
			cmd, _ := m.handleWfxMsg(msg)
			if _, ok := msg.(wfxGateMsg); ok && len(answer) > 0 {
				answer[0](m)
			}
			if _, ok := msg.(wfxDoneMsg); ok {
				*fns = append(*fns, func() { wfxRunCmd(cmd) })
				rf, _ := m.wfxRunByID(id)
				return rf
			}
		case <-deadline:
			t.Fatal("the run did not end")
		}
	}
}

// wfxRunCmd runs a command and the batches inside it, but not the
// listeners that would block.
func wfxRunCmd(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				wfxRunCmd(c)
			}
		}
	case <-time.After(200 * time.Millisecond):
	}
}
