package app

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/agentroll"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── WORKTREES view: the list model ───────────────────────────────────────

// wtKind is the f filter: All / Mine / Agents / Dirty / Asleep / External.
type wtKind int

const (
	wtKindAll wtKind = iota
	wtKindMine
	wtKindAgents
	wtKindDirty
	wtKindAsleep
	wtKindExternal
	wtKindCount
)

func (k wtKind) String() string {
	return [...]string{"All", "Mine", "Agents", "Dirty", "Asleep", "External"}[k]
}

// Agent states on a worktree card, most urgent first.
const (
	wtStateWaiting = "waiting"
	wtStateWorking = "working"
	wtStateDone    = "done"
	wtStateIdle    = "idle"
	wtStateAsleep  = "asleep"
	wtStateGone    = "gone"
)

// Groups of the list, in order.
var wtGroups = []string{"PINNED", "ACTIVE", "ASLEEP", "EXTERNAL"}

// wtAgent is the agent working in a worktree: a terminal tab of this
// window, or one of another window's (agentroll).
type wtAgent struct {
	name  string
	state string // working | waiting | done
	tab   int    // this window's tab index, -1 for another window's
}

// wtItem is one worktree card.
type wtItem struct {
	e      worktrees.Entry
	agent  wtAgent
	agents int // how many agents work in it
	here   bool
	state  string
	group  string
	setup  string // the setup state of a new worktree (worktrees_setup.go)
	depth  int    // drawn under its parent (WTX-11); set when drawn
}

// wtRow is a line of the list model: a group header or a card.
type wtRow struct {
	group string
	item  int // index into items; -1 for a group header
	count int // group headers: the cards in the group
	depth int // cards: how deep under a parent of the same group (0 = a root)
}

// wtTabAgent is the part of a terminal tab the view needs.
type wtTabAgent struct {
	name, cwd string
	state     agents.State
}

// wtOwner is the worktree a folder belongs to: the deepest worktree
// path that holds it ("" when none does).
func wtOwner(entries []worktrees.Entry, dir string) string {
	if dir == "" {
		return ""
	}
	dir = filepath.Clean(dir)
	best := ""
	for _, e := range entries {
		if e.Gone {
			continue
		}
		if (dir == e.Path || strings.HasPrefix(dir, e.Path+string(filepath.Separator))) && len(e.Path) > len(best) {
			best = e.Path
		}
	}
	return best
}

func wtStateRank(s string) int {
	switch s {
	case wtStateWaiting:
		return 0
	case wtStateWorking:
		return 1
	case wtStateDone:
		return 2
	}
	return 3
}

// wtItems joins the entries with the agents in them (this window's tabs
// first, then the other windows'), the "you are here" mark and the
// group. Archived worktrees are left out (W4 shows them).
func wtItems(entries []worktrees.Entry, tabs []wtTabAgent, others []agentroll.Rollup, root string) []wtItem {
	byPath := map[string][]wtAgent{}
	for i, t := range tabs {
		if t.name == "" {
			continue
		}
		state := ""
		switch t.state {
		case agents.Working:
			state = wtStateWorking
		case agents.Waiting:
			state = wtStateWaiting
		case agents.Done:
			state = wtStateDone
		}
		if p := wtOwner(entries, t.cwd); p != "" && state != "" {
			byPath[p] = append(byPath[p], wtAgent{name: t.name, state: state, tab: i})
		}
	}
	for _, r := range others {
		for _, it := range r.Items {
			if p := wtOwner(entries, it.Worktree); p != "" {
				byPath[p] = append(byPath[p], wtAgent{name: it.Name, state: it.State, tab: -1})
			}
		}
	}
	here := wtOwner(entries, root)
	var out []wtItem
	for _, e := range entries {
		if e.Meta.State == worktrees.StateArchived {
			continue
		}
		it := wtItem{e: e, here: e.Path == here, state: wtStateIdle}
		as := byPath[e.Path]
		sort.SliceStable(as, func(i, j int) bool { return wtStateRank(as[i].state) < wtStateRank(as[j].state) })
		if len(as) > 0 {
			it.agent, it.agents, it.state = as[0], len(as), as[0].state
		}
		switch {
		case e.Gone:
			it.state = wtStateGone
		case e.Meta.State == worktrees.StateAsleep && len(as) == 0:
			it.state = wtStateAsleep
		}
		switch {
		case e.Meta.Pinned:
			it.group = "PINNED"
		case e.Meta.State == worktrees.StateAsleep:
			it.group = "ASLEEP"
		case e.Kind == worktrees.KindExternal:
			it.group = "EXTERNAL"
		default:
			it.group = "ACTIVE"
		}
		out = append(out, it)
	}
	// In a group: agents that wait for you, then working ones, then the
	// last seen; git's order (the main checkout first) breaks ties.
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if ra, rb := wtStateRank(a.state), wtStateRank(b.state); ra != rb {
			return ra < rb
		}
		return a.e.Meta.LastSeen.After(b.e.Meta.LastSeen)
	})
	return out
}

// wtMatch: the card passes the f filter and the text filter.
func wtMatch(it wtItem, kind wtKind, filter string, dirty func(string) (int, bool)) bool {
	switch kind {
	case wtKindMine:
		if it.e.Kind != worktrees.KindMain && it.e.Kind != worktrees.KindUser {
			return false
		}
	case wtKindAgents:
		if it.agents == 0 && it.e.Kind != worktrees.KindAgent {
			return false
		}
	case wtKindDirty:
		if n, ok := dirty(it.e.Path); !ok || n == 0 {
			return false
		}
	case wtKindAsleep:
		if it.e.Meta.State != worktrees.StateAsleep {
			return false
		}
	case wtKindExternal:
		if it.e.Kind != worktrees.KindExternal {
			return false
		}
	}
	if filter == "" {
		return true
	}
	f := strings.ToLower(filter)
	for _, s := range []string{it.e.Title(), it.e.Branch, filepath.Base(it.e.Path), it.agent.name} {
		if s != "" && strings.Contains(strings.ToLower(s), f) {
			return true
		}
	}
	return false
}

// wtRows groups the cards that pass the filters: a header, then its
// cards, each child under its parent (worktrees.Nest; a parent that is
// gone or in another group leaves the child a root); empty groups are
// left out.
func wtRows(items []wtItem, kind wtKind, filter string, dirty func(string) (int, bool)) []wtRow {
	gone := map[string]bool{}
	for _, it := range items {
		if it.e.Gone {
			gone[it.e.Path] = true
		}
	}
	var rows []wtRow
	for _, g := range wtGroups {
		var idx []int
		var paths, parents []string
		for i, it := range items {
			if it.group == g && wtMatch(it, kind, filter, dirty) {
				idx = append(idx, i)
				paths = append(paths, it.e.Path)
				p := it.e.Meta.Parent
				if gone[p] {
					p = ""
				}
				parents = append(parents, p)
			}
		}
		if len(idx) == 0 {
			continue
		}
		rows = append(rows, wtRow{group: g, item: -1, count: len(idx)})
		order, depth := worktrees.Nest(paths, parents)
		for k, o := range order {
			rows = append(rows, wtRow{group: g, item: idx[o], depth: depth[k]})
		}
	}
	return rows
}

// wtTreeMark is the mark before a child's name: "└ " one level down,
// "└ … " deeper (drawn one level in).
func wtTreeMark(depth int) string {
	switch {
	case depth <= 0:
		return ""
	case depth == 1:
		return theme.ASCIIText("└ ")
	}
	return theme.ASCIIText("└ … ")
}
