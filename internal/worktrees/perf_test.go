package worktrees

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/perfbudget"
)

// porcelain200 is `git worktree list --porcelain` of a repository with
// 200 worktrees (some locked, some agent lanes).
func porcelain200() string {
	var b strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "worktree /src/app%s\nHEAD %040d\n", map[bool]string{true: "", false: fmt.Sprintf(".worktrees/w%03d", i)}[i == 0], i)
		switch {
		case i%10 == 5:
			fmt.Fprintf(&b, "branch refs/heads/termocode/agent-r%d\n", i)
		case i%17 == 3:
			b.WriteString("detached\nlocked on a drive\n")
		default:
			fmt.Fprintf(&b, "branch refs/heads/feat/w%03d\n", i)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func medianOf(n int, f func()) time.Duration {
	var ds []time.Duration
	for i := 0; i < n; i++ {
		t0 := time.Now()
		f()
		ds = append(ds, time.Since(t0))
	}
	slices.Sort(ds)
	return ds[len(ds)/2]
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()/100) / 10 }

// Perf guard (WTX-1): parsing 200 porcelain blocks and building the model
// with metadata for half of them.
func TestPerfWorktreesList200(t *testing.T) {
	perfbudget.Require(t)
	out := porcelain200()
	metas := map[string]Meta{}
	for i := 2; i < 200; i += 2 {
		metas[fmt.Sprintf("/src/app.worktrees/w%03d", i)] = Meta{Name: fmt.Sprintf("w%d", i), Pinned: i%20 == 0}
	}
	var es []Entry
	d := medianOf(9, func() { es = Build(git.ParseWorktreeList(out, "/src/app/.git"), metas) })
	if len(es) != 200 {
		t.Fatalf("%d entries", len(es))
	}
	perfbudget.Check(t, "worktrees_list_200", ms(d))
}

// Perf guard (WTX-1): the UI thread's share of a status refresh of 200
// worktrees (start the batch, read every cached value); the git calls run
// in the workers.
func TestPerfWorktreesStatus200(t *testing.T) {
	perfbudget.Require(t)
	c := NewStatusCache(func(string) (git.WorktreeStatus, error) {
		time.Sleep(time.Millisecond) // a git call
		return git.WorktreeStatus{Dirty: 2}, nil
	})
	var paths []string
	for i := 0; i < 200; i++ {
		paths = append(paths, fmt.Sprintf("/src/app.worktrees/w%03d", i))
	}
	var ds []time.Duration
	for i := 0; i < 9; i++ {
		for _, p := range paths {
			c.Invalidate(p)
		}
		done := make(chan struct{})
		t0 := time.Now()
		if !c.Refresh(paths, func() { close(done) }) {
			t.Fatal("nothing started")
		}
		for _, p := range paths {
			c.Get(p)
		}
		ds = append(ds, time.Since(t0))
		<-done // the workers' time is not the UI thread's
	}
	slices.Sort(ds)
	d := ds[len(ds)/2]
	perfbudget.Check(t, "worktrees_status_200", ms(d))
}
