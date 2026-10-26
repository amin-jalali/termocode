package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
)

// Runs (runs_*.go): the monitoring page of AI work. These tests use the
// demo data (runs_demo.go) for what is drawn, and real pipeline runs on
// scripted models for the verbs (Stop, Pause, Resume).

var runsDemoNow = time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)

// runsRig is a model with the Runs demo data at w×h.
func runsRig(t *testing.T, variant string, w, h int) *Model {
	t.Helper()
	agentsUnicode(t)
	perfColour(t)
	m, _ := aiRunModel(t, &recordingAIProvider{}, &recordingAIProvider{})
	t.Cleanup(clock.Set(func() time.Time { return runsDemoNow }))
	m.w, m.h = w, h
	m.applyLayout()
	m.runsDemo(variant)
	return m
}

func runsSideText(m *Model) string {
	m.runs.cache.sideKey = ""
	return ansi.Strip(m.renderRunsSidebar(m.explorerWidth-1, m.h))
}

func runsPageText(m *Model, w, h int) string {
	m.runs.cache.pageKy = ""
	return ansi.Strip(m.renderRunsPage(w, h))
}

func runsItemByTask(t *testing.T, m *Model, task string) runsItem {
	t.Helper()
	for _, it := range m.runsCollect(clock.Now()) {
		if it.task == task {
			return it
		}
	}
	t.Fatalf("no run %q", task)
	return runsItem{}
}

// Spec: runs are grouped by time: NOW (live, or stopped today with steps
// left), TODAY, older (up to 7 days); each run knows where it is.
func TestRunsCollectGroupsAndKinds(t *testing.T) {
	m := runsRig(t, "list", 200, 50)
	cases := []struct {
		task  string
		kind  runsKind
		group runsGroup
	}{
		{"fix the toast flicker", runsRunning, runsGroupNow},
		{"write release notes for 0.9", runsResumable, runsGroupNow},
		{"why does the toast flicker", runsDone, runsGroupToday},
		{"fix the double quit confirm", runsFailed, runsGroupToday},
		{"tidy the status bar code", runsDone, runsGroupOlder},
	}
	for _, c := range cases {
		it := runsItemByTask(t, m, c.task)
		if it.kind != c.kind || it.group != c.group {
			t.Errorf("%q: kind %d group %d, want %d %d", c.task, it.kind, it.group, c.kind, c.group)
		}
	}
	notes := runsItemByTask(t, m, "write release notes for 0.9")
	if notes.resume != 3 || notes.left != 3 || !strings.Contains(notes.why, "3 of 5 steps left") {
		t.Errorf("resume %d left %d why %q", notes.resume, notes.left, notes.why)
	}
	live := runsItemByTask(t, m, "fix the toast flicker")
	if live.cur != 1 || live.elapsed != 41*time.Second {
		t.Errorf("live cur %d elapsed %v", live.cur, live.elapsed)
	}
	if got := m.ai.runsStepLive(live, 1, clock.Now()); !got.live || got.elapsed != 29*time.Second ||
		!strings.Contains(got.now, "3 tool calls") || !strings.Contains(got.now, "last output 2 s ago") {
		t.Errorf("live step %+v", got)
	}
	if sp := m.ai.runsSpendOf(m.runsCollect(clock.Now())); sp.byModel[0].model != "claude-sonnet-4-5" || sp.sessionRuns != 1 {
		t.Errorf("spend %+v", sp)
	}
}

// Spec (OM-5): a run whose working step sends nothing for too long is
// "stalled" (amber) with the silent time, from the AGENTS activity.
func TestRunsStalledFromActivity(t *testing.T) {
	m := runsRig(t, "stall", 200, 50)
	it := runsItemByTask(t, m, "fix the toast flicker")
	if it.kind != runsStalled || it.stall != 92 || it.why != "no output for 1 m 32 s" {
		t.Errorf("kind %d stall %d why %q", it.kind, it.stall, it.why)
	}
	page := runsPageText(m, 165, 48)
	wantAll(t, page, "⚠ stalled", "no output for 1 m 32 s · Stop (x) or wait", "Stop (x)")
}

// Spec: the list shows two lines per run (task and its word; profile,
// mini pipeline and where it is) under NOW / TODAY / older…, and the
// day's spend on top.
func TestRunsSidebarRows(t *testing.T) {
	m := runsRig(t, "list", 200, 50)
	side := runsSideText(m)
	wantAll(t, side, "RUNS ◉", "today  $0.13", "of $2", "◉ NOW", "◉ fix the toast f", "$0.009",
		"review ●─◉ Fixer · 41 s", "resume", "plan ●─●─ǁ─○─○ 3 left", "TODAY", "✓ why does the to", "failed", "older…", "2 this week")
}

