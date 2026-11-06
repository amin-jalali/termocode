package app

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/perfbudget"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// Perf guard (WTX-2): one frame of the WORKTREES view with 200 worktrees
// and nothing cached (the cards built and drawn), at 150x42.
func TestPerfWorktreesDraw200(t *testing.T) {
	perfbudget.Require(t)
	list := []git.Worktree{{Path: "/src/app", Branch: "main", Main: true}}
	vals := map[string]git.WorktreeStatus{}
	metas := map[string]worktrees.Meta{}
	var tabs []terminalTab
	for i := 1; i < 200; i++ {
		p := fmt.Sprintf("/src/app.worktrees/w%03d", i)
		list = append(list, git.Worktree{Path: p, Branch: fmt.Sprintf("feat/w%03d", i)})
		vals[p] = git.WorktreeStatus{Ahead: i % 4, Dirty: i % 7}
		if i%10 == 0 {
			metas[p] = worktrees.Meta{Pinned: true, Notes: []worktrees.Note{{Text: "a note"}}}
		}
		if i%5 == 0 {
			tabs = append(tabs, terminalTab{Agent: "claude", Cwd: p, AgentState: agents.Working})
		}
	}
	st := newWTState()
	st.loaded, st.root = true, "/src/app"
	st.entries = worktrees.Build(list, metas)
	st.cache = wtCannedCache(vals)
	m := Model{wt: st, terminalTabs: tabs, focus: FocusExplorer}
	var ds []time.Duration
	for i := 0; i < 9; i++ {
		st.touch() // no cache hit
		t0 := time.Now()
		m.renderWorktreesSidebar(29, 42)
		ds = append(ds, time.Since(t0))
	}
	slices.Sort(ds)
	perfbudget.Check(t, "worktrees_draw_200", float64(ds[4].Microseconds()/100)/10)
}
