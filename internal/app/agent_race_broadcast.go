package app

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// ── Race: message all lanes / run in all lanes (CMP-20) ─────────────────
//
// b on the compare page asks for one message and types it into every
// lane whose agent sits at its input box (a finished lane with a live
// tab), never into a question or a busy agent; a card lists who gets it
// and who is skipped, Cancel first. A lane that got it works again and is
// done when its agent is back at the box (its files are read again).
// c asks for a command and runs it in every open lane's worktree, one
// task tab each, after a card (Cancel first). Nothing is sent or run
// without the card; the AI has no way to start either.

const (
	promptKindRaceBroadcast  promptKindEnum  = 4871
	promptKindRaceRun        promptKindEnum  = 4872
	confirmKindRaceBroadcast confirmKindEnum = 4873
	confirmKindRaceRun       confirmKindEnum = 4874
	raceBroadcastMax                         = 4000 // runes of one message
)

// raceBroadcast is the message or command a card is about.
type raceBroadcast struct {
	race    string
	text    string
	lanes   []string // lane ids that get it
	skipped []string // "Title: why", for the card
}

// raceSpawnRun runs line in lane l's worktree in a task tab (a var for
// tests).
var raceSpawnRun = func(m *Model, l agentrace.Lane, line string) (int, error) {
	return m.spawnTerminalJob(terminalJob{Label: "run · " + l.Title, Cmdline: line, Dir: l.Worktree, Task: "race-run:" + l.ID})
}

