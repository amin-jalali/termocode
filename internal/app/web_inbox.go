package app

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The inbox topic (MOB-2): the phone Home's "what needs me" list, merged
// from the laptop's own state — the waiting cards (same ids as the cards
// topic), the AI runs (runs topic), the agent lanes (the worktrees list,
// once the laptop read it), the workflow runs, the task runs and the
// branch's CI (once the laptop's CI watch has an answer). The inbox
// never starts a read of its own: no git, no gh, no disk scan beyond what
// the source topics already keep. Newest first, capped; the whole list
// goes out when a source changed, built at most once per webInboxEvery.

const webInboxEvery = 500 * time.Millisecond

func init() {
	webTopics["inbox"] = webTopicDef{snapshot: webInboxSnapshot, apply: webApplyReplace[webproto.InboxSnap]}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishInbox)
}

// webInboxDelta is an inbox delta: the whole list; a newer one replaces a
// queued one.
type webInboxDelta webproto.InboxSnap

func (webInboxDelta) coalesce(next any) (any, bool) {
	n, ok := next.(webInboxDelta)
	return n, ok
}

// webInboxPub is what the inbox topic sent last (Update only).
type webInboxPub struct {
	last    *webproto.InboxSnap
	checked time.Time
	tick    bool
}

// webInboxTickMsg: the wait is over; build the list again.
type webInboxTickMsg struct{}

func webInboxSnapshot(m *Model, _ string) (any, error) {
	snap := m.webInboxBuild()
	if m.web != nil {
		m.web.inbox = webInboxPub{last: &snap, checked: webNow()}
	}
	return snap, nil
}

// webPublishInbox builds the list at most once per webInboxEvery after an
// Update and sends it when it changed.
func (m *Model) webPublishInbox(msg tea.Msg) tea.Cmd {
	h := m.web
	p := &h.inbox
	if _, ok := msg.(webInboxTickMsg); ok {
		p.tick = false
	}
	if !h.webListens("inbox") {
		p.last = nil
		return nil
	}
	now := webNow()
	if wait := webInboxEvery - now.Sub(p.checked); p.last != nil && wait > 0 {
		if p.tick {
			return nil
		}
		p.tick = true
		return tea.Tick(wait, func(time.Time) tea.Msg { return webInboxTickMsg{} })
	}
	p.checked = now
	cur := m.webInboxBuild()
	if p.last != nil && reflect.DeepEqual(*p.last, cur) {
		return nil
	}
	p.last = &cur
	h.publish("inbox", webInboxDelta(cur))
	return nil
}

// webInboxBuild merges the sources, newest first, cut at MaxInbox.
func (m *Model) webInboxBuild() webproto.InboxSnap {
	root, _ := m.rootDir()
	home, _ := os.UserHomeDir()
	items := m.webInboxItems(webInboxPaths{root: root, home: home})
	sort.SliceStable(items, func(i, j int) bool { return items[i].Since > items[j].Since })
	s := webproto.InboxSnap{Items: []webproto.InboxItem{}}
	for _, it := range items {
		s.Counts.Add(it.State)
		if len(s.Items) == webproto.MaxInbox {
			s.More++
			continue
		}
		s.Items = append(s.Items, it)
	}
	return s
}

// webInboxPaths makes a title safe for the phone: the workspace's own
// paths become relative, the home folder ~, other absolute paths keep
// only their last part.
type webInboxPaths struct{ root, home string }

func (p webInboxPaths) title(s string) string {
	s = webName(s)
	if p.root != "" && p.root != "/" {
		s = strings.ReplaceAll(s, p.root+"/", "")
		s = strings.ReplaceAll(s, p.root, ".")
	}
	if p.home != "" && p.home != "/" {
		s = strings.ReplaceAll(s, p.home, "~")
	}
	f := strings.Fields(s)
	changed := false
	for i, w := range f {
		if len(w) > 1 && w[0] == '/' {
			f[i] = "…/" + w[strings.LastIndexByte(w, '/')+1:]
			changed = true
		}
	}
	if changed {
		s = strings.Join(f, " ")
	}
	return webproto.InboxTitle(s)
}

