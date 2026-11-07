package agentlog_test

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/agentlog/agentlogtest"
)

// Spec (CMP-30): an empty root builds the sessions of every project
// (Agent: Search All Sessions…), so a search from another folder still
// finds them; the project build of that other folder stays empty.
func TestIndexBuildAllProjects(t *testing.T) {
	home := agentlogtest.FakeHome(t)
	h := agentlog.HomesFrom(home, func(string) string { return "" })
	x := agentlog.NewIndex()
	if got := x.Build(h, "/work/other"); len(got) != 0 {
		t.Fatalf("other project: %+v", got)
	}
	all := agentlog.NewIndex().Build(h, "")
	if len(all) != 2 {
		t.Fatalf("all projects: %d sessions %+v", len(all), all)
	}
	for _, s := range all {
		if s.Cwd != agentlogtest.Folder {
			t.Errorf("cwd %q", s.Cwd)
		}
	}
	hits, capped := agentlog.Search(all, "queue", agentlog.DefaultSearchLimits)
	if len(hits) == 0 || capped {
		t.Errorf("hits %+v capped %v", hits, capped)
	}
}
