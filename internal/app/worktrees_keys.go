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
		[]string{"K J", "move it up or down in its group (Shift+↑ ↓ too) · the order is kept"},
		[]string{"i", "link an issue or pull request (its state and CI show on the card)"},
		[]string{"W", "send PR feedback (review comments, @termocode mentions, failing CI) to its agent (asks first)"},
		[]string{"w", "follow its PR: new feedback asks to go to the agent · again: stop"},
		[]string{"P", "the linked pull request: checks and their logs, review threads, merge"},
		[]string{"N", "the notes agents left on it, with their times"},
		[]string{"d", "diff vs base: what it changed since it left its base · keep or revert each change"},
		[]string{"n", "new worktree: name, start point, agent and first task"},
		[]string{"c", "new child worktree: from this one's branch, listed under it; its PR targets that branch"},
		[]string{"I", "start from an issue: pick an open issue, the form fills in branch, agent and task"},
		[]string{"e", "restack a child: replay its commits onto its parent's branch (asks first)"},
		[]string{"H", "hand off: a new agent goes on with a note of this work, here or in a child"},
		[]string{"E", "new worktree in each repository of this window, on one branch"},
		[]string{"L", "show the setup of a new worktree (its terminal tab)"},
		[]string{"r", "run the dev server on the worktree's own port ($PORT) · again: show its tab"},
		[]string{"o", "open the running dev server's address in the browser"},
		[]string{"A", "adopt a worktree made outside termocode (keeps its notes)"},
		[]string{"F", "forget a worktree whose folder is gone"},
		[]string{"x", "archive: remove the folder, keep the branch · on an archived row: restore"},
		[]string{"D", "delete the worktree and its branch (asks, step by step)"},
		[]string{"C", "clean up: tick merged and old worktrees, archive them together"},
		[]string{"S", "ship: commit (AI message), push, pull request (AI text), CI fixes, merge, archive · each step asks"},
		[]string{"M", "move to another machine: pack it into one file (commits, changes, new files, notes)"},
		[]string{"U", "unpack a moved lane as a new worktree, or pull one from a host over ssh"},
		[]string{"s", "sleep: end its agent tabs, move it under ASLEEP · again: wake it"},
		[]string{"b", "background task: an agent works on it alone in a sandbox container (asks first)"},
		[]string{"B", "background tasks: state, follow output, stop, remove"},
		[]string{"Sessions"},
		[]string{"T", "its agent sessions: Enter shows the tab"},
		[]string{"O", "new agent session: one more agent in it, its own tab"},
		[]string{"Watch"},
		[]string{"v", "preview: its agent's live screen in the panel, the list keeps the keys · again: close"},
		[]string{"V", "agent grid: up to 4 agent screens of this window side by side"},
		[]string{"Enter", "in the preview: jump into the selected agent's tab · Tab picks a tile · g grid"},
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
		{ID: wtMoveUpPaletteID, Title: "Worktree: Move Selected Up"},
		{ID: wtMoveDownPaletteID, Title: "Worktree: Move Selected Down"},
		{ID: wtResetOrderPaletteID, Title: "Worktree: Reset Order"},
		{ID: wtNewPaletteID, Title: "Worktree: New…"},
		{ID: wtChildPaletteID, Title: "Worktree: New Child of Selected…"},
		{ID: wtHandoffPaletteID, Title: "Worktree: Hand Off Selected…"},
		{ID: wtRestackPaletteID, Title: "Worktree: Restack Selected onto Parent…"},
		{ID: wtMultiPaletteID, Title: "Worktree: New in Each Repository…"},
		{ID: wtSetupLogPaletteID, Title: "Worktree: Show Setup Log"},
		{ID: wtRunPaletteID, Title: "Worktree: Run Dev Server of Selected"},
		{ID: wtRunOpenPaletteID, Title: "Worktree: Open Dev Server of Selected"},
		{ID: wtLinkPaletteID, Title: "Worktree: Link Issue or PR…"},
		{ID: wtFeedbackPaletteID, Title: "Worktree: Send PR Feedback to Agent…"},
		{ID: wtFollowPaletteID, Title: "Worktree: Follow PR Feedback (On or Off)"},
		{ID: wtPRPaletteID, Title: "Worktree: Show Pull Request of Selected"},
		{ID: wtNotesPaletteID, Title: "Worktree: Show Notes of Selected"},
		{ID: wtDiffPaletteID, Title: "Worktree: Review Changes vs Base of Selected"},
		{ID: wtDepsPaletteID, Title: "Worktree: Show Dependency Graph"},
		{ID: wtAdoptPaletteID, Title: "Worktree: Adopt Selected"},
		{ID: wtForgetPaletteID, Title: "Worktree: Forget Gone Selected"},
		{ID: wtSleepPaletteID, Title: "Worktree: Sleep or Wake Selected"},
		{ID: wtSessionsPaletteID, Title: "Worktree: Agent Sessions of Selected…"},
		{ID: wtSessionNewPaletteID, Title: "Worktree: New Agent Session in Selected…"},
		{ID: wtArchivePaletteID, Title: "Worktree: Archive or Restore Selected…"},
		{ID: wtDeletePaletteID, Title: "Worktree: Delete Selected…"},
		{ID: wtCleanupPaletteID, Title: "Worktree: Clean Up…"},
		{ID: wtShipPaletteID, Title: "Worktree: Ship Selected (commit, push, PR, merge, archive)…"},
		{ID: wtBgTaskPaletteID, Title: "Worktree: Run Agent Task in Background…"},
		{ID: wtBgTasksPaletteID, Title: "Worktree: Background Tasks…"},
		{ID: wtMovePaletteID, Title: "Worktree: Move Selected to Another Machine…"},
		{ID: wtUnpackPaletteID, Title: "Worktree: Unpack a Moved Lane…"},
		{ID: wtPullPaletteID, Title: "Worktree: Pull a Lane from a Host…"},
		{ID: wtPreviewPaletteID, Title: "Worktree: Preview Agent Screen"},
		{ID: wtPreviewGridPaletteID, Title: "Worktree: Preview Agent Grid"},
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
	case wtMoveUpPaletteID:
		return m.worktreeReorder(-1), true
	case wtMoveDownPaletteID:
		return m.worktreeReorder(1), true
	case wtResetOrderPaletteID:
		return m.worktreeResetOrder(), true
	case wtNewPaletteID:
		return m.openWorktreeNew(), true
	case wtChildPaletteID:
		return m.openWorktreeNewChild(), true
	case wtHandoffPaletteID:
		return m.worktreeHandoff(), true
	case wtRestackPaletteID:
		return m.worktreeRestack(), true
	case wtMultiPaletteID:
		return m.openWorktreeNewMulti(), true
	case wtSetupLogPaletteID:
		return m.worktreeSetupLog(), true
	case wtRunPaletteID:
		return m.worktreeRun(), true
	case wtRunOpenPaletteID:
		return m.worktreeRunOpen(), true
	case wtLinkPaletteID:
		return m.worktreeLink(), true
	case wtFeedbackPaletteID:
		return m.worktreeFeedback(), true
	case wtFollowPaletteID:
		return m.worktreeFollow(), true
	case wtPRPaletteID:
		return m.worktreePR(), true
	case wtNotesPaletteID:
		return m.worktreeNotes(), true
	case wtDiffPaletteID:
		return m.worktreeDiff(), true
	case wtDepsPaletteID:
		return m.worktreeDeps(), true
	case wtAdoptPaletteID:
		return m.worktreeAdopt(), true
	case wtForgetPaletteID:
		return m.worktreeForget(), true
	case wtSleepPaletteID:
		return m.worktreeSleep(), true
	case wtSessionsPaletteID:
		return m.worktreeSessions(), true
	case wtSessionNewPaletteID:
		return m.worktreeNewSession(), true
	case wtArchivePaletteID:
		return m.worktreeArchive(), true
	case wtDeletePaletteID:
		return m.worktreeDelete(), true
	case wtCleanupPaletteID:
		return m.worktreeCleanup(), true
	case wtShipPaletteID:
		return m.worktreeShip(), true
	case wtBgTaskPaletteID:
		return m.worktreeBgTask(), true
	case wtBgTasksPaletteID:
		return m.worktreeBgTasks(), true
	case wtMovePaletteID:
		return m.worktreeMove(), true
	case wtUnpackPaletteID:
		return m.worktreeUnpack(), true
	case wtPullPaletteID:
		return m.worktreePull(), true
	case wtPreviewPaletteID:
		return m.worktreePreview(false), true
	case wtPreviewGridPaletteID:
		return m.worktreePreview(true), true
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
	case "K", "shift+up":
		return m, m.worktreeReorder(-1)
	case "J", "shift+down":
		return m, m.worktreeReorder(1)
	case "n":
		return m, m.openWorktreeNew()
	case "c":
		return m, m.openWorktreeNewChild()
	case "I":
		return m, m.openWorktreeIssues()
	case "H":
		return m, m.worktreeHandoff()
	case "e":
		return m, m.worktreeRestack()
	case "E":
		return m, m.openWorktreeNewMulti()
	case "L":
		return m, m.worktreeSetupLog()
	case "r":
		return m, m.worktreeRun()
	case "o":
		return m, m.worktreeRunOpen()
	case "i":
		return m, m.worktreeLink()
	case "W":
		return m, m.worktreeFeedback()
	case "w":
		return m, m.worktreeFollow()
	case "P":
		return m, m.worktreePR()
	case "N":
		return m, m.worktreeNotes()
	case "d":
		return m, m.worktreeDiff()
	case "A":
		return m, m.worktreeAdopt()
	case "F":
		return m, m.worktreeForget()
	case "s":
		return m, m.worktreeSleep()
	case "T":
		return m, m.worktreeSessions()
	case "O":
		return m, m.worktreeNewSession()
	case "x":
		return m, m.worktreeArchive()
	case "D":
		return m, m.worktreeDelete()
	case "C":
		return m, m.worktreeCleanup()
	case "S":
		return m, m.worktreeShip()
	case "b":
		return m, m.worktreeBgTask()
	case "B":
		return m, m.worktreeBgTasks()
	case "M":
		return m, m.worktreeMove()
	case "U":
		return m, m.worktreeUnpack()
	case "v":
		return m, m.worktreePreview(false)
	case "V":
		return m, m.worktreePreview(true)
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
	m.wtPreviewMoved() // CMP-8: the preview follows the cursor
}
