package app

import (
	"reflect"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workflow/run"
)

// Workflows on the web (WFX-19, plan §5.7, §6.10). The workflows topic
// sends the workflows, their recent runs and a running run's last lines
// (redacted), never a file's text or an item's data. A run's write gate
// is a cards topic card of kind workflow: the phone may Deny (first) or
// Allow once; "Allow for this workflow" stays on the laptop. The phone
// may stop a run (it only ends work; finished items keep their results).

func init() {
	webTopics["workflows"] = webTopicDef{snapshot: webWorkflowsSnapshot, apply: webApplyReplace[webproto.WorkflowsSnap]}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishWorkflows)
	webOps[webproto.OpWorkflowsStop] = (*Model).webWorkflowsStop
}

// webWorkflowsDelta is the whole list; a newer one replaces a queued one.
type webWorkflowsDelta webproto.WorkflowsSnap

func (webWorkflowsDelta) coalesce(next any) (any, bool) {
	n, ok := next.(webWorkflowsDelta)
	return n, ok
}

// webWorkflowsEvery: the list is built at most this often (live tails).
const webWorkflowsEvery = 500 * time.Millisecond

// webWorkflowsPub is what the topic sent last (Update only).
type webWorkflowsPub struct {
	last    *webproto.WorkflowsSnap
	checked time.Time
	tick    bool
}

type webWorkflowsTickMsg struct{}

// webWorkflowCard is the web card of a write gate.
func webWorkflowCard(a *WorkflowWriteAsk, deny, allow webproto.CardChoice) webproto.Card {
	return webproto.Card{Kind: webproto.CardWorkflow, Title: a.Workflow + " wants to write",
		Body: workflowGateBody(*a) + "\n\n\"Allow for this workflow\" is on the laptop only.", Choices: []webproto.CardChoice{deny, allow}}
}

// webAnswerWorkflowGate answers a write gate from the web: allow is Allow
// once, anything else denies.
func (m *Model) webAnswerWorkflowGate(a *WorkflowWriteAsk, choice string) tea.Cmd {
	d := WorkflowGateDeny
	if choice == webproto.ChoiceAllow {
		d = WorkflowGateAllowOnce
	}
	g := m.wfGate
	if g == nil {
		return nil
	}
	if g.cur == a {
		if m.confirmOpen && m.confirmKind == confirmKindWorkflowGate {
			m.confirmOpen = false
		}
		return m.finishWorkflowGate(d)
	}
	for i, q := range g.queue { // not on show yet: answer it in the line
		if q == a {
			g.queue = append(g.queue[:i], g.queue[i+1:]...)
			if a.Answer != nil {
				a.Answer(d)
			}
			break
		}
	}
	return nil
}

func webWorkflowsSnapshot(m *Model, _ string) (any, error) {
	snap := m.webWorkflowsBuild()
	if m.web != nil {
		m.web.wfx = webWorkflowsPub{last: &snap, checked: webNow()}
	}
	return snap, nil
}

// webPublishWorkflows sends the list when it changed, at most once per
// webWorkflowsEvery.
func (m *Model) webPublishWorkflows(msg tea.Msg) tea.Cmd {
	h := m.web
	p := &h.wfx
	if _, ok := msg.(webWorkflowsTickMsg); ok {
		p.tick = false
	}
	if !h.webListens("workflows") {
		p.last = nil
		return nil
	}
	now := webNow()
	if wait := webWorkflowsEvery - now.Sub(p.checked); p.last != nil && wait > 0 {
		if p.tick {
			return nil
		}
		p.tick = true
		return tea.Tick(wait, func(time.Time) tea.Msg { return webWorkflowsTickMsg{} })
	}
	p.checked = now
	cur := m.webWorkflowsBuild()
	if p.last != nil && reflect.DeepEqual(*p.last, cur) {
		return nil
	}
	p.last = &cur
	h.publish("workflows", webWorkflowsDelta(cur))
	return nil
}

