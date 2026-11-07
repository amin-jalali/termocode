package app

import (
	"sort"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Runs: the run tree (CMP-10) ──────────────────────────────────────────
//
// A run the AI started from a step of another run (agent_run_start in a
// step: StoredRun.By.ParentRun) is that run's child. The Runs list draws
// each child under its parent, one level deeper, with a branch line:
//
//	◉ fix the toast flicker        $0.009
//	  review ●─◉ Fixer · 41 s
//	  └ ✓ why does the toast …      done
//	      review ●─● 58 s
//
// The whole family sits in the group of its most urgent run: a done
// parent with a live child is in NOW. "t" turns the tree off (the flat
// list by time) and on again. A run's page lists its family (RUN TREE):
// the parent and the sub-runs, each opens that run. The web gets the same
// order with the parent's key and the depth (webproto.RunItem).

// runsTreeMaxDepth stops a broken chain from drawing too deep (the AI may
// start runs two levels down; RunBy.Depth).
const runsTreeMaxDepth = 4

// runsNest orders items as a tree: each run whose parent run is listed
// follows it (oldest child first), depth one more than the parent's, in
// the group of its family's most urgent run. Families keep the order of
// items (group, then newest first). A run whose parent is not listed,
// or that is part of a loop, is a top-level row.
func runsNest(items []runsItem) []runsItem {
	if len(items) == 0 {
		return items
	}
	byID := make(map[string]int, len(items))
	for i, it := range items {
		if it.run.ID != "" {
			if _, dup := byID[it.run.ID]; !dup {
				byID[it.run.ID] = i
			}
		}
	}
	parent := make([]int, len(items))
	for i, it := range items {
		parent[i] = -1
		if it.run.By == nil || it.run.By.ParentRun == "" {
			continue
		}
		if j, ok := byID[it.run.By.ParentRun]; ok && j != i {
			parent[i] = j
		}
	}
	// a loop (a→b→a): every run in it becomes top-level; a run that
	// only leads into a loop keeps its parent
	for i := range items {
		seen := map[int]bool{i: true}
		for j := parent[i]; j >= 0; j = parent[j] {
			if seen[j] {
				k := j
				for {
					next := parent[k]
					parent[k] = -1
					if next < 0 || next == j {
						break
					}
					k = next
				}
				break
			}
			seen[j] = true
		}
	}
	kids := make([][]int, len(items))
	var roots []int
	for i := range items {
		if p := parent[i]; p >= 0 {
			kids[p] = append(kids[p], i)
		} else {
			roots = append(roots, i)
		}
	}
	for _, k := range kids {
		sort.SliceStable(k, func(a, b int) bool { return items[k[a]].started.Before(items[k[b]].started) })
	}
	// a family's group is its most urgent run's
	var famGroup func(i, d int) runsGroup
	famGroup = func(i, d int) runsGroup {
		g := items[i].group
		if d >= runsTreeMaxDepth {
			return g
		}
		for _, k := range kids[i] {
			g = min(g, famGroup(k, d+1))
		}
		return g
	}
	type fam struct {
		root  int
		group runsGroup
	}
	fams := make([]fam, len(roots))
	for n, r := range roots {
		fams[n] = fam{r, famGroup(r, 0)}
	}
	sort.SliceStable(fams, func(a, b int) bool {
		x, y := fams[a], fams[b]
		if x.group != y.group {
			return x.group < y.group
		}
		return items[x.root].started.After(items[y.root].started)
	})
	out := make([]runsItem, 0, len(items))
	var walk func(i, d int, g runsGroup, parentKey string)
	walk = func(i, d int, g runsGroup, parentKey string) {
		it := items[i]
		it.depth, it.group, it.parent, it.kids = d, g, parentKey, len(kids[i])
		out = append(out, it)
		for _, k := range kids[i] {
			if d+1 > runsTreeMaxDepth {
				// too deep: the rest of the chain as rows at the last level
				walk(k, d, g, it.key)
				continue
			}
			walk(k, d+1, g, it.key)
		}
	}
	for _, f := range fams {
		walk(f.root, 0, f.group, "")
	}
	return out
}

// runsTreeOn reports whether the list draws the tree ("t" turns it off).
func (m *Model) runsTreeOn() bool { return m.runs == nil || !m.runs.flat }

// runsToggleTree is "t": the tree off (the flat list by time) or on.
func (m *Model) runsToggleTree() tea.Cmd {
	st := m.runsS()
	st.flat = !st.flat
	st.scroll = 0
	st.cache.sideKey, st.cache.pageKy = "", ""
	m.runsEnsureSel()
	if st.flat {
		return m.runsInfo("Runs: flat list by time (t shows the run tree)")
	}
	return m.runsInfo("Runs: run tree, sub-runs under their parent (t for the flat list)")
}

// runsIndent is how many cells a row at depth d moves right.
func runsIndent(d int) int { return 2 * min(d, runsTreeMaxDepth) }

// runsBranch is the branch glyph drawn before a child row.
func runsBranch() string {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return "`"
	}
	return "└"
}

