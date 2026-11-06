package app

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentroll"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

var wtTestNow = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

// wtFixture is a repository with every kind of card: the main checkout
// (this window), a pinned user worktree whose Claude Code tab waits, a
// user worktree another window's agent works in, one asleep, one
// archived (not shown), an external one, an agent lane and a gone one.
func wtFixture(t *testing.T) (*wtState, []wtTabAgent) {
	t.Helper()
	list := []git.Worktree{
		{Path: "/src/app", Branch: "main", Main: true},
		{Path: "/src/app.worktrees/wt-sidebar", Branch: "feat/wt-sidebar"},
		{Path: "/src/app.worktrees/toast", Branch: "fix/toast-race"},
		{Path: "/src/app.worktrees/grpc", Branch: "spike/grpc"},
		{Path: "/src/app.worktrees/old", Branch: "old"},
		{Path: "/elsewhere/api-v2", Branch: "api-v2", Locked: true},
		{Path: "/src/app/.git/termocode/worktrees/r9-a", Branch: "termocode/agent-r9-a", Agent: true, AgentID: "r9-a"},
		{Path: "/tmp/deleted", Branch: "gone", Prunable: true},
	}
	metas := map[string]worktrees.Meta{
		"/src/app.worktrees/wt-sidebar": {Pinned: true, Notes: []worktrees.Note{{Text: "adding the pin row"}}, Issue: 412, LastSeen: wtTestNow.Add(-2 * time.Minute)},
		"/src/app.worktrees/grpc":       {State: worktrees.StateAsleep, LastSeen: wtTestNow.Add(-72 * time.Hour)},
		"/src/app.worktrees/old":        {State: worktrees.StateArchived},
	}
	st := newWTState()
	st.loaded, st.root = true, "/src/app"
	st.entries = worktrees.Build(list, metas)
	st.others = []agentroll.Rollup{{Workspace: "/src/app.worktrees/toast", PID: 7, Items: []agentroll.Item{{Name: "Codex", State: "working", Worktree: "/src/app.worktrees/toast/sub"}}}}
	wtFillCache(t, st, map[string]git.WorktreeStatus{
		"/src/app":                      {},
		"/src/app.worktrees/wt-sidebar": {Ahead: 3, Dirty: 4, Upstream: true},
		"/src/app.worktrees/toast":      {Ahead: 1, Dirty: 2},
		"/elsewhere/api-v2":             {Behind: 12, Upstream: true},
	})
	tabs := []wtTabAgent{
		{name: "Claude Code", cwd: "/src/app.worktrees/wt-sidebar", state: agents.Waiting},
		{name: "", cwd: "/src/app"}, // a plain shell
	}
	return st, tabs
}

// wtFillCache fills st's status cache from a table, without git.
func wtFillCache(t *testing.T, st *wtState, vals map[string]git.WorktreeStatus) {
	t.Helper()
	st.cache = wtCannedCache(vals)
}

