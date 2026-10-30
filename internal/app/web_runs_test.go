package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestRunsClock sets webNow and the app clock to the Runs demo's
// time; moving it moves both.
func webTestRunsClock(t *testing.T) *time.Time {
	t.Helper()
	now := runsDemoNow
	old := webNow
	webNow = func() time.Time { return now }
	t.Cleanup(func() { webNow = old })
	t.Cleanup(clock.Set(func() time.Time { return now }))
	return &now
}

func webTestRuns(t *testing.T, frames []map[string]any) (webproto.RunsSnap, int) {
	t.Helper()
	var s webproto.RunsSnap
	n := 0
	for _, f := range frames {
		if f["topic"] == "runs" {
			s = webproto.RunsSnap{}
			webTestData(t, f, &s)
			n++
		}
	}
	return s, n
}

func webTestRunByTitle(t *testing.T, s webproto.RunsSnap, title string) webproto.RunItem {
	t.Helper()
	for _, r := range s.Runs {
		if r.Title == title {
			return r
		}
	}
	t.Fatalf("no run %q in %+v", title, s.Runs)
	return webproto.RunItem{}
}

// Spec (WEB-25): the runs topic is the Runs view's list: each run's state,
// step, time and what the web may do with it; a live run's clock alone
// sends nothing; a state change sends the list. runs.stop on a run that is
// not going is a conflict, an unknown key not found; a stopped run of
// another chat resumes on the laptop only.
func TestRunsSnapshotAndStop(t *testing.T) {
	m := runsRig(t, "list", 200, 50)
	now := webTestRunsClock(t)
	webTestHub(t, m)
	c := webTestSub(t, m, "runs")
	s, _ := webTestRuns(t, webTestNow(t, c))
	live := webTestRunByTitle(t, s, "fix the toast flicker")
	if live.State != "running" || !live.Live || live.Step != 2 || live.Steps < 2 || live.ElapsedMs != 41000 ||
		strings.Join(live.Can, ",") != "stop,pause" {
		t.Fatalf("live run %+v", live)
	}
	notes := webTestRunByTitle(t, s, "write release notes for 0.9")
	if notes.State != "resumable" || !notes.NeedsYou || len(notes.Can) != 0 {
		t.Errorf("resumable run of another chat %+v", notes)
	}
	if done := webTestRunByTitle(t, s, "why does the toast flicker"); done.State != "done" || len(done.Can) != 0 {
		t.Errorf("done run %+v", done)
	}

	*now = now.Add(5 * time.Second)
	m.webAfterUpdate(nil)
	if _, n := webTestRuns(t, webTestNow(t, c)); n != 0 {
		t.Fatalf("%d frames for the clock alone", n)
	}

	doneKey := webTestRunByTitle(t, s, "why does the toast flicker").Key
	for _, tc := range []struct{ op, args, code string }{
		{webproto.OpRunsStop, `{"key":"` + doneKey + `"}`, "conflict"},
		{webproto.OpRunsPause, `{"key":"` + doneKey + `"}`, "conflict"},
		{webproto.OpRunsResume, `{"key":"` + notes.Key + `"}`, "conflict"},
		{webproto.OpRunsStop, `{"key":"no/such"}`, "not_found"},
	} {
		if f, _ := webTestCall(t, m, c, tc.op, tc.args); webTestErr(f) != tc.code {
			t.Errorf("%s %s: %v, want %s", tc.op, tc.args, f, tc.code)
		}
	}

	// The live run stops (its state changes): the list goes out.
	m.ai.streaming = false
	m.ai.conv.Messages[1].Run.State = ai.RunStateCancelled
	*now = now.Add(2 * time.Second)
	m.webAfterUpdate(nil)
	s, n := webTestRuns(t, webTestNow(t, c))
	if n != 1 || webTestRunByTitle(t, s, "fix the toast flicker").Live {
		t.Fatalf("after the stop: %d frames %+v", n, s.Runs)
	}
}

// Spec (WEB-25): runs.stop stops a live run the same way the laptop's
// Stop does (the page asked first): the working step ends.
func TestRunsStopFromWeb(t *testing.T) {
	rev := &blockingAIProvider{started: make(chan struct{}, 1), stopped: make(chan struct{})}
	m, _ := aiRunModel(t, rev, &recordingAIProvider{})
	m.output = newOutputStore()
	webTestHub(t, m)
	c := webTestSub(t, m)
	stopped := false
	runsDrive(t, m, m.sendAIChat("go"), func() bool {
		if stopped || aiRunMsg(t, m).Steps[0].State != string(agentdag.StateRunning) {
			return false
		}
		<-rev.started
		it, ok := m.runsFind("")
		if !ok {
			t.Fatal("no live run")
		}
		f, cmd := webTestCall(t, m, c, webproto.OpRunsStop, `{"key":"`+it.key+`"}`)
		if f["t"] != "res" {
			t.Fatalf("stop: %v", f)
		}
		_ = cmd
		stopped = true
		return false
	})
	select {
	case <-rev.stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the step was not stopped")
	}
	if rm := aiRunMsg(t, m); rm.Run.State != ai.RunStateCancelled {
		t.Errorf("run %+v", rm.Run)
	}
	if !strings.Contains(webTestAudit(m), "runs.stop") {
		t.Errorf("audit:\n%s", webTestAudit(m))
	}
}

// Spec (WEB-25): the runs replay golden: a live run that stops replays to
// the final snapshot.
func TestRunsReplayGolden(t *testing.T) {
	golden, err := filepath.Abs(webReplayPath("runs", "live_stops")) // the rig moves into a temp folder
	if err != nil {
		t.Fatal(err)
	}
	m := runsRig(t, "list", 200, 50)
	now := webTestRunsClock(t)
	h := webTestHub(t, m)
	c := webReplayRecord(t, m, h, "runs", func() {
		*now = now.Add(2 * time.Second)
		m.ai.streaming = false
		m.ai.conv.Messages[1].Run.State = ai.RunStateCancelled
		m.webAfterUpdate(nil)
	})
	if len(c.Deltas) != 1 {
		t.Fatalf("deltas %d", len(c.Deltas))
	}
	webReplayCheck(t, h.topics["runs"], c)
	webReplayGolden(t, golden, c)
}