// runsFamily is run it's parent (ok=false: none listed) and its sub-runs
// (oldest first), from every run of the list.
func (m *Model) runsFamily(it runsItem, now time.Time) (parent runsItem, hasParent bool, kids []runsItem) {
	all := m.runsCollect(now)
	if it.run.By != nil && it.run.By.ParentRun != "" {
		for _, x := range all {
			if x.run.ID == it.run.By.ParentRun && x.key != it.key {
				parent, hasParent = x, true
				break
			}
		}
	}
	for _, x := range all {
		if x.key != it.key && x.run.By != nil && x.run.By.ParentRun == it.run.ID && it.run.ID != "" {
			kids = append(kids, x)
		}
	}
	sort.SliceStable(kids, func(a, b int) bool { return kids[a].started.Before(kids[b].started) })
	return parent, hasParent, kids
}

// drawRunTree draws the RUN TREE section of run it's page at row y (x0:
// the left edge) when the run has a parent or sub-runs; it returns the
// row after it and the click targets ("run:<key>").
func (m *Model) drawRunTree(cv *rcCanvas, x0, y, w int, it runsItem, now time.Time, c runsColours) (int, []runsHit) {
	parent, hasParent, kids := m.runsFamily(it, now)
	if !hasParent && len(kids) == 0 {
		return y, nil
	}
	var hits []runsHit
	cv.text(x0, y, "RUN TREE", rcSt{fg: c.muted}.B())
	sum := "no sub-runs"
	if n := len(kids); n > 0 {
		sum = strconv.Itoa(n) + " sub-run" + plural(n)
	}
	if hasParent {
		sum = "started by run " + parent.run.ID + " · " + sum
	}
	cv.textR(w-x0, y, sum, rcSt{fg: c.dim})
	y += 2
	row := func(x int, lead string, r runsItem, self bool) {
		g, col, word := r.look(c)
		x = cv.text(x, y, lead, rcSt{fg: c.dimmer})
		if lead != "" {
			x++
		}
		x = cv.text(x, y, g, rcSt{fg: col}) + 1
		title := rcSt{fg: c.primary}
		if self {
			title = title.B()
		}
		right := cv.textR(w-x0, y, word, rcSt{fg: c.muted})
		tail := ""
		if self {
			tail = "  this run"
		}
		cv.textMax(x, y, r.task+tail, right-x-1, title)
		if !self {
			hits = append(hits, runsHit{x0: x0, x1: w - x0, y: y, id: "run:" + r.key, label: r.task})
		}
		y++
	}
	x := x0
	if hasParent {
		row(x, "↑", parent, false)
		x += 2
	}
	row(x, "", it, true)
	for _, k := range kids {
		row(x+2, runsBranch(), k, false)
	}
	return y + 1, hits
}
