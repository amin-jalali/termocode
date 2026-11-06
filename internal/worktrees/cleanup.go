package worktrees

import (
	"strconv"
	"time"
)

// Bulk cleanup rules (WTX-15): which worktrees the cleanup list ticks
// before the user looks. A worktree is pre-ticked when its branch is
// merged into the default branch, its linked PR is merged, or nothing
// happened in it for CleanupIdleDays — and only when it is clean: a
// worktree with changed files is never pre-ticked (archiving it loses
// them; the user ticks it and answers its own card).

// CleanupIdleDays: no activity for this long counts as done.
const CleanupIdleDays = 14

// CleanupFacts is what the rules look at.
type CleanupFacts struct {
	Base       string // the default branch ("main")
	Merged     bool   // the branch has no commit Base lacks
	PR         int
	PRMerged   bool
	Dirty      int       // changed files
	LastActive time.Time // the newest of its last commit, last seen and made
}

// CleanupReason says why a worktree can go (or why it is kept) and
// whether it is pre-ticked.
func CleanupReason(f CleanupFacts, now time.Time) (why string, tick bool) {
	days := 0
	if !f.LastActive.IsZero() {
		days = int(now.Sub(f.LastActive) / (24 * time.Hour))
	}
	switch {
	case f.Merged:
		why = "merged into " + f.Base
	case f.PRMerged:
		why = "PR #" + strconv.Itoa(f.PR) + " merged"
	case f.Dirty == 0 && !f.LastActive.IsZero() && days >= CleanupIdleDays:
		why = "no activity for " + strconv.Itoa(days) + " days"
	}
	if f.Dirty > 0 {
		files := plural(f.Dirty, "changed file")
		if why == "" {
			return "kept: " + files, false
		}
		return why + " · " + files, false
	}
	if why != "" {
		return why, true
	}
	switch {
	case f.LastActive.IsZero():
		return "kept: in use", false
	case days == 0:
		return "kept: active today", false
	}
	return "kept: active " + plural(days, "day") + " ago", false
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}
