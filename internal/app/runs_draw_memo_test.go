package app

import (
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Inside one frame the runs are collected once; the next frame (or work
// outside a frame) sees changes made in between.
func TestRunsCollectFrameMemo(t *testing.T) {
	m := perfRunsStore(t)
	m.revealRuns(false)
	st := m.runs
	now := runsNow()
	end := st.runsBeginDraw(now)
	a, b := m.runsCollect(now), m.runsCollect(now)
	if len(a) != 200 || &a[0] != &b[0] {
		t.Fatalf("not kept inside the frame: %d runs", len(a))
	}
	if c := m.runsCollect(now.Add(time.Second)); len(c) > 0 && &c[0] == &a[0] {
		t.Error("another time used the frame's runs")
	}
	end()
	m.ai.conv.Messages = append(m.ai.conv.Messages,
		ai.StoredMessage{Role: ai.StoredUser, Text: "a new run"},
		ai.StoredMessage{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "fresh", State: ai.RunStateDone, StartedAtMs: now.UnixMilli()}})
	if n := len(m.runsCollect(now)); n != 201 {
		t.Errorf("after the frame: %d runs, want 201", n)
	}
	defer st.runsBeginDraw(now)()
	if n := len(m.runsCollect(now)); n != 201 {
		t.Errorf("next frame: %d runs, want 201", n)
	}
}
