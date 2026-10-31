package app

import (
	"fmt"
	"io/fs"
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
//
// Every file gets an mtime an hour back, like a real work tree that is
// older than its index. Written "now", the files share a second with
// the index, git treats them as racily clean and each status reads all
// 20 000 again (this status takes no index lock, so it never settles
// that); whether the setup hit that second was luck, and the guard
// measured 17 or 70 ms on the same code.
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
	old := time.Now().Add(-time.Hour)
	write := func(p string, data []byte) {
		tb.Helper()
		if err := os.WriteFile(p, data, 0o644); err != nil {
			tb.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			tb.Fatal(err)
		}
	}
	path := func(d, f int) string {
		return filepath.Join(dir, fmt.Sprintf("pkg%03d", d), fmt.Sprintf("file%03d.go", f))
	}
	for d := 0; d < 200; d++ {
		if err := os.MkdirAll(filepath.Dir(path(d, 0)), 0o755); err != nil {
			tb.Fatal(err)
		}
		for f := 0; f < 100; f++ {
			write(path(d, f), []byte(fmt.Sprintf("package p\n\nfunc F%d() int { return %d }\n", f, f)))
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
				write(path(d, f), []byte("package p\n// changed\n"))
			}
			i++
		}
		write(filepath.Join(dir, fmt.Sprintf("pkg%03d", d), "new.go"), []byte("package p\n"))
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

// FIX-40: the background poll (status --porcelain=v2 --branch, no index
// lock) on the 20 000-file repo after every file was touched (a build
// tool, a checkout there and back): the text is the same but the times
// are not, and a status that never writes the index reads all 20 000
// files again on every poll. The poll's index refresh settles them: the
// median of five polls after the first.
func TestPerfGitPollTouched20k(t *testing.T) {
	perfbudget.Require(t)
	dir := bigGitRepo(t)
	t.Chdir(dir)
	// The touch is two seconds before the poll's refresh: git compares
	// whole seconds, and a file touched in the second the index is
	// written stays "racily clean" until a later refresh.
	touched := time.Now().Add(-2 * time.Second)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && d.Name() == ".git" {
				return filepath.SkipDir
			}
			return err
		}
		return os.Chtimes(p, touched, touched)
	})
	if err != nil {
		t.Fatal(err)
	}
	m := &Model{gitGate: &gitPollGate{}}
	m.applyGitSnapshot(fetchGitCmd()().(GitMsg))
	poll := func() time.Duration {
		t0 := time.Now()
		msg := m.pollGitCmd()().(GitMsg)
		d := time.Since(t0)
		if !msg.IsRepo || len(msg.Files) != 2200 {
			t.Fatalf("snapshot: repo %v, %d files", msg.IsRepo, len(msg.Files))
		}
		m.applyGitSnapshot(msg)
		return d
	}
	poll() // the first poll refreshes the index
	var took []time.Duration
	for i := 0; i < 5; i++ {
		took = append(took, poll())
	}
	slices.Sort(took)
	t.Logf("git poll, touched files: %v", took)
	perfbudget.Check(t, "git_poll_touched_20k", float64(took[2].Microseconds()/100)/10)
}
