package app

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/schedule"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// The scheduler in the app (WFX-15, plan §4.2): enabled, trusted
// workflows with a schedule fire on the fake clock; the state file gets
// last_run and next_run; a precheck that fails skips the slot;
// workflows_parallel_max caps runs across workflows.

// wfxClock is a fake schedule.Clock: Now is set by the test, After never
// fires (the tests call Tick).
type wfxClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *wfxClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *wfxClock) After(time.Duration) <-chan time.Time { return make(chan time.Time) }

const wfxScheduled = `{
  "version": 1, "name": "%NAME%", "title": "Hourly docs",
  "schedule": { "preset": "hourly", "time": ":00", "tz": "UTC"%PRE% },
  "steps": [
    { "id": "docs", "type": "source", "from": "files", "glob": "docs/**/*.md" }%WRITE%
  ]
}`

// wfxAddScheduled writes a private scheduled workflow and turns it on.
func wfxAddScheduled(t *testing.T, m *Model, name, pre string, write bool) {
	t.Helper()
	body := strings.NewReplacer("%NAME%", name, "%PRE%", pre, "%WRITE%", map[bool]string{true: `,
    { "id": "rep", "type": "write", "to": "file", "path": "reports/` + name + `.md", "content": "x" }`}[write]).Replace(wfxScheduled)
	dir := wfxPrivateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".jsonc"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m.wfxLoad("")
	e, ok := wire.Find(m.wfxS().list, name)
	if !ok || e.Broken() {
		t.Fatalf("%s: %+v", name, e.Problems)
	}
	if err := ledger.SaveState(m.wfxS().root, name, ledger.WorkflowState{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	m.wfxLoad("")
}

// wfxNextDue handles the scheduler's messages until a run starts and
// returns its id.
func wfxNextDue(t *testing.T, m *Model, wait time.Duration) (string, bool) {
	t.Helper()
	h := m.wfxS().sched
	deadline := time.After(wait)
	for {
		select {
		case msg := <-h.ch:
			before := map[string]bool{}
			for id := range m.wfxS().live {
				before[id] = true
			}
			if _, ok := m.handleWfxMsg(msg); !ok {
				t.Fatalf("not a workflow message: %T", msg)
			}
			for id := range m.wfxS().live {
				if !before[id] {
					return id, true
				}
			}
		case <-deadline:
			return "", false
		}
	}
}

// wfxWaitState polls the state file until ok says yes.
func wfxWaitState(t *testing.T, m *Model, name string, ok func(ledger.WorkflowState) bool) ledger.WorkflowState {
	t.Helper()
	e, _ := wire.Find(m.wfxS().list, name)
	deadline := time.Now().Add(5 * time.Second)
	for {
		s, err := ledger.LoadState(m.wfxS().root, name, e.Hash)
		if err == nil && ok(s) {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s state: %+v %v", name, s, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWorkflowsScheduledRunFires(t *testing.T) {
	m, _ := wfxRig(t)
	wfxAddScheduled(t, m, "tick", "", false)
	c := &wfxClock{now: wfxTestNow} // 09:41
	m.wfxSchedBoot(c)
	t.Cleanup(m.wfxSchedStop)
	next, ok := m.wfxS().sched.s.NextRun("tick")
	if want := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC); !ok || !next.Equal(want) {
		t.Fatalf("next run %v %v, want %v", next, ok, want)
	}
	m.wfxShowPage("tick", false)
	if page := wfxPage(m, 120, 30); !strings.Contains(page, "next 10:00") {
		t.Fatalf("the page shows the next run:\n%s", page)
	}
	c.mu.Lock()
	c.now = next
	c.mu.Unlock()
	m.wfxS().sched.s.Tick(next)
	id, ok := wfxNextDue(t, m, 5*time.Second)
	if !ok {
		t.Fatal("the due slot started no run")
	}
	if rf := wfxPump(t, m, id, nil); rf.State != run.RunOK {
		t.Fatalf("scheduled run: %s %s", rf.State, rf.Error)
	}
	s := wfxWaitState(t, m, "tick", func(s ledger.WorkflowState) bool { return s.LastRunMs == next.UnixMilli() })
	if s.NextRunMs != next.Add(time.Hour).UnixMilli() || s.LastOutcome != string(schedule.Ran) || !s.Enabled {
		t.Fatalf("state %+v", s)
	}
}

// Spec (plan §4.2): a precheck with a non-zero exit records the slot as
// skipped (precheck) and starts nothing.
func TestWorkflowsSchedulePrecheckSkips(t *testing.T) {
	m, _ := wfxRig(t)
	wfxAddScheduled(t, m, "gated", `, "precheck": ["false"]`, false)
	c := &wfxClock{now: wfxTestNow}
	m.wfxSchedBoot(c)
	t.Cleanup(m.wfxSchedStop)
	slot := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	m.wfxS().sched.s.Tick(slot)
	s := wfxWaitState(t, m, "gated", func(s ledger.WorkflowState) bool { return s.LastRunMs == slot.UnixMilli() })
	if s.LastOutcome != string(schedule.SkippedPrecheck) {
		t.Fatalf("state %+v", s)
	}
	if _, ok := wfxNextDue(t, m, 200*time.Millisecond); ok || len(m.wfxS().live) != 0 {
		t.Fatal("a failed precheck started a run")
	}
}

// Spec (plan §4.2, §5.8): workflows_parallel_max caps runs across
// workflows: with 1, the second due workflow waits until the first ends.
func TestWorkflowsScheduleParallelMax(t *testing.T) {
	m, _ := wfxRig(t)
	if err := saveHTTPSetting("workflows_parallel_max", 1); err != nil {
		t.Fatal(err)
	}
	wfxAddScheduled(t, m, "one", "", true)
	wfxAddScheduled(t, m, "two", "", true)
	m.wfxSchedBoot(&wfxClock{now: wfxTestNow})
	t.Cleanup(m.wfxSchedStop)
	m.wfxS().sched.s.Tick(time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC))
	first, ok := wfxNextDue(t, m, 5*time.Second)
	if !ok {
		t.Fatal("no run started")
	}
	if _, ok := wfxNextDue(t, m, 300*time.Millisecond); ok {
		t.Fatal("a second run started past workflows_parallel_max = 1")
	}
	wfxPump(t, m, first, func(m *Model) { m.answerWorkflowGate("deny"); m.confirmOpen = false })
	if _, ok := wfxNextDue(t, m, 5*time.Second); !ok {
		t.Fatal("the waiting run did not start after the first ended")
	}
}

// Spec (plan §5.8): turning a schedule on adds its job; off removes it;
// workflows_enabled = false runs no schedule at all.
func TestWorkflowsToggleSyncsScheduler(t *testing.T) {
	m, _ := wfxRig(t)
	wfxAddScheduled(t, m, "tick", "", false)
	m.wfxSchedBoot(&wfxClock{now: wfxTestNow})
	t.Cleanup(m.wfxSchedStop)
	e, _ := wire.Find(m.wfxS().list, "tick")
	m.wfxToggle(e) // off
	if _, ok := m.wfxS().sched.s.NextRun("tick"); ok {
		t.Fatal("off kept the job")
	}
	e, _ = wire.Find(m.wfxS().list, "tick")
	m.wfxToggle(e) // on
	if _, ok := m.wfxS().sched.s.NextRun("tick"); !ok {
		t.Fatal("on did not add the job")
	}
	if err := saveHTTPSetting("workflows_enabled", false); err != nil {
		t.Fatal(err)
	}
	m.wfxSchedSync()
	if _, ok := m.wfxS().sched.s.NextRun("tick"); ok {
		t.Fatal("workflows_enabled = false kept the job")
	}
}
