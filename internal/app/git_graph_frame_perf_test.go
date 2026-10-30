package app

import (
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/theme"
)

// BenchmarkGitSidebarFrame is the Source Control sidebar (GRAPH with 200
// commits, 50 rows high) drawn while the cursor moves down it.
func BenchmarkGitSidebarFrame(b *testing.B) {
	perfColour(b)
	m := graphModel200(b)
	m.gitLoaded, m.gitIsRepo = true, true
	rows := len(m.gitPanelRows())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.gitCursor = i % rows
		_ = m.renderGitSidebar(42, 50)
	}
}

// The remembered GRAPH rows match a fresh draw after each change: the
// cursor, the focus, the width, the age (time passes), a compare mark,
// the theme, and new commits.
func TestGitCommitRowMemoMatchesFreshDraw(t *testing.T) {
	perfColour(t)
	m := graphModel200(t)
	m.gitLoaded, m.gitIsRepo = true, true
	now := time.Date(2026, 10, 15, 20, 0, 0, 0, time.Local)
	check := func(step string, ci int, mark string, active, focused bool, w int) {
		t.Helper()
		got := m.gitRowMemo.commitRow(m.gitCommits, ci, mark, active, focused, w)
		if want := renderGitGraphCommitMark(m.gitCommits[ci], mark, active, focused, w); got != want {
			t.Fatalf("%s: remembered row %q, fresh %q", step, got, want)
		}
	}
	for _, ci := range []int{0, 1, 5} {
		check("first", ci, "", false, false, 40)
		check("again", ci, "", false, false, 40)
		check("cursor", ci, "", true, false, 40)
		check("focus", ci, "", true, true, 40)
		check("width", ci, "", true, true, 33)
		check("mark", ci, "A", true, true, 33)
	}
	t.Cleanup(clock.Set(func() time.Time { return now.Add(50 * 24 * time.Hour) }))
	check("later", 1, "", false, false, 40)
	theme.ApplyTheme("catppuccin-latte")
	check("theme", 1, "", false, false, 40)
	m.setGitCommits([]git.GraphCommit{{Hash: "zz1", FullHash: "zz1full", Subject: "fix: a new head", Author: "Ada L", Committed: now.Unix(), IsHead: true}}, false)
	check("new commits", 0, "", false, false, 40)
}
