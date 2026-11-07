package app

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Races on the phone (MOB-61, mobile plan §3.9). The races topic lists
// the agent races of the workspace's repository, newest first: the task
// line, each lane's agent, state, files and lines, verify result and
// cost, the AI's difference summary and the merged lane. It is read
// only; it never sends a lane's folder, a file's text or the verify
// command line. The records are read off the UI goroutine the first
// time a page listens (the same records the compare page uses).

func init() {
	webTopics["races"] = webTopicDef{snapshot: webRacesSnapshot, apply: webApplyReplace[webproto.RacesSnap]}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishRaces)
}

// webRacesDelta is the whole list; a newer one replaces a queued one.
type webRacesDelta webproto.RacesSnap

func (webRacesDelta) coalesce(next any) (any, bool) {
	n, ok := next.(webRacesDelta)
	return n, ok
}

// webRacesEvery: the list is built at most this often.
const webRacesEvery = 500 * time.Millisecond

// webRacesPub is what the topic sent last (Update only).
type webRacesPub struct {
	last    *webproto.RacesSnap
	checked time.Time
	tick    bool
	loading bool   // the records are being read for the page
	none    bool   // the workspace is not a git repository
	err     string // the records could not be read
}

type webRacesTickMsg struct{}

// webRacesLoadedMsg carries the records read for the page.
type webRacesLoadedMsg struct {
	races []agentrace.Race
	dir   string
	none  bool
	err   error
}

// webRacesLoadCmd reads the records of the repository at root.
func webRacesLoadCmd(root string) tea.Cmd {
	return func() tea.Msg {
		dir, err := git.AgentStateDir(root)
		if err != nil {
			return webRacesLoadedMsg{none: true}
		}
		rs, err := agentrace.Load(dir)
		return webRacesLoadedMsg{races: rs, dir: dir, err: err}
	}
}

func webRacesSnapshot(m *Model, _ string) (any, error) {
	snap := m.webRacesBuild()
	if m.web != nil {
		m.web.races.last, m.web.races.checked = &snap, webNow()
	}
	return snap, nil
}

// webPublishRaces reads the records the first time a page listens and
// sends the list when it changed, at most once per webRacesEvery.
func (m *Model) webPublishRaces(msg tea.Msg) tea.Cmd {
	h := m.web
	p := &h.races
	switch msg := msg.(type) {
	case webRacesTickMsg:
		p.tick = false
	case webRacesLoadedMsg:
		p.loading = false
		switch {
		case msg.none:
			p.none = true
		case msg.err != nil:
			p.err = msg.err.Error()
		default:
			p.err = ""
			m.raceAdopt(msg.races, msg.dir)
		}
		p.checked = time.Time{} // send the new state at once
	}
	if !h.webListens("races") {
		p.last = nil
		return nil
	}
	var load tea.Cmd
	if (m.race == nil || m.race.dir == "") && !p.loading && !p.none && p.err == "" {
		p.loading = true
		load = webRacesLoadCmd(aiWorkspaceRoot())
	}
	now := webNow()
	if wait := webRacesEvery - now.Sub(p.checked); p.last != nil && wait > 0 {
		if p.tick {
			return load
		}
		p.tick = true
		return tea.Batch(load, tea.Tick(wait, func(time.Time) tea.Msg { return webRacesTickMsg{} }))
	}
	p.checked = now
	cur := m.webRacesBuild()
	if p.last != nil && reflect.DeepEqual(*p.last, cur) {
		return load
	}
	p.last = &cur
	h.publish("races", webRacesDelta(cur))
	return load
}

// webRacesBuild is the topic's data.
func (m *Model) webRacesBuild() webproto.RacesSnap {
	snap := webproto.RacesSnap{State: "loading", Races: []webproto.RaceItem{}}
	if m.web != nil {
		switch p := &m.web.races; {
		case p.none:
			snap.State = "none"
			return snap
		case p.err != "":
			snap.State = "error"
			return snap
		}
	}
	if m.race == nil || m.race.dir == "" {
		return snap
	}
	snap.State = "ready"
	rs := m.race.races
	for i := len(rs) - 1; i >= 0 && len(snap.Races) < webproto.MaxRaces; i-- {
		snap.Races = append(snap.Races, webRaceItem(&rs[i]))
	}
	return snap
}

// webRaceItem is one race for the page.
func webRaceItem(r *agentrace.Race) webproto.RaceItem {
	it := webproto.RaceItem{ID: webName(r.ID), Number: r.Number, Name: webName(r.Name),
		Prompt: webRaceText(r.Prompt, webproto.MaxRacePrompt), Verify: r.Verify != "", Started: r.StartedMs,
		State: webRaceState(r), Lanes: []webproto.RaceLane{}, SummaryCost: webName(r.SummaryCost), Winner: webName(r.Winner)}
	for _, line := range r.Summary {
		if len(it.Summary) >= webproto.MaxRaceLines {
			break
		}
		it.Summary = append(it.Summary, webRaceText(line, webproto.MaxRaceLine))
	}
	for k := range r.Lanes {
		if len(it.Lanes) >= webproto.MaxRaceLanes {
			break
		}
		it.Lanes = append(it.Lanes, webRaceLane(&r.Lanes[k]))
	}
	return it
}

// webRaceState is a race's one word: working while a lane works or
// waits, closed once a lane is merged or every lane is closed, else done.
func webRaceState(r *agentrace.Race) string {
	if r.Winner != "" {
		return "closed"
	}
	closed := true
	for k := range r.Lanes {
		l := &r.Lanes[k]
		if raceActive(l) {
			return "working"
		}
		if !l.Closed() {
			closed = false
		}
	}
	if closed {
		return "closed"
	}
	return "done"
}

// webRaceLane is one lane for the page; its folder stays on the laptop.
func webRaceLane(l *agentrace.Lane) webproto.RaceLane {
	files, add, del := l.Totals()
	out := webproto.RaceLane{ID: webName(l.ID), Agent: webName(l.Agent), Title: webName(l.Title),
		State: webRaceLaneState(l.State), Branch: webName(l.Branch), Stats: l.Stats,
		Files: files, Add: add, Del: del, Verify: l.Verify.State, Note: webRaceText(l.Note, webproto.MaxRaceLine)}
	if out.Verify == agentrace.VerifyFail {
		out.Exit = l.Verify.Exit
	}
	if l.Cost != nil {
		out.Cost = fmt.Sprintf("$%.2f", *l.Cost)
	}
	for _, f := range l.Files {
		if len(out.Changed) >= webproto.MaxRaceFiles {
			break
		}
		st := f.Status
		if st != "A" && st != "D" {
			st = "M"
		}
		out.Changed = append(out.Changed, webproto.RaceFile{Status: st, Path: webName(f.Path), Add: f.Add, Del: f.Del, Binary: f.Binary})
	}
	return out
}

// webRaceLaneState is a lane's state word; an unknown one is "stopped".
func webRaceLaneState(s agentrace.State) string {
	switch s {
	case agentrace.Starting, agentrace.Working, agentrace.Waiting, agentrace.Done,
		agentrace.Stopped, agentrace.Failed, agentrace.Merged, agentrace.Discarded:
		return string(s)
	}
	return string(agentrace.Stopped)
}

// webRaceText is a line for the page: one line, redacted, at most n
// bytes (cut on a rune with "…").
func webRaceText(s string, n int) string {
	s = ai.RedactSecrets(webName(strings.Join(strings.Fields(s), " ")))
	if len(s) > n {
		s = strings.ToValidUTF8(s[:n], "") + "…"
	}
	return s
}
