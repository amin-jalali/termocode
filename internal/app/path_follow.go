package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// After a file or folder is renamed or deleted (by the agent or in the
// explorer) nothing may point at the old path: the closed-tabs list
// (Reopen Closed Editor), the recent files (recents.json, Welcome and
// Quick Open) and the hot-exit backups of an earlier run (keyed by path)
// follow a rename and forget a delete. Open tabs follow in
// renameOpenBuffers.

// movedPath maps p to its new path when p is root or lies under it.
func movedPath(p, root, to string) (string, bool) {
	if p == root {
		return to, true
	}
	if strings.HasPrefix(p, root+string(filepath.Separator)) {
		return to + p[len(root):], true
	}
	return "", false
}

// underAny reports whether p is one of roots or lies under one.
func underAny(p string, roots []string) bool {
	for _, r := range roots {
		if _, ok := movedPath(p, r, r); ok {
			return true
		}
	}
	return false
}

// followRename makes everything that remembers from point at to. Call it
// after the rename on disk.
func (m *Model) followRename(from, to string) {
	m.renameOpenBuffers(from, to)
	for i, p := range m.closedRing {
		if n, ok := movedPath(p, from, to); ok {
			m.closedRing[i] = n
		}
	}
	editRecents(func(p string) (string, bool) {
		if n, ok := movedPath(p, from, to); ok {
			return n, true
		}
		return p, true
	})
	m.editHotBackups(func(p string) (string, bool) {
		if n, ok := movedPath(p, from, to); ok {
			return n, true
		}
		return p, true
	})
	m.debugFollowRename(from, to) // breakpoints (K7)
}

// forgetPaths drops what remembers the deleted paths (or files under
// them). Call it after the delete on disk.
func (m *Model) forgetPaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	ring := m.closedRing[:0]
	for _, p := range m.closedRing {
		if !underAny(p, paths) {
			ring = append(ring, p)
		}
	}
	m.closedRing = ring
	keep := func(p string) (string, bool) { return p, !underAny(p, paths) }
	editRecents(keep)
	m.editHotBackups(keep)
	m.debugForgetPaths(paths) // breakpoints (K7)
}

// editRecents rewrites the recent files with f (false drops the entry)
// and saves them when something changed.
func editRecents(f func(string) (string, bool)) {
	entries := loadRecents()
	out := make([]RecentEntry, 0, len(entries))
	seen := map[string]bool{}
	changed := false
	for _, e := range entries {
		p, ok := f(e.Path)
		if !ok || seen[p] {
			changed = true
			continue
		}
		if p != e.Path {
			changed = true
			e.Path = p
		}
		seen[p] = true
		out = append(out, e)
	}
	if changed {
		saveRecents(out)
	}
}

// hotBackupName is the file name hotExitLua gives the backup of path.
func hotBackupName(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:])[:16] + ".bak"
}

// editHotBackups rewrites the backups of an earlier run that wait for
// Restore / Discard (previous/) with f: a moved file keeps its text under
// the new path (and the new path's file name, so a later backup of it
// replaces this one), a dropped one is removed. The live backup of this
// run is written again at once (it follows the buffers by itself, but
// only after a short delay).
func (m *Model) editHotBackups(f func(string) (string, bool)) {
	if m.nvim != nil && m.hotExitLive != "" {
		if err := m.nvim.ExecLua(hotExitFlushLua); err != nil {
			recordError("hot exit: " + err.Error())
		}
	}
	m.hotExitPending = editHotList(m.hotExitPending, f)
	m.hotExitWaiting = editHotList(m.hotExitWaiting, f)
	if m.hotExitRoot == "" {
		return
	}
	prev := filepath.Join(m.hotExitRoot, "previous")
	files, err := os.ReadDir(prev)
	if err != nil {
		return
	}
	for _, fe := range files {
		if !strings.HasSuffix(fe.Name(), ".bak") {
			continue
		}
		name := filepath.Join(prev, fe.Name())
		b, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		hb, ok := parseHotBackup(b)
		if !ok {
			continue
		}
		p, keep := f(hb.Path)
		switch {
		case !keep:
			_ = os.Remove(name)
		case p != hb.Path:
			body := p + "\n" + strings.Join(hb.Lines, "\n")
			dst := filepath.Join(prev, hotBackupName(p))
			if os.WriteFile(dst, []byte(body), 0o600) == nil && dst != name {
				_ = os.Remove(name)
			}
		}
	}
}

// editHotList applies f to backups held in memory (the Restore question
// on screen or waiting for its turn).
func editHotList(list []hotBackup, f func(string) (string, bool)) []hotBackup {
	out := list[:0]
	for _, b := range list {
		if p, ok := f(b.Path); ok {
			b.Path = p
			out = append(out, b)
		}
	}
	return out
}
