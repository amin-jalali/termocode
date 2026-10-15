package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/git"
)

// graphModel200 is a model with 200 GRAPH commits (one per 3 hours back
// from a fixed now, so they spread over several date groups) and every
// group shown.
func graphModel200(tb testing.TB) *Model {
	tb.Helper()
	now := time.Date(2026, 10, 15, 20, 0, 0, 0, time.Local)
	tb.Cleanup(clock.Set(func() time.Time { return now }))
	cs := make([]git.GraphCommit, 200)
	for i := range cs {
		cs[i] = git.GraphCommit{Hash: "h" + itoa(i), FullHash: "full" + itoa(i), Subject: "feat: change " + itoa(i),
			Author: "Amin Jalali", Committed: now.Add(-time.Duration(i) * 3 * time.Hour).Unix()}
	}
	cs[0].IsHead, cs[0].Refs = true, []git.Ref{{Name: "main"}, {Name: "origin/main", Kind: git.RefRemote}}
	m := &Model{gitChangesCollapsed: true}
	m.setGitCommits(cs, true)
	for i := 0; i < 14; i++ {
		m.gitShowMore()
	}
	return m
}

// Budget (scm-graph design §4.6): the accordion rows with 200 grouped
// commits.
func BenchmarkGitPanelRows200(b *testing.B) {
	m := graphModel200(b)
	if n := len(m.gitPanelRows()); n < 200 {
		b.Fatalf("only %d rows", n)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.gitPanelRows()
	}
}

// Budget (scm-graph design §4.6): a GRAPH frame renders the visible rows
// only (≤ 1 ms for a 40-row sidebar).
func BenchmarkGraphVisibleRows(b *testing.B) {
	m := graphModel200(b)
	rows := m.gitPanelRows()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, r := range rows[:40] {
			_ = m.renderGitPanelRow(r, false, false, 30)
		}
	}
}

// The hover card for one commit (rebuilt on each frame while shown).
func BenchmarkCommitHoverCard(b *testing.B) {
	d := git.CommitDetail{Hash: "a1b2c3d", Author: "Amin Jalali", DateRel: "2 hours ago", DateAbs: "2026-10-15 14:32",
		Subject: "fix: flaky test", Body: "The test waited 100 ms.\nOn a loaded CI box this is too short.", Files: 4, Add: 88, Del: 24}
	refs := []git.Ref{{Name: "main"}, {Name: "v1.2", Kind: git.RefTag}, {Name: "origin/main", Kind: git.RefRemote}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = renderCommitHoverCard(d, refs, commitHoverCardW)
	}
}

// Spec (scm-graph design §4.6): Source Control adds no timers; the only
// periodic work stays the 2 s git poll in git.go.
func TestNoNewTimers(t *testing.T) {
	files, err := filepath.Glob("git*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no git*.go files: %v", err)
	}
	timer := regexp.MustCompile(`tea\.Tick\(|tea\.Every\(|time\.NewTicker\(|time\.Tick\(|time\.AfterFunc\(|time\.NewTimer\(`)
	n := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range strings.Split(string(b), "\n") {
			if timer.MatchString(l) {
				n++
				if f != "git.go" || !strings.Contains(l, "gitPollInterval") {
					t.Errorf("%s: a new timer: %s", f, strings.TrimSpace(l))
				}
			}
		}
	}
	if n != 1 {
		t.Errorf("%d timers in git*.go, want only the git poll", n)
	}
}

// Perf guard: 200 grouped commits give their rows in well under a
// frame. The limit is loose (budget 50 µs; guard 5 ms over 1 call
// averaged on 50) so -race and busy CI boxes do not flake.
func TestGitPanelRows200Fast(t *testing.T) {
	m := graphModel200(t)
	t0 := time.Now()
	for i := 0; i < 50; i++ {
		_ = m.gitPanelRows()
	}
	if per := time.Since(t0) / 50; per > 5*time.Millisecond {
		t.Errorf("gitPanelRows with 200 commits: %v per call (budget 50 µs, guard 5 ms)", per)
	}
}
