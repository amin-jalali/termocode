package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── Sleep and wake (WTX-13) ──────────────────────────────────────────────
//
// s in WORKTREES (Worktree: Sleep or Wake Selected) puts a worktree to
// sleep: the agent tabs of this window that run in it end, it is marked
// asleep and moves under ASLEEP. Its files are never touched. A parent
// covers its children: the card offers "Sleep with its N children"
// next to "Sleep only this one". When agent tabs would end or children
// are there, a card asks first (Keep running first and focused; Esc
// keeps them running); otherwise it sleeps at once. Agents of other
// windows keep running (the card says so). s on an asleep worktree
// wakes it and its asleep children.

const (
	confirmKindWTSleep confirmKindEnum = 3604
	wtSleepPaletteID                   = "worktree-sleep"
)

// wtStopTab ends the terminal tab with nvim buffer buf (a var: tests).
var wtStopTab = func(m *Model, buf int) {
	for i, t := range m.terminalTabs {
		if t.BufID == buf {
			m.closeTerminalTab(i)
			return
		}
	}
}

// wtSleepPending is the open sleep card.
type wtSleepPending struct {
	path     string
	title    string
	children []string // their paths
	tabs     []int    // BufIDs of the agent tabs in the worktree
	allTabs  []int    // and in its children too
}

// wtDescendants are the worktrees under path (children, their children
// …), live ones only, in the list's order; a cycle stops the walk.
func wtDescendants(entries []worktrees.Entry, path string) []worktrees.Entry {
	var out []worktrees.Entry
	seen := map[string]bool{path: true}
	frontier := []string{path}
	for len(frontier) > 0 {
		var next []string
		for _, e := range entries {
			if seen[e.Path] || e.Gone || e.Meta.State == worktrees.StateArchived {
				continue
			}
			for _, p := range frontier {
				if e.Meta.Parent == p {
					seen[e.Path] = true
					out = append(out, e)
					next = append(next, e.Path)
					break
				}
			}
		}
		frontier = next
	}
	return out
}

// wtAgentTabsIn are this window's agent tabs whose folder is in one of
// paths: their BufIDs and titles.
func (m *Model) wtAgentTabsIn(paths []string) (bufs []int, names []string) {
	in := map[string]bool{}
	for _, p := range paths {
		in[p] = true
	}
	if m.wt != nil && m.wt.off { // a demo: its canned tabs, never stopped
		for _, t := range m.wt.demoTabs {
			if t.name != "" && in[wtOwner(m.wt.entries, t.cwd)] {
				bufs, names = append(bufs, -1), append(names, t.name)
			}
		}
		return bufs, names
	}
	for _, t := range m.terminalTabs {
		if t.Agent == "" {
			continue
		}
		cwd := t.Cwd
		if t.Worktree != "" {
			cwd = t.Worktree
		}
		if in[wtOwner(m.wt.entries, cwd)] {
			bufs = append(bufs, t.BufID)
			names = append(names, t.agentTitle())
		}
	}
	return bufs, names
}

// wtOtherAgentsIn counts the agents of other windows in paths.
func (st *wtState) wtOtherAgentsIn(paths []string) int {
	in := map[string]bool{}
	for _, p := range paths {
		in[p] = true
	}
	n := 0
	for _, r := range st.others {
		for _, it := range r.Items {
			if in[wtOwner(st.entries, it.Worktree)] {
				n++
			}
		}
	}
	return n
}

// worktreeSleep is s: sleep or wake the cursor's worktree.
func (m *Model) worktreeSleep() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	return m.wtSleepItem(it)
}

