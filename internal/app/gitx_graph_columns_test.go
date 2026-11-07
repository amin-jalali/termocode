package app

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/git"
)

// GITX-39 (graph columns): a GRAPH row shows the author and the date by
// default and no SHA; each column has its own toggle, every row stays
// the sidebar width, and the choice is saved in config.json
// (git_graph_columns) and read back at the next start.
func TestGitxGraphColumnsRender(t *testing.T) {
	m := graphModel(t, 1, 0, false)
	t.Cleanup(func() { gitGraphCols.Store(0) })
	gitGraphCols.Store(0)
	c := git.GraphCommit{Hash: "a1b2c3d", Subject: "fix: a", Author: "Amin Jalali", Committed: m.gitCommits[0].Committed}
	row := func(w int) string {
		r := ansi.Strip(renderGitGraphCommit(c, false, false, w))
		if got := ansi.StringWidth(r); got != w {
			t.Errorf("w=%d: width %d: %q", w, got, r)
		}
		return r
	}
	if r := row(60); !strings.Contains(r, "AJ") || !strings.HasSuffix(r, "now") || strings.Contains(r, "a1b2c3d") {
		t.Fatalf("default row %q", r)
	}
	gitGraphColToggle(gitColSHA)
	if r := row(60); !strings.Contains(r, "a1b2c3d") || !strings.Contains(r, "AJ") {
		t.Errorf("sha on: %q", r)
	}
	if r := row(30); strings.Contains(r, "a1b2c3d") {
		t.Errorf("sha shown below 40 cells: %q", r)
	}
	gitGraphColToggle(gitColAuthor)
	if r := row(60); strings.Contains(r, "AJ") || !strings.Contains(r, "a1b2c3d") {
		t.Errorf("author off: %q", r)
	}
	gitGraphColToggle(gitColDate)
	if r := row(60); strings.Contains(r, "now") {
		t.Errorf("date off: %q", r)
	}
	gitGraphColToggle(gitColDate)
	if r := row(60); !strings.HasSuffix(r, "now") {
		t.Errorf("date back on: %q", r)
	}
}

func TestGitxGraphColumnsToggleSaves(t *testing.T) {
	path, err := settingsConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path); gitGraphCols.Store(0) })
	gitGraphCols.Store(0)
	m, _ := gitxLogRepo(t)
	for _, id := range []string{"git-graph-col-author", "git-graph-col-date", "git-graph-col-sha"} {
		if _, ok := aiActionPolicies[id]; !ok {
			t.Errorf("no AI policy for %s", id)
		}
	}
	m.dispatchPaletteAction("git-graph-col-sha")
	if !toastHas(m, "SHA column shown") {
		t.Error("no toast for the SHA column")
	}
	m.dispatchPaletteAction("git-graph-col-author")
	if !toastHas(m, "Author column hidden") {
		t.Error("no toast for the author column")
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"git_graph_columns"`) || !strings.Contains(string(b), `"sha"`) || strings.Contains(string(b), `"author"`) {
		t.Fatalf("config.json:\n%s", b)
	}
	gitGraphCols.Store(0)
	gitGraphColsInit()
	if gitGraphColOn(gitColAuthor) || !gitGraphColOn(gitColDate) || !gitGraphColOn(gitColSHA) {
		t.Errorf("read back %b", gitGraphCols.Load())
	}
}
