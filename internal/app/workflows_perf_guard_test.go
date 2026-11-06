package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/perfbudget"
)

// Perf guards of Workflows (WFX-21, plan §4.2): no workflow on means no
// scheduler at all (idle wakeups unchanged); the workflows topic for 100
// workflows is built fast enough to send after every Update.
func TestWorkflowsNoSchedulerWhenNothingIsOn(t *testing.T) {
	m, ws := wfxRig(t)
	m.wfxBootIn(ws, &wfxClock{now: wfxTestNow})
	t.Cleanup(func() {
		if o := m.wfxS().owner; o != nil {
			_ = o.Release()
		}
	})
	if m.wfxS().sched != nil {
		t.Fatal("a scheduler started although no workflow is on")
	}
}

func TestPerfWorkflowsTopic100(t *testing.T) {
	perfbudget.Require(t)
	m, _ := wfxRig(t)
	for i := range 100 {
		name := fmt.Sprintf("wf-%03d", i)
		if err := os.WriteFile(filepath.Join(wfxPrivateDir(), name+".jsonc"), []byte(fmt.Sprintf(wfxSample, name)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m.wfxLoad("")
	var took []time.Duration
	for range 9 {
		start := time.Now()
		m.webWorkflowsBuild()
		took = append(took, time.Since(start))
	}
	perfbudget.Check(t, "workflows_topic_100", medianMs(took))
}
