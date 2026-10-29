package app

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

// bigGitRepo makes a repo with 20 000 committed files (200 folders of
// 100) and 2 000 changes: 1 500 modified, 500 deleted, 200 new files, and
// the changes of 10 folders staged.
func bigGitRepo(tb testing.TB) string {
	tb.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		tb.Skip("no git")
	}
	dir := tb.TempDir()
	run := func(args ...string) {
		tb.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			tb.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@e.x")
	run("config", "user.name", "T")
	run("config", "commit.gpgsign", "false")
	path := func(d, f int) string { return filepath.Join(dir, fmt.Sprintf("pkg%03d", d), fmt.Sprintf("file%03d.go", f)) }
	for d := 0; d < 200; d++ {
		if err := os.MkdirAll(filepath.Dir(path(d, 0)), 0o755); err != nil {
			tb.Fatal(err)
		}
		for f := 0; f < 100; f++ {
			if err := os.WriteFile(path(d, f), []byte(fmt.Sprintf("package p\n\nfunc F%d() int { return %d }\n", f, f)), 0o644); err != nil {
				tb.Fatal(err)
			}
		}
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")
	i := 0
	for d := 0; d < 200; d++ {
		for f := 0; f < 100; f += 10 {
			if i%4 == 3 {
				_ = os.Remove(path(d, f))
			} else {
				_ = os.WriteFile(path(d, f), []byte("package p\n// changed\n"), 0o644)
			}
			i++
		}
		_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("pkg%03d", d), "new.go"), []byte("package p\n"), 0o644)
	}
	run("add", "pkg00*")
	return dir
}

// The Source Control refresh after a git action (fetchGitCmd: branch,
// changed files, graph) on a 20 000-file repo with 2 000 changes: the
// median of five runs after one warm-up.
func TestPerfGitRefresh20k(t *testing.T) {
	perfbudget.Require(t)
	t.Chdir(bigGitRepo(t))
	refresh := func() time.Duration {
		t0 := time.Now()
		msg := fetchGitCmd()().(GitMsg)
		d := time.Since(t0)
		if !msg.IsRepo || len(msg.Files) != 2200 || len(msg.Graph) != 1 {
			t.Fatalf("snapshot: repo %v, %d files, %d commits", msg.IsRepo, len(msg.Files), len(msg.Graph))
		}
		return d
	}
	refresh()
	var took []time.Duration
	for i := 0; i < 5; i++ {
		took = append(took, refresh())
	}
	slices.Sort(took)
	t.Logf("git refresh: %v", took)
	perfbudget.Check(t, "git_refresh_20k", float64(took[2].Microseconds()/100)/10)
}