// webInboxItems lists every source's items, unsorted.
func (m *Model) webInboxItems(p webInboxPaths) []webproto.InboxItem {
	var out []webproto.InboxItem
	add := func(it webproto.InboxItem) {
		it.ID = it.Kind + ":" + it.Ref
		it.Title = p.title(it.Title)
		out = append(out, it)
	}
	if m.web != nil {
		for _, r := range m.webCards(&m.web.cards, false) {
			c := r.card
			it := webproto.InboxItem{Kind: webproto.InboxCard, Ref: c.ID, Title: c.Title, Since: c.Since, State: webproto.InboxNeedsYou}
			if !c.ReadOnly {
				it.Can = []string{"answer"}
			}
			add(it)
		}
	}
	if m.ai != nil {
		for _, r := range m.webRunsBuild(clock.Now()).Runs {
			st := webInboxRunState(r)
			if st == "" {
				continue
			}
			add(webproto.InboxItem{Kind: webproto.InboxRun, Ref: r.Key, Title: r.Title, Chat: p.title(r.Chat), Since: r.Started,
				State: st, Can: append([]string(nil), r.Can...)})
		}
	}
	if m.wt != nil && m.wt.loaded {
		for _, w := range m.webWorktreesBuild().Worktrees {
			st := map[string]string{"waiting": webproto.InboxNeedsYou, "working": webproto.InboxWorking, "done": webproto.InboxDone}[w.State]
			if st == "" || (w.Kind != "agent" && w.Agent == "") {
				continue
			}
			title := w.Title
			if w.Agent != "" {
				title = w.Agent + " · " + w.Title
			}
			add(webproto.InboxItem{Kind: webproto.InboxLane, Ref: w.ID, Title: title, Lane: w.ID, Since: w.LastSeen, State: st})
		}
	}
	if m.wfx != nil {
		for _, r := range m.webWorkflowsBuild().Runs {
			it := webproto.InboxItem{Kind: webproto.InboxWorkflow, Ref: r.ID, Title: r.Workflow, Since: r.Started}
			switch r.State {
			case "running":
				it.State, it.Can = webproto.InboxWorking, []string{"stop"}
				if r.Waiting != "" {
					it.State, it.Title = webproto.InboxNeedsYou, r.Workflow+" · "+r.Waiting
				}
			case "paused":
				it.State = webproto.InboxNeedsYou
			case "failed", "interrupted":
				it.State = webproto.InboxFailed
			case "done", "cancelled":
				it.State = webproto.InboxDone
			default:
				continue
			}
			add(it)
		}
	}
	if tr := m.tasks; tr != nil {
		seen := map[string]bool{}
		for i := len(tr.order) - 1; i >= 0; i-- {
			r := tr.runs[tr.order[i]]
			if r == nil || seen[r.Task.Label] {
				continue
			}
			seen[r.Task.Label] = true
			it := webproto.InboxItem{Kind: webproto.InboxTask, Ref: r.Task.Label, Title: r.Task.Label, Since: r.Started.UnixMilli()}
			switch {
			case r.Running:
				it.State, it.Can = webproto.InboxWorking, []string{"stop"}
			case r.Code != 0:
				it.State, it.Can = webproto.InboxFailed, []string{"run"}
			default:
				it.State, it.Can = webproto.InboxDone, []string{"run"}
			}
			add(it)
		}
	}
	if w := m.ci; w != nil && w.loaded && w.err == nil && w.branch != "" {
		st := map[ghci.State]string{ghci.StatePending: webproto.InboxWorking, ghci.StateFail: webproto.InboxFailed,
			ghci.StatePass: webproto.InboxDone}[w.snap.State()]
		if st != "" {
			add(webproto.InboxItem{Kind: webproto.InboxCI, Ref: webName(w.branch), Title: "CI · " + w.branch, State: st})
		}
	}
	return out
}

// webInboxRunState is a run's inbox state ("" leaves it out).
func webInboxRunState(r webproto.RunItem) string {
	switch {
	case r.NeedsYou:
		return webproto.InboxNeedsYou
	case r.Live, r.State == "paused":
		return webproto.InboxWorking
	case r.State == "failed":
		return webproto.InboxFailed
	case r.State == "done", r.State == "cancelled":
		return webproto.InboxDone
	}
	return ""
}
