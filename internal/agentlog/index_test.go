package agentlog_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/agentlog/agentlogtest"
	"github.com/amin-jalali/termocode/internal/perfbudget"
)

// watchOpens records every path agentlog opens until the test ends.
func watchOpens(t testing.TB) func() []string {
	var mu sync.Mutex
	var got []string
	old := agentlog.OnOpen
	agentlog.OnOpen = func(p string) { mu.Lock(); got = append(got, p); mu.Unlock() }
	t.Cleanup(func() { agentlog.OnOpen = old })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := got
		got = nil
		return out
	}
}

// Spec (AGX-9): the history of a project lists its sessions from both
// CLIs, newest first, with the start time, the first prompt, the prompt
// count, the tokens and the folder. A second build reads no session file
// that did not change (the index is cached by modification time and
// size); a changed file is read again; a file in an unknown format is a
// row that says it cannot be read.
func TestIndexBuild(t *testing.T) {
	home := agentlogtest.FakeHome(t)
	h := agentlog.HomesFrom(home, func(string) string { return "" })
	x := agentlog.NewIndex()
	sums := x.Build(h, agentlogtest.Folder)
	if len(sums) != 2 {
		t.Fatalf("%d sessions: %+v", len(sums), sums)
	}
	byCLI := map[agentlog.CLI]agentlog.Summary{}
	for _, s := range sums {
		byCLI[s.CLI] = s
	}
	c := byCLI[agentlog.Claude]
	if c.ID != "3f9a2c1e-0000-4000-8000-000000000001" || c.FirstPrompt != "Add the pinned row as in the issue. Run go test." ||
		c.Prompts != 1 || c.Tokens != 102200+440 || c.Cwd != "/work/demo" || c.Branch != "feat/pin-row" || c.Err != "" {
		t.Errorf("claude summary %+v", c)
	}
	if !c.Start.Equal(time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("claude start %v", c.Start)
	}
	k := byCLI[agentlog.Codex]
	if k.ID != "0199aaaa-bbbb-7ccc-8ddd-eeeeeeee0001" || k.FirstPrompt != "Fix the failing test in queue.go" || k.Prompts != 1 || k.Tokens != 20000 {
		t.Errorf("codex summary %+v", k)
	}
	if sums[0].ModTime.Before(sums[1].ModTime) {
		t.Error("not newest first")
	}
	opens := watchOpens(t)
	x.Build(h, agentlogtest.Folder)
	for _, p := range opens() {
		if strings.HasSuffix(p, ".jsonl") {
			t.Errorf("second build read %s", p)
		}
	}
	// A changed file is read again; an unknown format is a row.
	_ = os.WriteFile(c.Path, []byte("{\"format\":\"new\"}\n"), 0o600)
	later := time.Now().Add(time.Minute)
	_ = os.Chtimes(c.Path, later, later)
	sums = x.Build(h, agentlogtest.Folder)
	if sums[0].Path != c.Path || !strings.HasPrefix(sums[0].Err, "cannot read this session") {
		t.Errorf("changed file: %+v", sums[0])
	}
	// Another project has none of them.
	if got := x.Build(h, "/work/other"); len(got) != 0 {
		t.Errorf("other project: %+v", got)
	}
}

// Spec (AGX-9): the history index of 500 sessions (fixture copies, both
// CLIs) is built cold within agent_history_index_500.
func TestPerfAgentHistoryIndex500(t *testing.T) {
	perfbudget.Require(t)
	home := agentlogtest.FakeHome(t)
	h := agentlog.HomesFrom(home, func(string) string { return "" })
	cl, _ := filepath.Glob(filepath.Join(h.Claude, "projects", "-work-demo", "*.jsonl"))
	cx, _ := filepath.Glob(filepath.Join(h.Codex, "sessions", "2026", "11", "05", "*.jsonl"))
	cb, _ := os.ReadFile(cl[0])
	xb, _ := os.ReadFile(cx[0])
	for i := 0; i < 249; i++ {
		_ = os.WriteFile(filepath.Join(filepath.Dir(cl[0]), "s"+itoa(i)+".jsonl"), cb, 0o600)
		_ = os.WriteFile(filepath.Join(filepath.Dir(cx[0]), "rollout-"+itoa(i)+".jsonl"), xb, 0o600)
	}
	var ds []time.Duration
	for i := 0; i < 5; i++ {
		start := time.Now()
		n := len(agentlog.NewIndex().Build(h, agentlogtest.Folder))
		ds = append(ds, time.Since(start))
		if n != 500 {
			t.Fatalf("%d sessions", n)
		}
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	perfbudget.Check(t, "agent_history_index_500", float64(ds[len(ds)/2].Microseconds())/1000)
}