// Spec: the overview has the spend strip, NOW lanes with the chip
// pipeline (and a pill only when needed), TODAY rows and the pills.
func TestRunsOverviewPage(t *testing.T) {
	m := runsRig(t, "list", 200, 50)
	page := runsPageText(m, 165, 48)
	wantAll(t, page, "◉ Runs", "◉ 1 running", "ǁ 1 paused", "SPEND TODAY", "of $2.00 a day · 7 %",
		"■ claude-sonnet-4-5 $0.09", "session $0.04 · 1 run",
		"▐ 2 Fixer ◉ 29 s ▌", "now: edit internal/toast/toast.go · 3 tool calls · last output 2 s ago",
		"▐ Resume (r) ▌", "termocode closed at 11:20 · 3 of 5 steps left",
		"✓ 10:52  why does the toast flicker", "verify still fails", "older…   2 runs this week",
		"Open run (Enter)", "Stop (x)", "!Stop (x) ▌    \n")
}

// Spec: a run's page: pipeline boxes (the live step green with its now
// line), timeline, steps with time, tokens, cost and its share, budget,
// what happened, and the verbs of its state.
func TestRunsDetailPage(t *testing.T) {
	m := runsRig(t, "detail", 200, 50)
	page := runsPageText(m, 165, 48)
	wantAll(t, page, "◉ fix the toast flicker", "review · Review then fix · started 11:59", "◉ running · 41 s · 4.1k tok · $0.009",
		"▲ now · edit internal/toast/toast.go", "TIMELINE", "1 Reviewer · 12 s", "2 Fixer · 29 s", "now 12:00:00",
		"STEPS", "100 %", "main · claude-sonnet-4-5", "read-only", "edit files · waits for 1",
		"BUDGET", "of $2.00 a day", "WHAT HAPPENED", "11:59:31  1 Reviewer done · Found 2 problems",
		"Stop (x)", "Pause (p)", "Open in chat (c)")
	if got := m.runsPageTitle(); got != "◉ fix the toast flicker" {
		t.Errorf("tab title %q", got)
	}
}

// Spec: at 80x24 the list is one line per run and a run's page is one
// line of chips with compact steps.
func TestRunsNarrow(t *testing.T) {
	m := runsRig(t, "list", 80, 24)
	if m.showExp {
		t.Error("a narrow window keeps the sidebar list")
	}
	if !m.runs.pageOn {
		t.Fatal("no page")
	}
	list := runsPageText(m, 75, 22)
	wantAll(t, list, "◉ fix the toast flicker    review  ●─◉  Fixer · 41 s", "▐ Resume (r) ▌", "verify still fails     failed")
	m.runs.detail = "demo-live/runlive"
	page := runsPageText(m, 75, 22)
	wantAll(t, page, "you ──▶ ▐ 1 Reviewer ● ▌ ──▶ ▐ 2 Fixer ◉ ▌ ──▶ answer", "1 Reviewer  ● done", "2 Fixer     ◉ running",
		"this run $0.009 · today $0.13 of $2.00", "⋯ more")
}

// Spec: nothing ran: the page says how a run starts.
func TestRunsEmpty(t *testing.T) {
	m := runsRig(t, "empty", 200, 50)
	page := runsPageText(m, 165, 48)
	wantAll(t, page, "Nothing is running", "The active profile (review) starts a run", "Open the chat (c)",
		"finished runs stay in the list for 7 days", "no runs yet today")
	wantAll(t, runsSideText(m), "nothing is running", "no runs yet")
}

