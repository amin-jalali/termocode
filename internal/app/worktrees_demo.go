package app

import (
	"sync"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// wtCannedCache is a status cache filled from a table, without git (the
// demo and tests): every value is in when it returns.
func wtCannedCache(vals map[string]git.WorktreeStatus) *worktrees.StatusCache {
	c := worktrees.NewStatusCache(func(p string) (git.WorktreeStatus, error) { return vals[p], nil })
	paths := make([]string, 0, len(vals))
	for p := range vals {
		paths = append(paths, p)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	if c.Refresh(paths, wg.Done) {
		wg.Wait()
	}
	return c
}