func TestWTItemsGroupsAndAgents(t *testing.T) {
	st, tabs := wtFixture(t)
	items := wtItems(st.entries, tabs, st.others, st.root)
	by := map[string]wtItem{}
	for _, it := range items {
		by[it.e.Path] = it
	}
	check := func(path, group, state string) {
		t.Helper()
		it, ok := by[path]
		if !ok || it.group != group || it.state != state {
			t.Errorf("%s: group %q state %q (listed %v), want %s %s", path, it.group, it.state, ok, group, state)
		}
	}
	check("/src/app.worktrees/wt-sidebar", "PINNED", wtStateWaiting)
	check("/src/app", "ACTIVE", wtStateIdle)
	check("/src/app.worktrees/toast", "ACTIVE", wtStateWorking)
	check("/src/app.worktrees/grpc", "ASLEEP", wtStateAsleep)
	check("/elsewhere/api-v2", "EXTERNAL", wtStateIdle)
	check("/src/app/.git/termocode/worktrees/r9-a", "ACTIVE", wtStateIdle)
	check("/tmp/deleted", "EXTERNAL", wtStateGone)
	check("/src/app.worktrees/old", "ARCHIVED", wtStateArchived) // WTX-14: the row stays, x restores it
	if !by["/src/app"].here || by["/src/app.worktrees/toast"].here {
		t.Error("you are here is on the wrong card")
	}
	if a := by["/src/app.worktrees/toast"].agent; a.name != "Codex" || a.tab != -1 {
		t.Errorf("other window's agent: %+v", a)
	}
	if a := by["/src/app.worktrees/wt-sidebar"].agent; a.name != "Claude Code" || a.tab != 0 {
		t.Errorf("tab agent: %+v", a)
	}
	// ACTIVE: the working agent first, then git's order (main first).
	rows := wtRows(items, wtKindAll, "", st.dirty)
	var order []string
	for _, r := range rows {
		if r.item < 0 {
			order = append(order, r.group)
		} else if items[r.item].group == "ACTIVE" {
			order = append(order, items[r.item].e.Path)
		}
	}
	want := "PINNED ACTIVE /src/app.worktrees/toast /src/app /src/app/.git/termocode/worktrees/r9-a ASLEEP EXTERNAL ARCHIVED"
	if got := strings.Join(order, " "); got != want {
		t.Errorf("order\n got %s\nwant %s", got, want)
	}
}

func TestWTRowsFilters(t *testing.T) {
	st, tabs := wtFixture(t)
	items := wtItems(st.entries, tabs, st.others, st.root)
	names := func(kind wtKind, text string) string {
		var out []string
		for _, r := range wtRows(items, kind, text, st.dirty) {
			if r.item >= 0 {
				out = append(out, wtTitle(items[r.item]))
			}
		}
		return strings.Join(out, ",")
	}
	for _, c := range []struct {
		kind       wtKind
		text, want string
	}{
		{wtKindMine, "", "feat/wt-sidebar,fix/toast-race,main,spike/grpc"},
		{wtKindAgents, "", "feat/wt-sidebar,fix/toast-race,agent r9-a"},
		{wtKindDirty, "", "feat/wt-sidebar,fix/toast-race"},
		{wtKindAsleep, "", "spike/grpc"},
		{wtKindExternal, "", "api-v2,gone"},
		{wtKindAll, "TOAST", "fix/toast-race"},
		{wtKindAll, "codex", "fix/toast-race"},
		{wtKindMine, "api", ""},
	} {
		if got := names(c.kind, c.text); got != c.want {
			t.Errorf("%v %q: got %q, want %q", c.kind, c.text, got, c.want)
		}
	}
}

// Card lines go from the bottom up as the panel narrows: 4 lines from
// 28 cells, 2 lines from 22, one line below that. Every line is exactly
// the panel's width.
func TestWTCardLinesByWidth(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	st, tabs := wtFixture(t)
	items := wtItems(st.entries, tabs, st.others, st.root)
	var pinned wtItem
	for _, it := range items {
		if it.group == "PINNED" {
			pinned = it
		}
	}
	for _, c := range []struct{ w, lines int }{{36, 4}, {28, 4}, {24, 2}} {
		got := wtCardLines(pinned, st.status(pinned), true, true, c.w, wtTestNow)
		if len(got) != c.lines {
			t.Errorf("w=%d: %d lines, want %d: %q", c.w, len(got), c.lines, ansi.Strip(strings.Join(got, "|")))
		}
		for i, l := range got {
			if ansi.StringWidth(l) != c.w {
				t.Errorf("w=%d line %d is %d wide: %q", c.w, i, ansi.StringWidth(l), ansi.Strip(l))
			}
		}
	}
	full := ansi.Strip(strings.Join(wtCardLines(pinned, st.status(pinned), false, false, 36, wtTestNow), "\n"))
	for _, want := range []string{"feat/wt-sidebar", "‼ waiting", "3↑", "4 dirty", "Claude Code", `"adding the`, "#412", "2m ago"} {
		if !strings.Contains(full, want) {
			t.Errorf("36 wide card misses %q:\n%s", want, full)
		}
	}
	// At 28 the counts stay whole; the branch is cut first.
	if l2 := ansi.Strip(wtCardLines(pinned, st.status(pinned), false, false, 28, wtTestNow)[1]); !strings.Contains(l2, "3↑ · 4 dirty") {
		t.Errorf("28 wide line 2: %q", l2)
	}
	one := wtCompactLine(pinned, st.status(pinned), false, false, 20)
	if ansi.StringWidth(one) != 20 || !strings.Contains(ansi.Strip(one), "3↑ ~4") {
		t.Errorf("compact: %q", ansi.Strip(one))
	}
}

