package activity

import "testing"

// Spec (WFX-7, plan §5.8): the Workflows icon sits right after Runs, and
// only while the workspace has workflows (SetWorkflowsShown).
func TestWorkflowsIconAfterRuns(t *testing.T) {
	t.Cleanup(func() { SetWorkflowsShown(false) })
	m := New()
	m.SetHeight(60)
	find := func(v View) (int, bool) {
		for y := 0; y < 60; y++ {
			if got, ok := m.itemAt(y); ok && got == v {
				return y, true
			}
		}
		return 0, false
	}
	if _, ok := find(ViewWorkflows); ok {
		t.Fatal("hidden by default")
	}
	SetWorkflowsShown(true)
	runs, ok1 := find(ViewRuns)
	wf, ok2 := find(ViewWorkflows)
	if !ok1 || !ok2 || wf != runs+itemStride() {
		t.Fatalf("runs row %d (%v), workflows row %d (%v)", runs, ok1, wf, ok2)
	}
	if !WorkflowsShown() {
		t.Fatal("WorkflowsShown")
	}
}
