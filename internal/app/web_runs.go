package app

import (
	"reflect"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The runs topic and its requests (WEB-25, web design 2.8): the AI runs
// of the Runs view (the open chat's, plus the stored chats' once the
// laptop's Runs view has read them), each with what the web may do now.
// A live run's time grows on the page (live), so the clock alone sends
// nothing; the list is built at most once per webRunsEvery, and only
// after an Update. runs.stop, runs.pause and runs.resume take the same
// paths as the laptop's Runs view (the page asked first); a stopped run
// of another chat resumes on the laptop only (it would switch the
// laptop's chat).

const webRunsEvery = time.Second

func init() {
	webTopics["runs"] = webTopicDef{snapshot: webRunsSnapshot, apply: webApplyReplace[webproto.RunsSnap]}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishRuns)
	webOps[webproto.OpRunsStop] = (*Model).webRunsStop
	webOps[webproto.OpRunsPause] = (*Model).webRunsPause
	webOps[webproto.OpRunsResume] = (*Model).webRunsResume
}

// webRunsDelta is a runs delta: the whole list; a newer one replaces a
// queued one.
type webRunsDelta webproto.RunsSnap

func (webRunsDelta) coalesce(next any) (any, bool) {
	n, ok := next.(webRunsDelta)
	return n, ok
}

// webRunsPub is what the runs topic sent last (Update only).
type webRunsPub struct {
	last    *webproto.RunsSnap
	checked time.Time // when the list was last built
	tick    bool      // a one-shot tick is on its way
}

// webRunsTickMsg: the wait is over; build the list again.
type webRunsTickMsg struct{}

func webRunsSnapshot(m *Model, _ string) (any, error) {
	snap := m.webRunsBuild(clock.Now())
	if m.web != nil {
		m.web.runs = webRunsPub{last: &snap, checked: webNow()}
	}
	return snap, nil
}

// webPublishRuns builds the list at most once per webRunsEvery after an
// Update and sends it when more than a live run's clock moved.
func (m *Model) webPublishRuns(msg tea.Msg) tea.Cmd {
	h := m.web
	p := &h.runs
	if _, ok := msg.(webRunsTickMsg); ok {
		p.tick = false
	}
	if !h.webListens("runs") {
		p.last = nil
		return nil
	}
	now := webNow()
	if wait := webRunsEvery - now.Sub(p.checked); p.last != nil && wait > 0 {
		if p.tick {
			return nil
		}
		p.tick = true
		return tea.Tick(wait, func(time.Time) tea.Msg { return webRunsTickMsg{} })
	}
	p.checked = now
	cur := m.webRunsBuild(clock.Now())
	if p.last != nil && webRunsSame(*p.last, cur) {
		return nil
	}
	p.last = &cur
	h.publish("runs", webRunsDelta(cur))
	return nil
}

// webRunsSame compares two lists, leaving out the clock of live runs.
func webRunsSame(a, b webproto.RunsSnap) bool {
	clock := func(s webproto.RunsSnap) webproto.RunsSnap {
		out := webproto.RunsSnap{Runs: make([]webproto.RunItem, len(s.Runs))}
		for i, r := range s.Runs {
			if r.Live {
				r.ElapsedMs = 0
			}
			out.Runs[i] = r
		}
		return out
	}
	return reflect.DeepEqual(clock(a), clock(b))
}

// webRunsState names a run's kind on the wire.
var webRunsState = map[runsKind]string{
	runsRunning: "running", runsStalled: "stalled", runsPausing: "pausing", runsPaused: "paused",
	runsResumable: "resumable", runsDone: "done", runsFailed: "failed", runsCancelled: "cancelled",
}

// webRunsBuild lists the runs, newest first.
func (m *Model) webRunsBuild(now time.Time) webproto.RunsSnap {
	s := webproto.RunsSnap{Runs: []webproto.RunItem{}}
	for i, it := range m.runsCollect(now) {
		if i == webproto.MaxRuns {
			break
		}
		r := webproto.RunItem{Key: it.key, Title: webName(it.task), Chat: webName(it.chatTitle), Profile: it.run.Profile,
			State: webRunsState[it.kind], Why: webName(it.why), Steps: len(it.steps),
			Started: it.started.UnixMilli(), ElapsedMs: it.elapsed.Milliseconds(),
			Live:    it.kind == runsRunning || it.kind == runsStalled || it.kind == runsPausing,
			CostUSD: it.cost, NeedsYou: it.kind.needsYou(), Can: m.webRunsCan(it)}
		if it.cur >= 0 && it.cur < len(it.steps) {
			r.Step = it.cur + 1
			r.StepTitle = webName(it.steps[it.cur].Title)
		}
		s.Runs = append(s.Runs, r)
	}
	return s
}

// webRunsCan is what the web may do with a run now.
func (m *Model) webRunsCan(it runsItem) []string {
	var can []string
	if it.kind.live() {
		can = append(can, "stop")
	}
	if it.kind == runsRunning || it.kind == runsStalled {
		can = append(can, "pause")
	}
	if m.webRunsResumable(it) {
		can = append(can, "resume")
	}
	return can
}

// webRunsResumable: a paused live run, or a stopped run of the open chat
// while the AI is free.
func (m *Model) webRunsResumable(it runsItem) bool {
	switch {
	case it.kind == runsPaused || it.kind == runsPausing:
		return true
	case it.kind == runsResumable:
		return it.open && it.resume > 0 && !m.aiS().streaming
	}
	return false
}

// webRunsFind is the run with key, or a failed call.
func (m *Model) webRunsFind(call *webCall) (runsItem, bool) {
	key := call.args.(*webproto.KeyArgs).Key
	for _, it := range m.runsCollect(clock.Now()) {
		if it.key == key {
			return it, true
		}
	}
	call.fail(webproto.CodeNotFound, "That run is gone.")
	return runsItem{}, false
}

func (m *Model) webRunsStop(call *webCall) tea.Cmd {
	it, ok := m.webRunsFind(call)
	if !ok {
		return nil
	}
	if !it.kind.live() {
		call.fail(webproto.CodeConflict, "This run is not going.")
		return nil
	}
	cmd := m.answerRunsStop("stop")
	m.webAuditOp(call, it.key, "ok")
	call.reply(nil)
	return cmd
}

func (m *Model) webRunsPause(call *webCall) tea.Cmd {
	it, ok := m.webRunsFind(call)
	if !ok {
		return nil
	}
	if it.kind != runsRunning && it.kind != runsStalled {
		call.fail(webproto.CodeConflict, "Only a run that is going can pause.")
		return nil
	}
	cmd := m.runsPause(it)
	m.webAuditOp(call, it.key, "ok")
	call.reply(nil)
	return cmd
}

func (m *Model) webRunsResume(call *webCall) tea.Cmd {
	it, ok := m.webRunsFind(call)
	if !ok {
		return nil
	}
	if !m.webRunsResumable(it) {
		call.fail(webproto.CodeConflict, "Resume this run on the laptop.")
		return nil
	}
	cmd := m.runsResume(it)
	m.webAuditOp(call, it.key, "ok")
	call.reply(nil)
	return cmd
}
