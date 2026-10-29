package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/perfbudget"
)

// bigAgentRepo is a committed repo of n small files in 50 folders.
func bigAgentRepo(tb testing.TB, n int) string {
	tb.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		tb.Skip("git not available")
	}
	dir := tb.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			tb.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	run("config", "commit.gpgsign", "false")
	run("checkout", "-q", "-b", "main")
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, fmt.Sprintf("d%02d", i%50), fmt.Sprintf("f%04d.txt", i))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(fmt.Sprintf("file %d\nline two\n", i)), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	return dir
}

// agentCycle creates a worktree, lets the "agent" change k files, and
// merges them back. It returns the create and the merge time.
func agentCycle(tb testing.TB, dir, id string, k int) (create, merge time.Duration) {
	tb.Helper()
	t0 := time.Now()
	w, err := CreateAgentWorktree(dir, id)
	if err != nil {
		tb.Fatal(err)
	}
	create = time.Since(t0)
	for i := 0; i < k; i++ {
		p := filepath.Join(w.Path, fmt.Sprintf("d%02d", i%50), fmt.Sprintf("f%04d.txt", i))
		if err := os.WriteFile(p, []byte(fmt.Sprintf("file %d\nchanged by %s\n", i, id)), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
	t1 := time.Now()
	res, err := MergeAgentWorktree(dir, id, false)
	if err != nil {
		tb.Fatal(err)
	}
	merge = time.Since(t1)
	if len(res.Applied) != k || len(res.Conflicts) != 0 {
		tb.Fatalf("merge: %+v", res)
	}
	if err := RemoveAgentWorktree(dir, id, true); err != nil {
		tb.Fatal(err)
	}
	// The next cycle starts from a clean tree again.
	cmd := exec.Command("git", "checkout", "-q", "--", ".")
	cmd.Dir = dir
	_ = cmd.Run()
	return create, merge
}

func median(ds []time.Duration) time.Duration {
	s := slices.Clone(ds)
	slices.Sort(s)
	return s[len(s)/2]
}

// Perf guard (PERF-7): an agent worktree of a 5 000-file repo, created
// and its 20 changed files merged back.
func TestPerfAgentWorktree5k(t *testing.T) {
	perfbudget.Require(t)
	dir := bigAgentRepo(t, 5000)
	var cs, ms []time.Duration
	for i := 0; i < 5; i++ {
		c, m := agentCycle(t, dir, fmt.Sprintf("perf%d", i), 20)
		cs, ms = append(cs, c), append(ms, m)
	}
	t.Logf("create %v, merge %v (all: %v / %v)", median(cs), median(ms), cs, ms)
	perfbudget.Check(t, "agent_worktree_create_5k", float64(median(cs).Microseconds()/100)/10)
	perfbudget.Check(t, "agent_worktree_merge_20", float64(median(ms).Microseconds()/100)/10)
}

func BenchmarkAgentWorktreeMerge20(b *testing.B) {
	dir := bigAgentRepo(b, 5000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		agentCycle(b, dir, fmt.Sprintf("b%d", i), 20)
	}
}