// webWorkflowsBuild is the topic's data.
func (m *Model) webWorkflowsBuild() webproto.WorkflowsSnap {
	snap := webproto.WorkflowsSnap{Workflows: []webproto.WorkflowItem{}, Runs: []webproto.WorkflowRun{}}
	if m.wfx == nil {
		ws, _ := m.rootDir()
		if !wfxHasProject(ws) {
			return snap
		}
		m.wfxLoad(ws)
	}
	st := m.wfxS()
	var all []*run.RunFile
	for _, e := range st.list {
		if len(snap.Workflows) >= webproto.MaxWorkflows {
			break
		}
		_, word, _ := m.wfxDot(e)
		it := webproto.WorkflowItem{Name: webName(e.Name), Title: webName(wfxTitle(e)), State: map[string]string{
			"running": "running", "file error": "broken", "not trusted": "untrusted", "on": "on"}[word]}
		if it.State == "" {
			it.State = "off"
		}
		if e.File != nil && e.File.Schedule != nil {
			sc := e.File.Schedule
			it.When = webName(strings.TrimSpace(firstNonEmpty(sc.Cron, sc.Preset+" "+sc.Time) + " " + sc.TZ))
			if next, ok := m.wfxNextRun(e.Name); ok && e.Enabled {
				it.Next = next.UnixMilli()
			}
		}
		if runs := st.runs[e.Name]; len(runs) > 0 {
			it.Last = runs[0].State
		}
		snap.Workflows = append(snap.Workflows, it)
		all = append(all, st.runs[e.Name]...)
	}
	for _, l := range st.live {
		found := false
		for i, rf := range all {
			if rf.ID == l.id {
				all[i], found = l.h.Snapshot(), true
			}
		}
		if !found {
			all = append(all, l.h.Snapshot())
		}
	}
	sortRunsNewest(all)
	for _, rf := range all {
		if len(snap.Runs) >= webproto.MaxWorkflowRuns {
			break
		}
		snap.Runs = append(snap.Runs, m.webWorkflowRun(rf))
	}
	return snap
}

func sortRunsNewest(all []*run.RunFile) {
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && all[j].StartedAt.After(all[j-1].StartedAt); j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
}

// webWorkflowRun is one run for the page.
func (m *Model) webWorkflowRun(rf *run.RunFile) webproto.WorkflowRun {
	r := webproto.WorkflowRun{ID: rf.ID, Workflow: webName(rf.Workflow), State: rf.State, Dry: rf.Dry,
		Started: rf.StartedAt.UnixMilli(), Resumed: rf.Resumed}
	if !rf.EndedAt.IsZero() {
		r.Ended = rf.EndedAt.UnixMilli()
	}
	for _, s := range rf.Steps {
		if s == nil || s.Each == nil {
			continue
		}
		for _, it := range s.Each.Items {
			r.Items++
			if it.State == run.StateFailed {
				r.Failed++
			}
		}
	}
	for _, w := range rf.Writes {
		if w.State == "done" {
			r.Wrote++
		}
	}
	if l := m.wfxS().live[rf.ID]; l != nil {
		r.Waiting = webName(l.waiting)
		for _, ln := range l.tail(webproto.MaxWorkflowTail) {
			if len(ln) > webproto.MaxWorkflowLine {
				ln = strings.ToValidUTF8(ln[:webproto.MaxWorkflowLine], "") + "…"
			}
			r.Tail = append(r.Tail, ai.RedactSecrets(aiCleanText(ln)))
		}
	}
	return r
}

// webWorkflowsStop stops a live run (workflows.stop).
func (m *Model) webWorkflowsStop(call *webCall) tea.Cmd {
	a := call.args.(*webproto.WorkflowRunArgs)
	var l *wfxLive
	if m.wfx != nil {
		l = m.wfx.live[a.Run]
	}
	if l == nil {
		call.fail(webproto.CodeGone, "This run is not running.")
		return nil
	}
	l.h.Cancel()
	device := call.conn.device
	if device == "" {
		device = "web"
	}
	call.reply(nil)
	return m.aiInfo(l.name + ": stopped on " + device)
}