// wtSleepItem sleeps or wakes it.
func (m *Model) wtSleepItem(it wtItem) tea.Cmd {
	switch {
	case it.e.Kind == worktrees.KindMain:
		return m.pushToastDetail(toast.Info, "The main checkout stays awake", "Sleep is for the other worktrees.")
	case it.e.Gone:
		return m.pushToastDetail(toast.Info, "Its folder is gone", "F forgets it.")
	case it.e.Meta.State == worktrees.StateAsleep:
		return m.wtWake(it)
	}
	kids := wtDescendants(m.wt.entries, it.e.Path)
	var awake []string
	for _, k := range kids {
		if k.Meta.State != worktrees.StateAsleep {
			awake = append(awake, k.Path)
		}
	}
	tabs, names := m.wtAgentTabsIn([]string{it.e.Path})
	allTabs, allNames := m.wtAgentTabsIn(append([]string{it.e.Path}, awake...))
	if len(allTabs) == 0 && len(awake) == 0 {
		return m.wtSleepDo([]string{it.e.Path}, nil, wtTitle(it))
	}
	p := &wtSleepPending{path: it.e.Path, title: wtTitle(it), children: awake, tabs: tabs, allTabs: allTabs}
	m.wt.sleepAsk = p

	var body strings.Builder
	body.WriteString(wtShortPath(it.e.Path) + " is marked asleep and moves under ASLEEP. Its files are not touched; s wakes it.")
	if len(names) > 0 {
		body.WriteString("\n\nIts agent tabs end: " + strings.Join(names, ", ") + ".")
	}
	if len(awake) > 0 {
		var titles []string
		for _, k := range kids {
			if k.Meta.State != worktrees.StateAsleep {
				titles = append(titles, k.Title())
			}
		}
		body.WriteString("\n\nIt has " + wtChildCount(len(awake)) + ": " + strings.Join(titles, ", ") + ".")
		if extra := allNames[len(names):]; len(extra) > 0 {
			body.WriteString(" Sleeping with " + map[bool]string{true: "it", false: "them"}[len(awake) == 1] + " also ends " + strings.Join(extra, ", ") + ".")
		}
	}
	if n := m.wt.wtOtherAgentsIn(append([]string{it.e.Path}, awake...)); n > 0 {
		body.WriteString("\n\n" + wtPlural(n, "agent") + " in another window keeps running.")
	}
	buttons := []confirm.Button{}
	if len(awake) > 0 {
		buttons = append(buttons, confirm.Button{ID: "one", Title: "Sleep only this one"},
			confirm.Button{ID: "all", Title: "Sleep with its " + wtChildWord(len(awake)), Style: confirm.StylePrimary})
	} else {
		buttons = append(buttons, confirm.Button{ID: "one", Title: "Sleep", Style: confirm.StylePrimary})
	}
	m.confirm = confirm.Risky("Put "+wtTitle(it)+" to sleep?", body.String(), confirm.Button{ID: "keep", Title: "Keep running"}, buttons...)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWTSleep
	return nil
}

// wtChildCount is "1 child" or "N children".
func wtChildCount(n int) string {
	if n == 1 {
		return "1 child"
	}
	return wtChildWord(n)
}

// wtChildWord is "child" or "N children".
func wtChildWord(n int) string {
	if n == 1 {
		return "child"
	}
	return strconv.Itoa(n) + " children"
}

// answerWTSleep answers the card ("" = Esc = Keep running).
func (m *Model) answerWTSleep(id string) tea.Cmd {
	p := m.wt.sleepAsk
	m.wt.sleepAsk = nil
	if p == nil {
		return nil
	}
	switch id {
	case "one":
		return m.wtSleepDo([]string{p.path}, p.tabs, p.title)
	case "all":
		return m.wtSleepDo(append([]string{p.path}, p.children...), p.allTabs, p.title)
	}
	return nil
}

// wtSleepDo ends the tabs and marks paths asleep.
func (m *Model) wtSleepDo(paths []string, tabs []int, title string) tea.Cmd {
	for _, b := range tabs {
		if b >= 0 {
			wtStopTab(m, b)
		}
	}
	now := clock.Now()
	for _, p := range paths {
		if err := m.wtTouchMeta(p, func(md *worktrees.Meta) { md.State, md.LastSeen = worktrees.StateAsleep, now }); err != nil {
			return m.pushToastDetail(toast.Warn, "The worktree could not be put to sleep", err.Error())
		}
	}
	m.wt.cursorPath = paths[0]
	detail := "Its files were not touched; s wakes it."
	if len(paths) > 1 {
		detail = "With its " + wtChildWord(len(paths)-1) + ". " + detail
	}
	if len(tabs) > 0 {
		detail = wtPlural(len(tabs), "agent tab") + " ended. " + detail
	}
	return m.pushToastDetail(toast.Info, title+" sleeps", detail)
}

// wtWake wakes the worktree and its asleep children.
func (m *Model) wtWake(it wtItem) tea.Cmd {
	paths := []string{it.e.Path}
	for _, k := range wtDescendants(m.wt.entries, it.e.Path) {
		if k.Meta.State == worktrees.StateAsleep {
			paths = append(paths, k.Path)
		}
	}
	now := clock.Now()
	for _, p := range paths {
		if err := m.wtTouchMeta(p, func(md *worktrees.Meta) { md.State, md.LastSeen = "", now }); err != nil {
			return m.pushToastDetail(toast.Warn, "The worktree could not be woken", err.Error())
		}
	}
	m.wt.cursorPath = it.e.Path
	detail := "Start an agent in it again when you need one."
	if len(paths) > 1 {
		detail = "With its " + wtChildWord(len(paths)-1) + ". " + detail
	}
	return m.pushToastDetail(toast.Info, "Woke "+wtTitle(it), detail)
}

func init() {
	aiActionPolicies[wtSleepPaletteID] = aiDestructive()
}