// Spec: ↑↓ pick a run, Enter opens it, Esc goes back; / filters;
// openRuns(profile) keeps one profile's runs.
func TestRunsKeysAndFilter(t *testing.T) {
	m := runsRig(t, "list", 200, 50)
	m.focus = FocusExplorer
	if m.runs.sel != "demo-live/runlive" {
		t.Fatalf("first selection %q", m.runs.sel)
	}
	key := func(k string) {
		var msg tea.KeyMsg
		switch k {
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		if m.focus == FocusExplorer {
			*m, _ = m.handleRunsSidebarKey(msg)
		} else {
			m.handleRunsPageKey(msg)
		}
	}
	key("down")
	if m.runs.sel != "demo-notes/runnotes" {
		t.Fatalf("after down %q", m.runs.sel)
	}
	key("enter")
	if m.runs.detail != "demo-notes/runnotes" || m.focus != FocusEditor {
		t.Fatalf("enter: detail %q focus %v", m.runs.detail, m.focus)
	}
	key("esc")
	if m.runs.detail != "" {
		t.Errorf("esc kept the run page")
	}
	key("/")
	for _, r := range "quit" {
		key(string(r))
	}
	key("enter")
	items := m.runsItems(clock.Now())
	if len(items) != 1 || items[0].task != "fix the double quit confirm" {
		t.Errorf("filter: %d items", len(items))
	}
	m.openRuns(ai.ProfilePlan)
	items = m.runsItems(clock.Now())
	if len(items) != 1 || items[0].run.Profile != ai.ProfilePlan || m.activity.Active() != activity.ViewRuns {
		t.Errorf("openRuns(plan): %d items", len(items))
	}
	wantAll(t, runsSideText(m), "profile plan · Esc")
}

// Spec: a click on a list row picks it and shows its page; a click on a
// lane's pill runs the verb.
func TestRunsClicks(t *testing.T) {
	m := runsRig(t, "list", 200, 50)
	var row hitTarget
	for _, h := range m.runsSidebarTargets(activity.Width, m.explorerWidth-1) {
		if h.ID == "runs:side:run:demo-why/runwhy" {
			row = h
			break
		}
	}
	if row.W == 0 {
		t.Fatal("no row target")
	}
	m.runsClick(strings.TrimPrefix(row.ID, "runs:side:"), false)
	if m.runs.sel != "demo-why/runwhy" || m.runs.detail != "demo-why/runwhy" {
		t.Errorf("click: sel %q detail %q", m.runs.sel, m.runs.detail)
	}
	m.runs.detail = ""
	m.runs.cache.pageKy = ""
	m.renderRunsPage(165, 48)
	found := false
	for _, h := range m.runs.cache.pageHits {
		if h.id == "verb:resume:demo-notes/runnotes" && h.label == "Resume (r)" {
			found = true
		}
	}
	if !found {
		t.Errorf("no Resume pill target: %+v", m.runs.cache.pageHits)
	}
}

// gatedAIProvider answers after gate closes; started gets one value per
// request.
type gatedAIProvider struct {
	gate    chan struct{}
	started chan struct{}
	reply   string
}

func (*gatedAIProvider) Name() string        { return "gated" }
func (*gatedAIProvider) Configured() bool    { return true }
func (*gatedAIProvider) SupportsTools() bool { return true }
func (p *gatedAIProvider) Stream(ctx context.Context, _ ai.Request) <-chan ai.Chunk {
	ch := make(chan ai.Chunk, 2)
	go func() {
		defer close(ch)
		p.started <- struct{}{}
		select {
		case <-p.gate:
		case <-ctx.Done():
			ch <- ai.Chunk{Done: true, Err: ctx.Err()}
			return
		}
		ch <- ai.Chunk{Delta: p.reply}
		ch <- ai.Chunk{Done: true}
	}()
	return ch
}

// runsDrive runs a chat turn's messages; each(m) runs after every one
// and may stop the loop (true).
func runsDrive(t *testing.T, m *Model, cmd tea.Cmd, each func() bool) {
	t.Helper()
	for i := 0; cmd != nil && i < 300; i++ {
		msg := nextAIMsg(t, cmd)
		if msg == nil {
			return
		}
		if _, ok := m.handleAIMsg(msg); !ok {
			t.Fatalf("unrouted %T", msg)
		}
		if _, done := msg.(aiChatDoneMsg); done {
			return
		}
		if each != nil && each() {
			return
		}
		cmd = waitAIChat(m.ai.chatCh)
	}
}

// Spec: Pause (p) lets the working step finish, then the run waits
// (amber, "paused by you", no budget card); Resume (r) runs the rest.
func TestRunsPauseThenResume(t *testing.T) {
	rev := &gatedAIProvider{gate: make(chan struct{}), started: make(chan struct{}, 4), reply: "found it"}
	fix := &recordingAIProvider{replies: []ai.Chunk{{Delta: "fixed"}}}
	m, _ := aiRunModel(t, rev, fix)
	m.w, m.h = 200, 50
	m.revealRuns(true)
	paused := false
	runsDrive(t, m, m.sendAIChat("fix it"), func() bool {
		rm := aiRunMsg(t, m)
		if !paused && rm.Steps[0].State == string(agentdag.StateRunning) {
			<-rev.started
			it, ok := m.runsFind("")
			if !ok || it.kind != runsRunning {
				t.Fatalf("live run %+v", it)
			}
			*m, _ = m.handleRunsSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
			if it, _ := m.runsFind(""); it.kind != runsPausing {
				t.Errorf("after p: kind %d", it.kind)
			}
			paused = true
			close(rev.gate)
		}
		return rm.Run.State == ai.RunStatePaused
	})
	it, _ := m.runsFind("")
	if it.kind != runsPaused || it.why != "paused by you" || m.confirmOpen {
		t.Fatalf("paused: kind %d why %q card %v", it.kind, it.why, m.confirmOpen)
	}
	if len(fix.reqs) != 0 {
		t.Fatal("the fixer ran while paused")
	}
	if got := aiTranscriptText(m, 100); !strings.Contains(got, "Paused by you") {
		t.Errorf("chat card:\n%s", got)
	}
	*m, _ = m.handleRunsSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	runsDrive(t, m, waitAIChat(m.ai.chatCh), nil)
	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateDone || len(fix.reqs) != 1 {
		t.Errorf("after resume: %+v, fixer %d requests", rm.Run, len(fix.reqs))
	}
}

// Spec: Stop (x) asks first with the safe answer ("Keep running") first;
// Keep running changes nothing, Stop stops the run.
func TestRunsStopAsksFirst(t *testing.T) {
	rev := &blockingAIProvider{started: make(chan struct{}, 1), stopped: make(chan struct{})}
	m, _ := aiRunModel(t, rev, &recordingAIProvider{})
	m.w, m.h = 200, 50
	m.revealRuns(true)
	asked := false
	runsDrive(t, m, m.sendAIChat("go"), func() bool {
		if asked || aiRunMsg(t, m).Steps[0].State != string(agentdag.StateRunning) {
			return false
		}
		<-rev.started
		*m, _ = m.handleRunsSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
		if !m.confirmOpen || m.confirmKind != confirmKindRunsStop || m.confirm.SafeID() != "keep" {
			t.Fatalf("stop card: open %v kind %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
		}
		m.handleAIConfirm("keep")
		if !m.ai.streaming {
			t.Fatal("Keep running stopped the run")
		}
		m.handleAIConfirm("stop")
		asked = true
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
}

// Spec (OM-6): Resume (r) on a run that a restart stopped, in another
// chat, opens that chat and runs the steps that are left.
func TestRunsResumeStoredChat(t *testing.T) {
	fix := &recordingAIProvider{replies: []ai.Chunk{{Delta: "fixed now"}}}
	m, _ := aiRunModel(t, &recordingAIProvider{}, fix)
	m.w, m.h = 200, 50
	c := ai.NewConversation()
	c.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "fix the flicker"},
		{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "r7", Profile: ai.ProfileReview, State: ai.RunStateRunning,
			StartedAtMs: clock.Now().Add(-5 * time.Minute).UnixMilli()},
			Steps: []ai.StoredStep{{ID: "reviewer", Title: "Reviewer", State: "completed", Text: "found it"},
				{ID: "fixer", Title: "Fixer", State: "running"}}},
	}
	if err := m.ai.store.Save(&c); err != nil {
		t.Fatal(err)
	}
	m.revealRuns(true)
	it, ok := m.runsFind("")
	if !ok || it.kind != runsResumable || it.group != runsGroupNow || it.resume != 2 {
		t.Fatalf("stored run %+v", it)
	}
	*m, _ = m.handleRunsSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if m.ai.conv.ID != c.ID || !m.ai.streaming {
		t.Fatalf("resume did not open the chat (%q) or start", m.ai.conv.ID)
	}
	runsDrive(t, m, waitAIChat(m.ai.chatCh), nil)
	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateDone || len(fix.reqs) != 1 || !strings.Contains(lastUserContent(fix.reqs[0]), "found it") {
		t.Errorf("after resume %+v (fixer %d)", rm.Run, len(fix.reqs))
	}
}

// Spec: a run's start time is its StartedAtMs, else the time in its id.
func TestRunsStartedFromID(t *testing.T) {
	at := time.Date(2026, 10, 20, 8, 30, 0, 0, time.UTC)
	restore := clock.Set(func() time.Time { return at })
	id := aiNewRunID()
	restore()
	if got := aiRunStarted(ai.StoredRun{ID: id}, time.Time{}); !got.Equal(at) {
		t.Errorf("from id %q: %v", id, got)
	}
	if got := aiRunStarted(ai.StoredRun{ID: "r1", StartedAtMs: at.UnixMilli()}, time.Time{}); !got.Equal(at) {
		t.Errorf("from field: %v", got)
	}
	fb := at.Add(time.Hour)
	if got := aiRunStarted(ai.StoredRun{ID: "r1"}, fb); !got.Equal(fb) {
		t.Errorf("fallback: %v", got)
	}
}

func TestRunsDump(t *testing.T) {
	if os.Getenv("RUNS_DUMP") == "" {
		t.Skip("set RUNS_DUMP=1 to print the screens")
	}
	for _, v := range []string{"list", "detail", "stall", "empty"} {
		m := runsRig(t, v, 200, 50)
		t.Logf("== %s sidebar\n%s", v, runsSideText(m))
		t.Logf("== %s page\n%s", v, runsPageText(m, 165, 48))
	}
}
