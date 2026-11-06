package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// Spec (WFX-20, plan §6.9): the admin policy is checked when a run starts
// and when schedules are synced: a denied write target refuses the run
// (a dry run still shows it), "schedule": true keeps every schedule off.
func TestWorkflowsAdminPolicy(t *testing.T) {
	m, _ := wfxRig(t)
	pol := filepath.Join(t.TempDir(), "workflows-policy.json")
	prev := workflow.SystemPolicyPath
	workflow.SystemPolicyPath = pol
	t.Cleanup(func() { workflow.SystemPolicyPath = prev })
	if err := os.WriteFile(pol, []byte(`{"deny": [{"write": "file"}, {"schedule": true}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	e, _ := wire.Find(m.wfxS().list, "mine")
	if _, _, _, err := m.wfxStartID(e, false); err == nil || !strings.Contains(err.Error(), "writes to file are denied by policy") {
		t.Fatalf("a denied run started: %v", err)
	}
	id, h, _, err := m.wfxStartID(e, true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	h.Wait()
	delete(m.wfxS().live, id)

	wfxAddScheduled(t, m, "tick", "", false)
	m.wfxSchedBoot(&wfxClock{now: wfxTestNow})
	t.Cleanup(m.wfxSchedStop)
	if _, ok := m.wfxNextRun("tick"); ok {
		t.Fatal("a schedule ran under a policy that denies schedules")
	}
	if err := os.WriteFile(pol, []byte(`{"deny": [`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := m.wfxStartID(e, false); err == nil || !strings.Contains(err.Error(), "blocks every workflow") {
		t.Fatalf("a broken policy must block: %v", err)
	}
}