func wtRenderAt(t *testing.T, st *wtState, tabs []wtTabAgent, w, h int) string {
	t.Helper()
	items := wtItems(st.entries, tabs, st.others, st.root)
	rows := wtRows(items, st.kind, st.filter, st.dirty)
	out := st.renderRaw(items, rows, w, h, true, wtTestNow)
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Fatalf("%dx%d: %d lines", w, h, len(lines))
	}
	for i, l := range lines {
		if ansi.StringWidth(l) != w {
			t.Errorf("%dx%d line %d is %d wide: %q", w, h, i, ansi.StringWidth(l), ansi.Strip(l))
		}
	}
	return ansi.Strip(out)
}

func TestWTRenderLayouts(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	st, tabs := wtFixture(t)
	tall := wtRenderAt(t, st, tabs, 29, 42)
	for _, want := range []string{"WORKTREES", "PINNED", "ACTIVE", "ASLEEP", "EXTERNAL", "you are here", "folder gone", "12↓", "? keys"} {
		if !strings.Contains(tall, want) {
			t.Errorf("29x42 misses %q:\n%s", want, tall)
		}
	}
	// A short window: one line per worktree, no group headers.
	short := wtRenderAt(t, st, tabs, 29, 21)
	if strings.Contains(short, "PINNED") || !strings.Contains(short, "api-v2 (ext)") || !strings.Contains(short, "Enter card") {
		t.Errorf("29x21 is not compact:\n%s", short)
	}
	for _, sz := range [][2]int{{20, 42}, {36, 42}, {12, 10}, {60, 24}} {
		wtRenderAt(t, st, tabs, sz[0], sz[1])
	}
	// The card page of the compact layout.
	st.detail = true
	page := wtRenderAt(t, st, tabs, 29, 21)
	for _, want := range []string{"Enter  open it here", "Esc    back to the list", "Esc back"} {
		if !strings.Contains(page, want) {
			t.Errorf("card page misses %q:\n%s", want, page)
		}
	}
	// Nothing loaded yet; not a repository.
	st2 := newWTState()
	if s := wtRenderAt(t, st2, nil, 29, 20); !strings.Contains(s, "Reading the worktrees") {
		t.Errorf("loading: %s", s)
	}
	st2.loaded, st2.err = true, "not a git repository"
	if s := wtRenderAt(t, st2, nil, 29, 20); !strings.Contains(s, "Not a git repository") {
		t.Errorf("no repo: %s", s)
	}
}

// The view draws from the cache: a second frame with nothing changed is
// the same string and reads no status (no git call per frame).
func TestWTRenderFromCache(t *testing.T) {
	st, _ := wtFixture(t)
	var calls atomic.Int32
	inner := st.cache
	st.cache = worktrees.NewStatusCache(func(p string) (git.WorktreeStatus, error) {
		calls.Add(1)
		s, _ := inner.Get(p)
		return s.WorktreeStatus, nil
	})
	m := Model{wt: st}
	a := m.renderWorktreesSidebar(29, 42)
	b := m.renderWorktreesSidebar(29, 42)
	if a != b || calls.Load() != 0 {
		t.Errorf("second frame changed (%v) or read %d statuses", a != b, calls.Load())
	}
}
