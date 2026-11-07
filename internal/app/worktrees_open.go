package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── WORKTREES view: open and add as a folder (WTX-3) ─────────────────────

// wtOpenFolder opens a worktree the File: Open Recent Workspace way (the
// process re-execs into the folder; a session server keeps the old
// session); wtAddFolder adds it as a folder of this window. Tests swap
// them.
var (
	wtOpenFolder = func(m *Model, path string) tea.Cmd { return m.openWorkspace(path) }
	wtAddFolder  = func(m *Model, path string) tea.Cmd { return m.addRootFromPrompt(path) }
)

// wtSelected is the cursor's card (ok: there is one).
func (m *Model) wtSelected() (wtItem, bool) {
	st := m.wt
	if st == nil {
		return wtItem{}, false
	}
	items, rows := m.wtList()
	cur := st.cursorIndex(items, rows)
	if cur < 0 {
		return wtItem{}, false
	}
	return items[rows[cur].item], true
}

// wtItemByPath is the card of the worktree at path, whatever the
// filters show.
func (m *Model) wtItemByPath(path string) (wtItem, bool) {
	st := m.wt
	if st == nil {
		return wtItem{}, false
	}
	for _, it := range wtItems(st.entries, m.wtTabs(), st.others, st.root) {
		if it.e.Path == path {
			return it, true
		}
	}
	return wtItem{}, false
}

// wtUsable says why a worktree cannot be opened or added ("" = it can).
func wtUsable(it wtItem) string {
	if it.e.Gone {
		return "Its folder is gone: " + wtShortPath(it.e.Path)
	}
	return ""
}

// worktreeOpen opens the cursor's worktree (Enter, worktree-open).
func (m *Model) worktreeOpen() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	if why := wtUsable(it); why != "" {
		return m.pushToastDetail(toast.Info, "This worktree cannot be opened", why)
	}
	if it.here {
		return m.pushToastDetail(toast.Info, "You are in this worktree", wtShortPath(it.e.Path))
	}
	m.wtTouchMeta(it.e.Path, func(md *worktrees.Meta) { md.LastSeen = clock.Now() })
	return wtOpenFolder(m, it.e.Path)
}

// worktreeAddFolder adds the cursor's worktree as a folder (a,
// worktree-add-folder).
func (m *Model) worktreeAddFolder() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	if why := wtUsable(it); why != "" {
		return m.pushToastDetail(toast.Info, "This worktree cannot be added", why)
	}
	return wtAddFolder(m, it.e.Path)
}

// wtTouchMeta changes one worktree's metadata in worktrees.json, then in
// the list; a demo writes nothing to disk. It returns the write error
// (the list is left as it was then).
func (m *Model) wtTouchMeta(path string, fn func(*worktrees.Meta)) error {
	st := m.wt
	if !st.off && st.common != "" {
		err := worktrees.Update(st.common, func(all map[string]worktrees.Meta) {
			md := all[path]
			fn(&md)
			all[path] = md
		})
		if err != nil {
			recordError("[worktrees] " + err.Error())
			return err
		}
	}
	for i := range st.entries {
		if st.entries[i].Path == path {
			fn(&st.entries[i].Meta)
		}
	}
	st.touch()
	return nil
}

// worktreePin pins or unpins the cursor's worktree (p, worktree-pin).
func (m *Model) worktreePin() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	pin := !it.e.Meta.Pinned
	if err := m.wtTouchMeta(it.e.Path, func(md *worktrees.Meta) { md.Pinned, md.Order = pin, 0 }); err != nil {
		return m.pushToastDetail(toast.Warn, "The pin could not be saved", err.Error())
	}
	m.wt.cursorPath = it.e.Path
	return nil
}
