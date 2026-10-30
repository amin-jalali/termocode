package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestEvents returns the attention events queued on c.
func webTestEvents(t *testing.T, c *webConn) []webproto.Attention {
	t.Helper()
	var out []webproto.Attention
	for _, f := range webTestNow(t, c) {
		if f["t"] != "ev" || f["kind"] != webproto.EventAttention {
			continue
		}
		var a webproto.Attention
		webTestData(t, f, &a)
		out = append(out, a)
	}
	return out
}

// Spec (WEB-12, never-do 14): attention comes from the Model's state
// changes: a run that ends (done / failed), a task terminal that finishes.
// A terminal printing text, a run that stays running and a task that
// keeps running send nothing; every browser gets the event, subscribed
// or not.
func TestAttentionOnlyFromModel(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m) // no topics: events still come
	m.ai.conv.Messages = []ai.StoredMessage{{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "r1", State: ai.RunStateRunning}}}
	m.ai.runMsg = 0
	m.terminalTabs = []terminalTab{{BufID: 7, Task: "build", Running: true}, {BufID: 8, Name: "zsh"}}
	m.webAfterUpdate(nil)
	m.terminalTabs[1].LastCmd = "printf '\\a attention'" // bytes in a shell: never an event
	m.webAfterUpdate(nil)
	if ev := webTestEvents(t, c); len(ev) != 0 {
		t.Fatalf("no change, got %v", ev)
	}
	m.terminalTabs[0].Running = false
	m.ai.conv.Messages[0].Run.State = ai.RunStateDone
	m.webAfterUpdate(nil)
	m.webAfterUpdate(nil) // seen once only
	ev := webTestEvents(t, c)
	want := []webproto.Attention{{Kind: "run_done", ID: "r1"}, {Kind: "task_done", ID: "build"}}
	if len(ev) != 2 || ev[0] != want[0] || ev[1] != want[1] {
		t.Fatalf("events %v, want %v", ev, want)
	}
	m.ai.conv.Messages = append(m.ai.conv.Messages, ai.StoredMessage{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "r2", State: ai.RunStateRunning}})
	m.ai.runMsg = 1
	m.webAfterUpdate(nil)
	m.ai.conv.Messages[1].Run.State = ai.RunStateFailed
	m.webAfterUpdate(nil)
	if ev := webTestEvents(t, c); len(ev) != 1 || ev[0] != (webproto.Attention{Kind: "run_failed", ID: "r2"}) {
		t.Fatalf("failed run: %v", ev)
	}
}
