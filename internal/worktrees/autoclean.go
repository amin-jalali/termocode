package worktrees

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// Auto clean-up rules (CMP-16): with worktree_auto_archive_days or
// worktree_max set, reading the worktree list archives worktrees by
// itself. Only clean ones (no changed files), never pinned, never one an
// agent works in, never one used in the last AutoMinIdle. Archive keeps
// the branch, so x restores any of them in one step.

// AutoMinIdle: a worktree used this recently is never archived by itself,
// not even over the cap.
const AutoMinIdle = 24 * time.Hour

// AutoCand is a worktree the auto clean-up may archive.
type AutoCand struct {
	Path       string
	Pinned     bool
	Busy       bool      // an agent works in it
	Dirty      int       // changed files; < 0: unknown
	LastActive time.Time // the newest of its last commit, last seen and made
}

// AutoPick is a worktree to archive and why.
type AutoPick struct {
	Path, Why string
}

// AutoPicks are the worktrees to archive: first the ones idle for days
// (0 = off), then, while more than max (0 = off) of the live worktrees
// would stay, the oldest of the rest. live counts every live worktree
// but the main checkout, also the ones not in cands.
func AutoPicks(cands []AutoCand, live, days, max int, now time.Time) []AutoPick {
	var ok []AutoCand
	for _, c := range cands {
		if c.Pinned || c.Busy || c.Dirty != 0 || c.LastActive.IsZero() || now.Sub(c.LastActive) < AutoMinIdle {
			continue
		}
		ok = append(ok, c)
	}
	var picks []AutoPick
	taken := map[string]bool{}
	if days > 0 {
		for _, c := range ok {
			if d := int(now.Sub(c.LastActive) / (24 * time.Hour)); d >= days {
				picks = append(picks, AutoPick{Path: c.Path, Why: "no activity for " + strconv.Itoa(d) + " days"})
				taken[c.Path] = true
			}
		}
	}
	if max > 0 && live-len(picks) > max {
		rest := make([]AutoCand, 0, len(ok))
		for _, c := range ok {
			if !taken[c.Path] {
				rest = append(rest, c)
			}
		}
		sort.SliceStable(rest, func(i, j int) bool { return rest[i].LastActive.Before(rest[j].LastActive) })
		why := "over the limit of " + plural(max, "worktree") + " (oldest first)"
		for _, c := range rest {
			if live-len(picks) <= max {
				break
			}
			picks = append(picks, AutoPick{Path: c.Path, Why: why})
		}
	}
	return picks
}

// autoSizeFiles bounds DirSize's walk on huge folders.
const autoSizeFiles = 500_000

// DirSize is the size of the files under dir (what archiving it frees);
// links are not followed. A folder that cannot be read counts 0.
func DirSize(dir string) int64 {
	var n int64
	files := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		files++
		if files > autoSizeFiles {
			return filepath.SkipAll
		}
		if info, err := d.Info(); err == nil {
			n += info.Size()
		}
		return nil
	})
	return n
}
