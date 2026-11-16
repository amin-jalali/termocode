//go:build unix

package app

import (
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/now"
	"github.com/amin-jalali/termocode/internal/nowview"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (Now fixes): the CI item's time is its newest run's last change;
// gh gives no time: the item has none (never an age since 1970), and the
// TUI row shows no age.
func TestNowCITime(t *testing.T) {
	m, c, _ := webTestNowModel(t)
	s, _ := webTestInbox(t, webTestNow(t, c))
	if ci := webTestInboxItem(t, s, "ci:main"); ci.Since != 0 || ci.WhyAt != 0 {
		t.Errorf("ci without a run time: since %d why_at %d", ci.Since, ci.WhyAt)
	}
	for _, r := range m.nowData().Turn {
		if r.ID == "ci:main" && r.Age != "" {
			t.Errorf("TUI CI row age %q without a time", r.Age)
		}
	}
	at := time.Date(2026, 11, 16, 9, 5, 0, 0, time.UTC)
	m.ci.snap.Runs[0].Updated = at.Format(time.RFC3339)
	s = webTestLastInbox(t, m, c)
	if ci := webTestInboxItem(t, s, "ci:main"); ci.Since != at.UnixMilli() || ci.WhyAt != at.UnixMilli() {
		t.Errorf("ci since %d why_at %d, want %d", ci.Since, ci.WhyAt, at.UnixMilli())
	}
}

// Spec (Now fixes): the turns of one AI chat are one row: the newest
// turn's title and time; a turn that waits for the user or runs makes
// the row that state (its key, so Stop stops it). Runs of another chat
// are their own row.
func TestNowRunsOneRowPerChat(t *testing.T) {
	run := func(key, title, state string, started int64, live bool) webproto.RunItem {
		return webproto.RunItem{Key: key, Title: title, Chat: "سلام", State: state, Started: started, Live: live}
	}
	runs := []webproto.RunItem{
		run("c1/r5", "سلتم چطوری؟", "done", 500, false),
		run("c1/r4", "turn 4", "done", 400, false),
		run("c1/r3", "turn 3", "running", 300, true),
		run("c1/r2", "turn 2", "done", 200, false),
		run("c1/r1", "turn 1", "done", 100, false),
		run("c2/r1", "other chat", "failed", 50, false),
	}
	got := nowRunItems(runs, webInboxPaths{})
	if len(got) != 2 {
		t.Fatalf("%d rows, want 2 (one per chat): %+v", len(got), got)
	}
	if c1 := got[0]; c1.Title != "سلتم چطوری؟" || c1.Since != 500 || c1.State != webproto.InboxWorking || c1.Ref != "c1/r3" {
		t.Errorf("chat c1 row %+v", c1)
	}
	if c2 := got[1]; c2.Ref != "c2/r1" || c2.State != webproto.InboxFailed || c2.Turn != now.TurnMe {
		t.Errorf("chat c2 row %+v", c2)
	}
	runs[2].State, runs[2].Live = "done", false
	if got := nowRunItems(runs, webInboxPaths{}); got[0].State != webproto.InboxDone || got[0].Ref != "c1/r5" {
		t.Errorf("all done: %+v", got[0])
	}
}

// Spec (Now fixes): Now lists only what needs the user, runs, or waits
// on others: a finished item that waits on nobody (a done task) is not on
// it, not even under Everything else; a failed or running one stays.
func TestNowLeavesOutFinished(t *testing.T) {
	m, _, _ := webTestNowModel(t)
	webTestTaskRun(m, "lint", time.Now().Add(-3*time.Minute), false, 0)
	ids := map[string]bool{}
	d := m.nowData()
	for _, rs := range [][]nowview.Row{d.Turn, d.Overdue, d.Waiting, d.Other} {
		for _, r := range rs {
			ids[r.ID] = true
		}
	}
	if ids["task:lint"] {
		t.Error("a done task is on Now")
	}
	if !ids["task:test"] || !ids["task:build"] {
		t.Errorf("a failed or running item left Now: %v", ids)
	}
	for _, it := range []webproto.InboxItem{
		{State: webproto.InboxDone, Turn: now.TurnNone},
		{State: webproto.InboxDone},
	} {
		if nowShows(it) {
			t.Errorf("%+v shows", it)
		}
	}
	for _, it := range []webproto.InboxItem{
		{State: webproto.InboxDone, Turn: now.TurnMe},
		{State: webproto.InboxDone, Turn: now.TurnOthers},
		{State: webproto.InboxWorking, Turn: now.TurnNone},
		{State: webproto.InboxFailed, Turn: now.TurnNone},
		{State: webproto.InboxNeedsYou},
	} {
		if !nowShows(it) {
			t.Errorf("%+v does not show", it)
		}
	}
}