// raceOpenBroadcast is b: the message prompt.
func (m *Model) raceOpenBroadcast() tea.Cmd {
	if m.raceS().current() == nil {
		return nil
	}
	m.prompt = prompt.New(" Message all lanes ", "Message for every agent at its input box:", "")
	m.prompt.SetNote("It is typed into each lane's agent, like you would type it. A card shows the lanes first.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindRaceBroadcast
	return nil
}

// raceOpenRunAll is c: the command prompt.
func (m *Model) raceOpenRunAll() tea.Cmd {
	if m.raceS().current() == nil {
		return nil
	}
	m.prompt = prompt.New(" Run in all lanes ", "Command to run in every lane's worktree:", "")
	m.prompt.SetNote("Each lane gets its own task tab. A card shows the lanes first.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindRaceRun
	return nil
}

// raceBroadcastTargets splits r's lanes into the ones whose agent is at
// its input box and the ones skipped (with the reason). Merged and
// discarded lanes are not listed.
func (m *Model) raceBroadcastTargets(r *agentrace.Race) (ready []string, skipped []string) {
	m.raceSyncTabs()
	s := m.raceS()
	for i := range r.Lanes {
		l := &r.Lanes[i]
		if l.Closed() {
			continue
		}
		buf, live := s.bufs[l.ID]
		var tab *terminalTab
		if live {
			tab, _ = m.termTabByBuf(buf)
		}
		why := ""
		switch {
		case tab == nil:
			why = "its tab is closed"
		case l.State == agentrace.Waiting || tab.AgentState == agents.Waiting:
			why = "it asks a question; answer it in its tab"
		case !l.Typed:
			why = "it has not got the task yet"
		case l.State != agentrace.Done || tab.AgentState != agents.Done:
			why = "it still works; send again when it is done"
		}
		if why != "" {
			skipped = append(skipped, l.Title+": "+why)
			continue
		}
		ready = append(ready, l.ID)
	}
	return ready, skipped
}

// onRaceBroadcastText is the message's Enter: the card, or a page line
// when no lane can take it.
func (m *Model) onRaceBroadcastText(text string) tea.Cmd {
	text = strings.TrimSpace(text)
	if r := []rune(text); len(r) > raceBroadcastMax {
		text = string(r[:raceBroadcastMax])
	}
	s := m.raceS()
	r := s.current()
	if text == "" || r == nil {
		return nil
	}
	ready, skipped := m.raceBroadcastTargets(r)
	if len(ready) == 0 {
		m.raceMessage("No agent is at its input box: the message goes to finished lanes only")
		return nil
	}
	s.bcast = raceBroadcast{race: r.ID, text: text, lanes: ready, skipped: skipped}
	var names []string
	for _, id := range ready {
		names = append(names, r.Lane(id).Title)
	}
	body := "Type this into " + strings.Join(names, ", ") + " and press Enter:\n\n  " + aiOneLine(text) +
		"\n\nEach agent works on in its own worktree; your files do not change."
	if len(skipped) > 0 {
		body += "\n\nSkipped:\n  " + strings.Join(skipped, "\n  ")
	}
	what := "Send to 1 lane"
	if len(ready) > 1 {
		what = fmt.Sprintf("Send to %d lanes", len(ready))
	}
	m.confirm = confirm.Risky("Message all lanes · "+r.Name, body, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "send", Title: what, Style: confirm.StylePrimary}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindRaceBroadcast
	return nil
}

// onRaceRunText is the command's Enter: the card.
func (m *Model) onRaceRunText(line string) tea.Cmd {
	line = strings.TrimSpace(line)
	s := m.raceS()
	r := s.current()
	if line == "" || r == nil {
		return nil
	}
	m.raceSyncTabs()
	var ids, names, busy []string
	for i := range r.Lanes {
		l := &r.Lanes[i]
		if l.Closed() || l.Worktree == "" {
			continue
		}
		ids, names = append(ids, l.ID), append(names, l.Title)
		if raceActive(l) {
			busy = append(busy, l.Title)
		}
	}
	if len(ids) == 0 {
		m.raceMessage("No open lane: every lane is merged or discarded")
		return nil
	}
	s.bcast = raceBroadcast{race: r.ID, text: line, lanes: ids}
	body := "Run this in the worktree of " + strings.Join(names, ", ") + ", one task tab each:\n\n  " + aiOneLine(line) +
		"\n\nIt runs in the lanes' worktrees, not in your folder."
	if len(busy) > 0 {
		body += "\n\n" + strings.Join(busy, ", ") + " still works; the command runs next to its agent."
	}
	what := "Run in 1 lane"
	if len(ids) > 1 {
		what = fmt.Sprintf("Run in %d lanes", len(ids))
	}
	m.confirm = confirm.Risky("Run in all lanes · "+r.Name, body, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "run", Title: what, Style: confirm.StylePrimary}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindRaceRun
	return nil
}

// answerRaceBroadcastCard runs the card's answer ("cancel" or "": nothing).
func (m *Model) answerRaceBroadcastCard(kind confirmKindEnum, id string) tea.Cmd {
	s := m.raceS()
	b := s.bcast
	s.bcast = raceBroadcast{}
	r := s.find(b.race)
	if r == nil {
		return nil
	}
	switch {
	case kind == confirmKindRaceBroadcast && id == "send":
		return m.raceSendAll(r, b)
	case kind == confirmKindRaceRun && id == "run":
		return m.raceRunAll(r, b)
	}
	return nil
}

// raceSendAll types b.text into the lanes still at their input box (the
// screen may have changed while the card was open).
func (m *Model) raceSendAll(r *agentrace.Race, b raceBroadcast) tea.Cmd {
	s := m.raceS()
	ready, _ := m.raceBroadcastTargets(r)
	ok := map[string]bool{}
	for _, id := range ready {
		ok[id] = true
	}
	var cmds []tea.Cmd
	n := 0
	for _, id := range b.lanes {
		l := r.Lane(id)
		if l == nil || !ok[id] {
			continue
		}
		buf := s.bufs[id]
		raceSendKeys(m, buf, b.text)
		l.State, l.Verify = agentrace.Working, agentrace.Verify{}
		s.worked[id] = false
		if t, _ := m.termTabByBuf(buf); t != nil {
			t.AgentState = agents.Unknown // the old input box reads as a change
		}
		if raceSubmitDelay <= 0 {
			raceSendKeys(m, buf, "\r")
		} else {
			lane := id
			cmds = append(cmds, tea.Tick(raceSubmitDelay, func(time.Time) tea.Msg { return raceSubmitMsg{buf: buf, lane: lane} }))
		}
		n++
	}
	m.raceRefreshPage()
	if n == 0 {
		m.raceMessage("No agent was at its input box any more; nothing was typed")
		return nil
	}
	m.raceMessage(fmt.Sprintf("Sent to %d lane%s", n, plural(n)))
	return tea.Batch(append(cmds, m.raceSave())...)
}

// raceRunAll runs b.text in each lane's worktree.
func (m *Model) raceRunAll(r *agentrace.Race, b raceBroadcast) tea.Cmd {
	var cmds []tea.Cmd
	n := 0
	for _, id := range b.lanes {
		l := r.Lane(id)
		if l == nil || l.Closed() || l.Worktree == "" {
			continue
		}
		if _, err := raceSpawnRun(m, *l, b.text); err != nil {
			cmds = append(cmds, m.toastErr("Could not run the command in "+l.Title, err))
			continue
		}
		n++
	}
	m.raceMessage(fmt.Sprintf("Running in %d lane%s: %s", n, plural(n), aiOneLine(b.text)))
	return tea.Batch(cmds...)
}
