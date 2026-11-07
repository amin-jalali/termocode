package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/picker"
)

// ── WORKTREES view: keys and palette (WTX-2) ─────────────────────────────

// worktreesGuidePage is the guide section of the view.
const worktreesGuidePage = "guides/worktrees.md"

func helpLocWorktrees() help.Loc { return help.Loc{Page: worktreesGuidePage, Anchor: "worktrees"} }

const wtChildPaletteID = "worktree-new-child"

func init() {
	aiActionPolicies[wtChildPaletteID] = aiNever(aiNoPicker)
}

// worktreeKeyRows are the rows of the ? card (the guide's table).
func worktreeKeyRows() []confirm.KeyRow {
	return keyRows(
		[]string{"Move"},
		[]string{"↑ ↓ j k", "move · g G top, end"},
		[]string{"Worktree"},
		[]string{"Enter", "open it in this window (the old session keeps running)"},
		[]string{"a", "add it as a folder of this window"},
		[]string{"p", "pin or unpin"},
		[]string{"i", "link an issue or pull request (its state and CI show on the card)"},
		[]string{"P", "the linked pull request: checks and their logs, review threads, merge"},
		[]string{"N", "the notes agents left on it, with their times"},
		[]string{"n", "new worktree: name, start point, agent and first task"},
		[]string{"c", "new child worktree: from this one's branch, listed under it"},
		[]string{"L", "show the setup of a new worktree (its terminal tab)"},
		[]string{"A", "adopt a worktree made outside termocode (keeps its notes)"},
		[]string{"F", "forget a worktree whose folder is gone"},
		[]string{"x", "archive: remove the folder, keep the branch · on an archived row: restore"},
		[]string{"D", "delete the worktree and its branch (asks, step by step)"},
		[]string{"C", "clean up: tick merged and old worktrees, archive them together"},
		[]string{"s", "sleep: end its agent tabs, move it under ASLEEP · again: wake it"},
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
		{ID: "worktree-jump", Title: "Worktree: Jump…"},
		{ID: "worktree-pin", Title: "Worktree: Pin or Unpin Selected"},
		{ID: "worktree-filter", Title: "Worktree: Next Filter"},
		{ID: wtNewPaletteID, Title: "Worktree: New…"},
		{ID: wtChildPaletteID, Title: "Worktree: New Child of Selected…"},
		{ID: wtSetupLogPaletteID, Title: "Worktree: Show Setup Log"},
		{ID: wtLinkPaletteID, Title: "Worktree: Link Issue or PR…"},
		{ID: wtPRPaletteID, Title: "Worktree: Show Pull Request of Selected"},
		{ID: wtNotesPaletteID, Title: "Worktree: Show Notes of Selected"},
		{ID: wtAdoptPaletteID, Title: "Worktree: Adopt Selected"},
		{ID: wtForgetPaletteID, Title: "Worktree: Forget Gone Selected"},
		{ID: wtSleepPaletteID, Title: "Worktree: Sleep or Wake Selected"},
		{ID: wtArchivePaletteID, Title: "Worktree: Archive or Restore Selected…"},
		{ID: wtDeletePaletteID, Title: "Worktree: Delete Selected…"},
		{ID: wtCleanupPaletteID, Title: "Worktree: Clean Up…"},
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
	case "worktree-jump":
		return m.worktreeJump(), true
	case "worktree-pin":
		return m.worktreePin(), true
	case "worktree-filter":
		m.worktreeNextFilter()
		return m.revealWorktrees(), true
	case wtNewPaletteID:
		return m.openWorktreeNew(), true
	case wtChildPaletteID:
		return m.openWorktreeNewChild(), true
	case wtSetupLogPaletteID:
		return m.worktreeSetupLog(), true
	case wtLinkPaletteID:
		return m.worktreeLink(), true
	case wtPRPaletteID:
		return m.worktreePR(), true
	case wtNotesPaletteID:
		return m.worktreeNotes(), true
	case wtAdoptPaletteID:
		return m.worktreeAdopt(), true
	case wtForgetPaletteID:
		return m.worktreeForget(), true
	case wtSleepPaletteID:
		return m.worktreeSleep(), true
	case wtArchivePaletteID:
		return m.worktreeArchive(), true
	case wtDeletePaletteID:
		return m.worktreeDelete(), true
	case wtCleanupPaletteID:
		return m.worktreeCleanup(), true
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
		case "p":
			return m, m.worktreePin()
		}
	}
	if !st.detail {
		if text, typing, handled := inlineFilterStep(st.filter, st.filterTyping, k); handled {
			m.worktreeSetFilter(text, typing)
			return m, nil
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
	case "p":
		return m, m.worktreePin()
	case "n":
		return m, m.openWorktreeNew()
	case "c":
		return m, m.openWorktreeNewChild()
	case "L":
		return m, m.worktreeSetupLog()
	case "i":
		return m, m.worktreeLink()
	case "P":
		return m, m.worktreePR()
	case "N":
		return m, m.worktreeNotes()
	case "A":
		return m, m.worktreeAdopt()
	case "F":
		return m, m.worktreeForget()
	case "s":
		return m, m.worktreeSleep()
	case "x":
		return m, m.worktreeArchive()
	case "D":
		return m, m.worktreeDelete()
	case "C":
		return m, m.worktreeCleanup()
	case "f":
		m.worktreeNextFilter()
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
