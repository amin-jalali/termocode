package app

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/theme"
)

// graphModel has n commits, one per hour back from now, then 40 more
// days back one per day.
func graphModel(t *testing.T, today, older int, more bool) *Model {
	t.Helper()
	now := time.Date(2026, 10, 15, 20, 0, 0, 0, time.Local)
	t.Cleanup(clock.Set(func() time.Time { return now }))
	var cs []git.GraphCommit
	for i := 0; i < today; i++ {
		cs = append(cs, git.GraphCommit{Hash: "t" + itoa(i), Subject: "today " + itoa(i), Committed: now.Add(-time.Duration(i) * time.Hour).Unix()})
	}
	for i := 0; i < older; i++ {
		cs = append(cs, git.GraphCommit{Hash: "o" + itoa(i), Subject: "old " + itoa(i), Committed: now.AddDate(0, 0, -3-i).Unix()})
	}
	m := &Model{gitChangesCollapsed: true}
	m.setGitCommits(cs, more)
	return m
}

// graphKinds lists the GRAPH body rows as short strings.
func graphKinds(m *Model) []string {
	var out []string
	for _, r := range m.gitPanelRows() {
		switch r.kind {
		case gitRowGroup:
			out = append(out, "["+r.group+"]")
		case gitRowCommit:
			out = append(out, r.hash)
		case gitRowMore:
			if r.older {
				out = append(out, "older")
			} else {
				out = append(out, "more")
			}
		}
	}
	return out
}

// Spec (scm-graph §2.2): GRAPH shows all of Today and Yesterday, at least
// 8 and at most 15 commits, in date groups, then "Show 15 more".
func TestGraphDefaultVisible(t *testing.T) {
	m := graphModel(t, 3, 30, false)
	got := graphKinds(m)
	if got[0] != "[Today]" || got[4] != "[This week]" || got[len(got)-1] != "more" {
		t.Fatalf("rows = %v", got)
	}
	if n := strings.Count(strings.Join(got, " "), " o") + 3; n != 8 {
		t.Errorf("visible commits = %d, want 8 (%v)", n, got)
	}
	if m := graphModel(t, 20, 0, false); m.gitGraphVisible() != 15 {
		t.Errorf("20 today: visible = %d, want 15", m.gitGraphVisible())
	}
	if m := graphModel(t, 2, 1, false); len(graphKinds(m)) != 5 { // 2 groups + 3 commits, no more row
		t.Errorf("3 commits: rows = %v", graphKinds(m))
	}
}

func TestGraphShowMoreAndLoadOlder(t *testing.T) {
	m := graphModel(t, 3, 30, true)
	if cmd := m.gitShowMore(); cmd != nil || m.gitGraphVisible() != 23 {
		t.Fatalf("show more: visible = %d, cmd = %v", m.gitGraphVisible(), cmd != nil)
	}
	m.gitShowMore()
	if m.gitGraphVisible() != 33 || graphKinds(m)[len(graphKinds(m))-1] != "older" {
		t.Fatalf("all shown: visible = %d, rows end %v", m.gitGraphVisible(), graphKinds(m)[len(graphKinds(m))-3:])
	}
	if cmd := m.gitShowMore(); cmd == nil {
		t.Fatal("Load older did not start a fetch")
	}
	// A page that does not follow the loaded commits is dropped.
	m.onGitOlder(gitOlderMsg{skip: 5, commits: []git.GraphCommit{{Hash: "x"}}})
	if len(m.gitCommits) != 33 {
		t.Fatalf("stale page appended: %d", len(m.gitCommits))
	}
	m.onGitOlder(gitOlderMsg{skip: 33, commits: []git.GraphCommit{{Hash: "n1", Committed: 1}, {Hash: "n2", Committed: 1}}})
	if len(m.gitCommits) != 35 || m.gitGraphMore || graphKinds(m)[len(graphKinds(m))-1] != "n2" {
		t.Errorf("after the last page: %d commits, more=%v, rows end %v", len(m.gitCommits), m.gitGraphMore, graphKinds(m)[len(graphKinds(m))-2:])
	}
	// No more pages: "Show more" does nothing.
	if cmd := m.gitShowMore(); cmd != nil {
		t.Error("fetch started with nothing left")
	}
}

func TestGraphFoldGroup(t *testing.T) {
	m := graphModel(t, 3, 5, false)
	m.gitToggleGroup("Today")
	got := strings.Join(graphKinds(m), " ")
	if strings.Contains(got, "t0") || !strings.Contains(got, "[Today] [This week] o0") {
		t.Errorf("folded Today: %s", got)
	}
	m.gitToggleGroup("Today")
	if !strings.Contains(strings.Join(graphKinds(m), " "), "t0") {
		t.Error("unfold did not bring the rows back")
	}
}

// Spec (scm-graph §2.2): every row is exactly the sidebar width, the age
// sits in the same column, initials show only from 40 cells, nothing is
// bold, and refs that do not fit fold into "+N".
func TestRenderGitGraphCommitWidths(t *testing.T) {
	m := graphModel(t, 1, 0, false)
	c := git.GraphCommit{Hash: "a1", Subject: "fix: a long subject that never fits in the sidebar at all",
		Author: "Amin Jalali", Committed: m.gitCommits[0].Committed, IsHead: true, Unpushed: true,
		Refs: []git.Ref{{Name: "main"}, {Name: "v1.2", Kind: git.RefTag}, {Name: "origin/main", Kind: git.RefRemote}}}
	for _, w := range []int{26, 30, 45, 60} {
		row := renderGitGraphCommit(c, false, false, w)
		plain := ansi.Strip(row)
		if got := ansi.StringWidth(plain); got != w {
			t.Errorf("w=%d: width %d: %q", w, got, plain)
		}
		if !strings.HasSuffix(plain, "now") {
			t.Errorf("w=%d: age not at the right edge: %q", w, plain)
		}
		if strings.Contains(plain, "AJ") != (w >= 40) {
			t.Errorf("w=%d: initials shown=%v: %q", w, strings.Contains(plain, "AJ"), plain)
		}
		if !strings.Contains(plain, theme.GlyphUnpushed.String()) {
			t.Errorf("w=%d: no unpushed mark: %q", w, plain)
		}
		if strings.Contains(row, "\x1b[1m") || strings.Contains(row, ";1m") || strings.Contains(row, "\x1b[2m") {
			t.Errorf("w=%d: bold or faint in a GRAPH row: %q", w, row)
		}
	}
	if plain := ansi.Strip(renderGitGraphCommit(c, false, false, 45)); !strings.Contains(plain, "+") {
		t.Errorf("w=45: refs that do not fit are not folded into +N: %q", plain)
	}
	if plain := ansi.Strip(renderGitGraphCommit(c, false, false, 60)); !strings.Contains(plain, "main") {
		t.Errorf("w=60: no branch pill: %q", plain)
	}
}

func TestGitTypePrefix(t *testing.T) {
	for in, want := range map[string]int{
		"fix: x": 5, "feat(git): y": 11, "feat(ui)!: z": 11, "Merge branch 'x'": 0,
		"Fix: x": 0, "a: b": 3, "nothing here": 0, "fix(: x": 0,
	} {
		if got := gitTypePrefix(in); got != want {
			t.Errorf("gitTypePrefix(%q) = %d, want %d", in, got, want)
		}
	}
}
