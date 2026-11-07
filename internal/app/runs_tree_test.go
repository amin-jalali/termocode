package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
)

// CMP-10: the Runs view shows a run the AI started from another run
// (By.ParentRun) under that run, one level deeper: a tree of parent and
// child runs. "t" turns the tree off (a flat list by time) and on again.

func runsTreeItem(key, id, parent string, g runsGroup, ago time.Duration) runsItem {
	it := runsItem{key: key, group: g, started: runsDemoNow.Add(-ago)}
	it.run.ID = id
	if parent != "" {
		it.run.By = &ai.RunBy{ParentRun: parent, Depth: 1}
	}
	return it
}

func runsTreeKeys(items []runsItem) string {
	var out []string
	for _, it := range items {
		out = append(out, strings.Repeat(".", it.depth)+it.key)
	}
	return strings.Join(out, " ")
}

// Spec: children follow their parent (oldest child first), one level
// deeper per generation; a parent with a live child moves up to NOW; a
// child whose parent is not listed, or a loop, stays a top-level row.
func TestRunsNestOrder(t *testing.T) {
	items := []runsItem{
		runsTreeItem("c/live", "live", "", runsGroupNow, time.Minute),
		runsTreeItem("c/kid2", "kid2", "top", runsGroupNow, 2*time.Minute),
		runsTreeItem("c/top", "top", "", runsGroupToday, time.Hour),
		runsTreeItem("c/kid1", "kid1", "top", runsGroupToday, 50*time.Minute),
		runsTreeItem("c/grand", "grand", "kid1", runsGroupToday, 40*time.Minute),
		runsTreeItem("c/lost", "lost", "gone", runsGroupToday, 2*time.Hour),
		runsTreeItem("c/a", "a", "b", runsGroupOlder, 48*time.Hour),
		runsTreeItem("c/b", "b", "a", runsGroupOlder, 49*time.Hour),
	}
	got := runsNest(items)
	want := "c/live c/top .c/kid1 ..c/grand .c/kid2 c/lost c/a c/b"
	if k := runsTreeKeys(got); k != want {
		t.Fatalf("order\n got %s\nwant %s", k, want)
	}
	byKey := map[string]runsItem{}
	for _, it := range got {
		byKey[it.key] = it
	}
	if g := byKey["c/top"].group; g != runsGroupNow {
		t.Errorf("parent of a live child: group %d, want NOW", g)
	}
	if g := byKey["c/grand"].group; g != runsGroupNow {
		t.Errorf("grandchild: group %d, want its root's NOW", g)
	}
	if p, n := byKey["c/kid1"].parent, byKey["c/top"].kids; p != "c/top" || n != 2 {
		t.Errorf("kid1 parent %q, top kids %d", p, n)
	}
	if p := byKey["c/lost"].parent; p != "" {
		t.Errorf("a run whose parent is not listed has parent %q", p)
	}
	if len(got) != len(items) {
		t.Errorf("%d rows, want %d", len(got), len(items))
	}
}

// runsTreeRig is the list demo with the done run "why does the toast
// flicker" started by a step of the live run.
func runsTreeRig(t *testing.T) *Model {
	t.Helper()
	m := runsRig(t, "list", 200, 50)
	for ci := range m.runs.chats {
		for mi := range m.runs.chats[ci].Messages {
			if r := m.runs.chats[ci].Messages[mi].Run; r != nil && r.ID == "runwhy" {
				r.By = &ai.RunBy{Chat: "demo-live", ParentRun: "runlive", Step: "Fixer", Depth: 1}
			}
		}
	}
	return m
}

// Spec: the child run is drawn under its parent with a branch line, in
// the parent's group; "t" gives the flat list back and says so.
func TestRunsTreeSidebar(t *testing.T) {
	m := runsTreeRig(t)
	items := m.runsItems(clock.Now())
	if k := runsTreeKeys(items[:2]); k != "demo-live/runlive .demo-why/runwhy" {
		t.Fatalf("tree order %s", k)
	}
	side := runsSideText(m)
	wantAll(t, side, "◉ fix the toast f", "└ ✓ why does the")
	lines := strings.Split(side, "\n")
	parent, child, today := -1, -1, -1
	for i, l := range lines {
		switch {
		case strings.Contains(l, "fix the toast f") && parent < 0:
			parent = i
		case strings.Contains(l, "why does the"):
			child = i
		case strings.Contains(l, "TODAY"):
			today = i
		}
	}
	if parent < 0 || child < 0 || today < 0 || !(parent < child && child < today) {
		t.Fatalf("the child is not under its parent in NOW (parent %d child %d TODAY %d):\n%s", parent, child, today, side)
	}
	m.focus = FocusExplorer
	_, cmd := m.handleRunsSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if cmd == nil {
		t.Error("t should say the list is flat now")
	}
	if !m.runs.flat {
		t.Fatal("t did not turn the tree off")
	}
	flat := m.runsItems(clock.Now())
	for _, it := range flat {
		if it.depth != 0 {
			t.Errorf("flat list: %s has depth %d", it.key, it.depth)
		}
		if it.key == "demo-why/runwhy" && it.group != runsGroupToday {
			t.Errorf("flat list: the child is in group %d, want TODAY", it.group)
		}
	}
	if s := runsSideText(m); strings.Contains(s, "└ ✓ why") {
		t.Errorf("flat list still draws the branch:\n%s", s)
	}
	m.handleRunsSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if m.runs.flat {
		t.Error("a second t did not turn the tree on")
	}
}

// Spec: a run's page lists its family (RUN TREE): the parent run and the
// sub-runs, each a click target that opens that run.
func TestRunsTreeDetailPage(t *testing.T) {
	m := runsTreeRig(t)
	m.runsShowPage("demo-live/runlive", true)
	page := runsPageText(m, 165, 120)
	wantAll(t, page, "RUN TREE", "1 sub-run", "└ ✓ why does the toast flicker")
	if !runsHasHit(m.runs.cache.pageHits, "run:demo-why/runwhy") {
		t.Error("the sub-run row is not a click target")
	}
	m.runsShowPage("demo-why/runwhy", true)
	page = runsPageText(m, 165, 120)
	wantAll(t, page, "RUN TREE", "↑ ◉ fix the toast flicker", "this run")
	if !runsHasHit(m.runs.cache.pageHits, "run:demo-live/runlive") {
		t.Error("the parent row is not a click target")
	}
}

func runsHasHit(hits []runsHit, id string) bool {
	for _, h := range hits {
		if h.id == id {
			return true
		}
	}
	return false
}

// Spec: the web gets the same tree: rows in tree order with the parent's
// key and the depth.
func TestWebRunsTree(t *testing.T) {
	m := runsTreeRig(t)
	snap := m.webRunsBuild(clock.Now())
	if len(snap.Runs) < 2 {
		t.Fatalf("%d runs", len(snap.Runs))
	}
	kid := snap.Runs[1]
	if kid.Key != "demo-why/runwhy" || kid.Parent != "demo-live/runlive" || kid.Depth != 1 {
		t.Errorf("second row %+v, want the child of the live run at depth 1", kid)
	}
	if snap.Runs[0].Depth != 0 || snap.Runs[0].Kids != 1 {
		t.Errorf("first row %+v, want the parent with 1 sub-run", snap.Runs[0])
	}
}
