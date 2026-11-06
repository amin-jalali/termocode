package git

import (
	"os"
	"strings"
	"sync"
	"time"
)

// The STASHES section of the Source Control panel (GITX-10) is read on
// every git poll. StashListCached keeps the last list per repository and
// runs `git stash list` again only when the stash reflog file changed.

type stashCacheEntry struct {
	path    string // the stash reflog (absolute)
	mod     time.Time
	size    int64
	entries []StashEntry
}

var stashCache sync.Map // dir → *stashCacheEntry

// StashListCached is StashList, reused while the stash reflog is the
// same. No stash, an error or not a repository: nil.
func StashListCached(dir string) []StashEntry {
	var c *stashCacheEntry
	if v, ok := stashCache.Load(dir); ok {
		c = v.(*stashCacheEntry)
	} else {
		out, err := runOut(dir, "git", "rev-parse", "--path-format=absolute", "--git-path", "logs/refs/stash")
		if err != nil {
			return nil
		}
		c = &stashCacheEntry{path: strings.TrimSpace(out)}
		stashCache.Store(dir, c)
	}
	st, err := os.Stat(c.path)
	if err != nil || st.Size() == 0 {
		return nil
	}
	cur := *c
	if cur.entries != nil && cur.mod.Equal(st.ModTime()) && cur.size == st.Size() {
		return cur.entries
	}
	entries, err := StashList(dir)
	if err != nil {
		return nil
	}
	if entries == nil {
		entries = []StashEntry{}
	}
	stashCache.Store(dir, &stashCacheEntry{path: c.path, mod: st.ModTime(), size: st.Size(), entries: entries})
	return entries
}
