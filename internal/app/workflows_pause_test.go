package app

import (
	"strings"
	"testing"
)

// The run page's p pauses the selected workflow's live run (no new step
// starts; the page says so) and p again lets it go on. No card: it only
// waits, nothing is lost.
func TestWorkflowsPauseKey(t *testing.T) {
	m, _ := wfxRig(t)
	st := m.wfxS()
	st.sel = "mine"
	if cmd, ok := m.wfxVerb("p"); !ok || cmd == nil {
		t.Fatalf("p without a live run: ok %v, want a short note", ok)
	}
	m.answerWorkflowCardKind(confirmKindWorkflowRun, "run")
	id := wfxOnlyLive(t, m)
	st.detail = "run:" + id
	paused := false
	wfxPump(t, m, id, func(m *Model) {
		if paused {
			m.answerWorkflowGate("deny")
			m.confirmOpen = false
			return
		}
		paused = true
		m.wfxVerb("p")
		l := m.wfxS().live[id]
		if !l.h.Held() {
			t.Fatal("p did not pause the run")
		}
		page := wfxPage(m, 120, 40)
		if !strings.Contains(page, "PAUSED") || !strings.Contains(page, "[Continue (p)]") {
			t.Errorf("page:\n%s", page)
		}
		m.wfxVerb("p")
		if l.h.Held() {
			t.Fatal("p again did not continue")
		}
		if page := wfxPage(m, 120, 40); strings.Contains(page, "PAUSED") || !strings.Contains(page, "[Pause (p)]") {
			t.Errorf("page after continue:\n%s", page)
		}
		m.answerWorkflowGate("deny")
		m.confirmOpen = false
	})
	found := false
	for _, r := range wfxKeyRowsList() {
		if len(r) == 2 && r[0] == "p" {
			found = true
		}
	}
	if !found {
		t.Error("the key card has no p")
	}
}
