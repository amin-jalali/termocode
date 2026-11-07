package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── WORKTREES view: the user's own order (CMP-27) ────────────────────────
//
// Shift+K / Shift+J (or Shift+↑ / Shift+↓) move the selected card one
// place up or down among its siblings in its group. The whole group is
// then written as Order 1..n in worktrees.json, so the order stays after
// a restart and in every window. Cards with no order (new ones) follow
// the ordered ones in the automatic sort. A pin change drops the card's
// order; Worktree: Reset Order drops every order.

const (
	wtMoveUpPaletteID     = "worktree-move-up"
	wtMoveDownPaletteID   = "worktree-move-down"
	wtResetOrderPaletteID = "worktree-reset-order"
)

func init() {
	// Only the list order changes; Reset Order undoes it.
	aiActionPolicies[wtMoveUpPaletteID] = aiAuto()
	aiActionPolicies[wtMoveDownPaletteID] = aiAuto()
	aiActionPolicies[wtResetOrderPaletteID] = aiAuto()
}

// worktreeReorder moves the cursor's card d places (-1 up, 1 down)
// among its siblings (same group, same parent row).
func (m *Model) worktreeReorder(d int) tea.Cmd {
	st := m.wt
	if st == nil || !st.loaded {
		return m.revealWorktrees()
	}
	items, rows := m.wtList()
	cur := st.cursorIndex(items, rows)
	if cur < 0 {
		return m.revealWorktrees()
	}
	parent := wtRowParent(rows, cur)
	var sibs []int // row indexes
	at := -1
	for i, r := range rows {
		if r.item < 0 || r.group != rows[cur].group || r.depth != rows[cur].depth || wtRowParent(rows, i) != parent {
			continue
		}
		if i == cur {
			at = len(sibs)
		}
		sibs = append(sibs, i)
	}
	to := at + d
	if at < 0 || to < 0 || to >= len(sibs) {
		return nil // at the end already
	}
	a, b := items[rows[cur].item].e.Path, items[rows[sibs[to]].item].e.Path
	// The group in the list's order (hidden cards too), a and b swapped.
	var group []string
	for _, it := range items {
		if it.group != rows[cur].group {
			continue
		}
		switch it.e.Path {
		case a:
			group = append(group, b)
		case b:
			group = append(group, a)
		default:
			group = append(group, it.e.Path)
		}
	}
	order := make(map[string]int, len(group))
	for i, p := range group {
		order[p] = i + 1
	}
	if err := m.wtSetOrder(func(path string, md *worktrees.Meta) {
		if n, ok := order[path]; ok {
			md.Order = n
		}
	}); err != nil {
		return m.pushToastDetail(toast.Warn, "The order could not be saved", err.Error())
	}
	st.cursorPath = a
	st.touch()
	return nil
}

// worktreeResetOrder drops every stored order: the groups sort by
// themselves again.
func (m *Model) worktreeResetOrder() tea.Cmd {
	st := m.wt
	if st == nil || !st.loaded {
		return m.revealWorktrees()
	}
	if err := m.wtSetOrder(func(_ string, md *worktrees.Meta) { md.Order = 0 }); err != nil {
		return m.pushToastDetail(toast.Warn, "The order could not be reset", err.Error())
	}
	return m.pushToast(toast.Info, "Worktrees sort by themselves again")
}

// wtRowParent is the row index of row i's parent card (-1 for a root).
func wtRowParent(rows []wtRow, i int) int {
	d := rows[i].depth
	if d <= 0 {
		return -1
	}
	for j := i - 1; j >= 0; j-- {
		if rows[j].item >= 0 && rows[j].group == rows[i].group && rows[j].depth == d-1 {
			return j
		}
	}
	return -1
}

// wtSetOrder changes the metadata of every worktree of the list in one
// write of worktrees.json, then in the list; a demo writes nothing.
func (m *Model) wtSetOrder(fn func(path string, md *worktrees.Meta)) error {
	st := m.wt
	if !st.off && st.common != "" {
		err := worktrees.Update(st.common, func(all map[string]worktrees.Meta) {
			for _, e := range st.entries {
				md := all[e.Path]
				fn(e.Path, &md)
				all[e.Path] = md
			}
		})
		if err != nil {
			recordError("[worktrees] " + err.Error())
			return err
		}
	}
	for i := range st.entries {
		fn(st.entries[i].Path, &st.entries[i].Meta)
	}
	st.touch()
	return nil
}
