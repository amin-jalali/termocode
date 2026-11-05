package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/picker"
)

// ── WORKTREES view: keys and palette (WTX-2) ─────────────────────────────

// worktreesGuidePage is the guide section of the view.
const worktreesGuidePage = "guides/git.md"

func helpLocWorktrees() help.Loc { return help.Loc{Page: worktreesGuidePage, Anchor: "worktrees"} }

// worktreeKeyRows are the rows of the ? card (the guide's table).
func worktreeKeyRows() []confirm.KeyRow {
	return keyRows(
		[]string{"Move"},
		[]string{"↑ ↓ j k", "move · g G top, end"},
		[]string{"Worktree"},
		[]string{"Enter", "open it in this window (the old session keeps running)"},
		[]string{"a", "add it as a folder of this window"},
		[]string{"p", "pin or unpin"},
		[]string{"List"},
		[]string{"f", "filter: All, Mine, Agents, Dirty, Asleep, External"},
		[]string{"/", "filter by name · Esc clears"},
		[]string{"R", "read every worktree again"},
		[]string{"Anywhere"},
		[]string{"Alt+Shift+W", "show this view"},
		[]string{"Ctrl+Shift+J", "jump to a worktree or its agent"},
	)
}

// worktreePaletteItems are the "Worktree: …" palette rows.
func worktreePaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "worktree-view", Title: "Worktree: Show"},
		{ID: "worktree-open", Title: "Worktree: Open Selected"},
		{ID: "worktree-add-folder", Title: "Worktree: Add Selected as Folder"},
	}
}

// dispatchWorktreePalette runs the "Worktree: …" palette commands.
func (m *Model) dispatchWorktreePalette(id string) (tea.Cmd, bool) {
	switch id {
	case "worktree-view":
		return m.revealWorktrees(), true
	case "worktree-open":
		return m.worktreeOpen(), true
	case "worktree-add-folder":
		return m.worktreeAddFolder(), true
	}
	return nil, false
}

// handleWTSidebarKey is a key while the WORKTREES view has the focus.
func (m Model) handleWTSidebarKey(k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.wt
	if st.detail {
		switch k.String() {
		case "esc", "left", "h":
			st.detail = false
			st.touch()
			return m, nil
		case "enter":
			return m, m.worktreeOpen()
		}
	}
	items, rows := m.wtList()
	cur := st.cursorIndex(items, rows)
	switch k.String() {
	case "up", "k":
		m.wtMove(items, rows, cur, -1)
	case "down", "j":
		m.wtMove(items, rows, cur, 1)
	case "pgup":
		m.wtMove(items, rows, cur, -5)
	case "pgdown":
		m.wtMove(items, rows, cur, 5)
	case "home", "g":
		m.wtMove(items, rows, cur, -len(rows))
	case "end", "G":
		m.wtMove(items, rows, cur, len(rows))
	case "enter", "l":
		if wtCompact(m.explorerWidth-1, m.h) {
			if cur >= 0 {
				st.detail = true
				st.touch()
			}
			return m, nil
		}
		return m, m.worktreeOpen()
	case "a":
		return m, m.worktreeAddFolder()
	case "R":
		return m, m.worktreesReload()
	case "?":
		return m, m.openKeyCard("Worktree keys", worktreeKeyRows(), helpLocWorktrees(), nil)
	case "esc":
		m.focus = FocusEditor
	}
	return m, nil
}

// wtMove moves the cursor d cards down (up when negative), stopping at
// the ends.
func (m *Model) wtMove(items []wtItem, rows []wtRow, cur, d int) {
	st := m.wt
	var cards []int
	at := 0
	for i, r := range rows {
		if r.item >= 0 {
			if i == cur {
				at = len(cards)
			}
			cards = append(cards, i)
		}
	}
	if len(cards) == 0 {
		return
	}
	at = min(max(at+d, 0), len(cards)-1)
	st.cursorPath = items[rows[cards[at]].item].e.Path
	st.detail = false
	st.touch()
}
