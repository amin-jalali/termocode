package agentlog_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/agentlog/agentlogtest"
	"github.com/amin-jalali/termocode/internal/perfbudget"
)

func fixtureIndex(t testing.TB) (agentlog.Homes, []agentlog.Summary) {
	home := agentlogtest.FakeHome(t)
	h := agentlog.HomesFrom(home, func(string) string { return "" })
	return h, agentlog.NewIndex().Build(h, agentlogtest.Folder)
}

// Spec (AGX-10): a local, case-blind text search over this project's
// sessions: prompts, answers, tool calls and results. Each hit has the
// session, the turn's time and a one-line snippet around the match; at
// most 3 hits per file. Caps: Files and Bytes (the sum of the files
// read); a capped search says so.
func TestSearch(t *testing.T) {
	_, sums := fixtureIndex(t)
	hits, capped := agentlog.Search(sums, "QUEUE", agentlog.SearchLimits{})
	if capped || len(hits) == 0 {
		t.Fatalf("hits %d capped %v", len(hits), capped)
	}
	for _, h := range hits {
		if h.Session.CLI != agentlog.Codex || !strings.Contains(strings.ToLower(h.Snippet), "queue") || h.Time.IsZero() || strings.Contains(h.Snippet, "\n") {
			t.Errorf("hit %+v", h)
		}
	}
	if !hits[0].Time.Equal(time.Date(2026, 11, 5, 11, 0, 2, 0, time.UTC)) || hits[0].Snippet != "Fix the failing test in queue.go" {
		t.Errorf("first hit %v %q", hits[0].Time, hits[0].Snippet)
	}
	if n := len(hits); n > 3 {
		t.Errorf("%d hits in one file, want at most 3", n)
	}
	if hits, _ := agentlog.Search(sums, "worktree", agentlog.SearchLimits{}); len(hits) != 3 {
		t.Errorf("worktree: %d hits, want the first 3", len(hits))
	}
	if hits, capped := agentlog.Search(sums, "the", agentlog.SearchLimits{Files: 1}); !capped || len(hits) == 0 || hits[0].Session.Path != sums[0].Path {
		t.Errorf("file cap: %d hits capped %v", len(hits), capped)
	}
	if _, capped := agentlog.Search(sums, "the", agentlog.SearchLimits{Bytes: 10}); !capped {
		t.Error("byte cap not reported")
	}
	if hits, _ := agentlog.Search(sums, "  ", agentlog.SearchLimits{}); len(hits) != 0 {
		t.Errorf("blank query: %d hits", len(hits))
	}
	l := agentlog.DefaultSearchLimits
	if l.Files != 200 || l.Bytes != 50<<20 || l.PerFile != 3 {
		t.Errorf("default limits %+v", l)
	}
}

// Spec (AGX-10, D1): the search runs on redacted text: a secret in a
// session is never a hit and never in a snippet.
func TestSearchRedacted(t *testing.T) {
	dir := t.TempDir()
	key := "sk-" + "ant-" + "api03-" + strings.Repeat("Z", 40)
	p := filepath.Join(dir, "s.jsonl")
	line := `{"type":"user","sessionId":"s","cwd":"/w","timestamp":"2026-11-05T10:00:00Z","message":{"role":"user","content":"deploy with ` + key + ` now"}}` + "\n"
	if err := os.WriteFile(p, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	sums := []agentlog.Summary{{CLI: agentlog.Claude, Path: p}}
	if hits, _ := agentlog.Search(sums, "ZZZZ", agentlog.SearchLimits{}); len(hits) != 0 {
		t.Errorf("the secret is a hit: %+v", hits)
	}
	hits, _ := agentlog.Search(sums, "deploy", agentlog.SearchLimits{})
	if len(hits) != 1 || strings.Contains(hits[0].Snippet, "ZZZZ") {
		t.Errorf("hits %+v", hits)
	}
}

// Spec (AGX-10): a search over 200 sessions is within
// agent_sessions_search_200.
func TestPerfAgentSessionsSearch200(t *testing.T) {
	perfbudget.Require(t)
	h, sums := fixtureIndex(t)
	src, _ := os.ReadFile(sums[0].Path)
	for i := 0; i < 198; i++ {
		_ = os.WriteFile(filepath.Join(filepath.Dir(sums[0].Path), "copy-"+itoa(i)+".jsonl"), src, 0o600)
	}
	sums = agentlog.NewIndex().Build(h, agentlogtest.Folder)
	if len(sums) != 200 {
		t.Fatalf("%d sessions", len(sums))
	}
	var ds []time.Duration
	for i := 0; i < 5; i++ {
		start := time.Now()
		agentlog.Search(sums, "drains", agentlog.DefaultSearchLimits)
		ds = append(ds, time.Since(start))
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	perfbudget.Check(t, "agent_sessions_search_200", float64(ds[len(ds)/2].Microseconds())/1000)
}
