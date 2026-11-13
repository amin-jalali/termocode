package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// Drag and drop in the Files explorer (SCN-18). Press on a row, drag
// onto a folder (or onto a file: its folder) and let go. The folder under
// the pointer is lit while the drag is over it. A card asks first: Cancel
// is first and has the focus, Move does it. A move never replaces a file,
// a folder never moves into itself, and a selected group moves together
// when the drag starts on one of its rows. Open tabs and git follow the
// move like a rename.

// confirmKindExplorerMove is the card of a drop in the tree.
const confirmKindExplorerMove confirmKindEnum = 6187

// explorerDrag is a press in the Files tree that may become a drag.
type explorerDrag struct {
	from  string // the row pressed; "" = no press in the tree
	y     int    // the panel-local row of the press
	moved bool   // the pointer left that row: it is a drag
}

// explorerDragMouse runs the drag part of a mouse event on the Files tree
// (y panel-local). handled = the tree must not see the event as a click.
func (m *Model) explorerDragMouse(y int, msg tea.MouseMsg) (tea.Cmd, bool) {
	switch {
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		m.endExplorerDrag()
		if msg.Ctrl || msg.Shift { // selection clicks, not a drag
			return nil, false
		}
		if n, ok := m.explorer.VisibleAt(y); ok {
			m.expDrag = explorerDrag{from: n.Path, y: y}
		}
		return nil, false
	case msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonLeft:
		// A terminal reports a drag as left-button events; the tree took
		// each one as one more click (a folder opened and closed).
		if m.expDrag.from == "" {
			return nil, true
		}
		if y != m.expDrag.y {
			m.expDrag.moved = true
		}
		if m.expDrag.moved {
			m.explorer.SetDropTarget(m.dropFolderAt(y))
		}
		return nil, true
	case msg.Action == tea.MouseActionRelease:
		d := m.expDrag
		m.endExplorerDrag()
		if d.from == "" || !d.moved {
			return nil, false
		}
		dest := m.dropFolderAt(y)
		if dest == "" {
			return nil, true
		}
		return m.askExplorerMove(m.dragSources(d.from), dest), true
	}
	return nil, false
}

// endExplorerDrag forgets the drag and unlights its folder.
func (m *Model) endExplorerDrag() {
	m.expDrag = explorerDrag{}
	m.explorer.SetDropTarget("")
}

// cancelExplorerDragOutside ends a drag let go outside the Files tree: the
// drop is off.
func (m *Model) cancelExplorerDragOutside(msg tea.MouseMsg) {
	if m.expDrag.from == "" || msg.Action != tea.MouseActionRelease {
		return
	}
	x := msg.X - activity.Width
	inTree := m.showExp && !m.zenMode && x >= 0 && x < m.explorerWidth && m.activity.Active() == activity.ViewFiles
	if !inTree {
		m.endExplorerDrag()
	}
}

// dropFolderAt is the folder a drop on row y goes into: the folder row
// itself, or the folder of a file row; "" off the rows.
func (m *Model) dropFolderAt(y int) string {
	n, ok := m.explorer.VisibleAt(y)
	if !ok {
		return ""
	}
	if n.IsDir {
		return n.Path
	}
	return filepath.Dir(n.Path)
}

// dragSources is what a drag from row from moves: the whole selection
// when the row is part of it, else the row alone.
func (m *Model) dragSources(from string) []string {
	sel := m.explorer.Selection()
	if len(sel) > 1 && m.explorer.IsSelected(from) {
		sort.Strings(sel)
		return sel
	}
	return []string{from}
}

// planExplorerMove is the moves that put srcs into the folder dest, or
// the one reason it cannot be done. Items already in dest are left out;
// no moves and no problem means there is nothing to do. exists reports
// whether a path is taken; roots are the workspace roots (they stay).
func planExplorerMove(srcs []string, dest string, roots []string, exists func(string) bool) ([]git.Move, string) {
	sep := string(filepath.Separator)
	destName := filepath.Base(dest)
	targets := map[string]bool{}
	var moves []git.Move
	for _, s := range srcs {
		for _, r := range roots {
			if s == r {
				return nil, filepath.Base(s) + " is a workspace root"
			}
		}
		if s == dest || strings.HasPrefix(dest, s+sep) {
			return nil, "A folder cannot move into itself"
		}
		for _, other := range srcs {
			if other != s && strings.HasPrefix(s, other+sep) {
				return nil, "Select a folder or its files, not both"
			}
		}
		if filepath.Dir(s) == dest {
			continue
		}
		to := filepath.Join(dest, filepath.Base(s))
		if targets[to] || exists(to) {
			return nil, filepath.Base(s) + " already exists in " + destName
		}
		targets[to] = true
		moves = append(moves, git.Move{From: s, To: to})
	}
	return moves, ""
}

// askExplorerMove opens the card for moving srcs into dest, or says why
// it cannot be done.
func (m *Model) askExplorerMove(srcs []string, dest string) tea.Cmd {
	exists := func(p string) bool { _, err := os.Lstat(p); return err == nil }
	if _, ok := m.remoteFS(); ok {
		// A container: no engine call on the UI loop (FIX-2); the move
		// checks each name there and refuses a taken one.
		exists = func(string) bool { return false }
	}
	moves, problem := planExplorerMove(srcs, dest, m.explorer.Roots(), exists)
	if problem != "" {
		m.err = problem
		return m.pushToastDetail(toast.Warn, "Cannot move", problem)
	}
	if len(moves) == 0 {
		return nil
	}
	into := m.dropFolderName(dest)
	var lines []string
	if len(moves) == 1 {
		lines = append(lines, fmt.Sprintf("Move %q into %q?", filepath.Base(moves[0].From), into))
	} else {
		lines = append(lines, fmt.Sprintf("Move %d items into %q?", len(moves), into), "")
		const previewN = 5
		for i, mv := range moves {
			if i == previewN {
				lines = append(lines, fmt.Sprintf("  …and %d more", len(moves)-previewN))
				break
			}
			lines = append(lines, "  "+filepath.Base(mv.From))
		}
	}
	lines = append(lines, "", "Open tabs follow the move.")
	m.confirm = confirm.New("Move?", strings.Join(lines, "\n"), []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "move", Title: "Move"},
	}).WithFocus(0)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindExplorerMove
	m.expMoves = moves
	return nil
}

// dropFolderName is dest as the card shows it: its path in the workspace,
// or the root folder's name.
func (m *Model) dropFolderName(dest string) string {
	for _, r := range m.explorer.Roots() {
		if rel, err := filepath.Rel(r, dest); err == nil && !strings.HasPrefix(rel, "..") {
			if rel == "." {
				return filepath.Base(r)
			}
			return filepath.ToSlash(rel)
		}
	}
	return filepath.Base(dest)
}

// answerExplorerMove runs the card's answer.
func (m *Model) answerExplorerMove(id string) tea.Cmd {
	moves := m.expMoves
	m.expMoves = nil
	if id != "move" || len(moves) == 0 {
		return nil
	}
	if fsys, ok := m.remoteFS(); ok { // a container: off the UI loop (FIX-2)
		return m.remoteExplorerMoves(fsys, moves)
	}
	var done []git.Move
	fail := ""
	for _, mv := range moves {
		// The card waited for the user: a name taken since then stays.
		if _, err := os.Lstat(mv.To); err == nil {
			fail = filepath.Base(mv.To) + " already exists in " + filepath.Base(filepath.Dir(mv.To))
			break
		}
		if err := os.Rename(mv.From, mv.To); err != nil {
			fail = fileOpFailText(err)
			break
		}
		m.followRename(mv.From, mv.To)
		done = append(done, mv)
	}
	return m.finishExplorerMoves(done, fail, true)
}

// finishExplorerMoves shows the result of the moves done (and of the one
// that failed): the tree, search results, git and a toast.
func (m *Model) finishExplorerMoves(done []git.Move, fail string, local bool) tea.Cmd {
	if local && len(done) > 0 {
		moveGitIndex(done...)
	}
	m.explorer.Reload()
	if len(done) > 0 {
		m.explorer.RevealPath(done[0].To)
	}
	cmds := []tea.Cmd{m.refreshSearchResults()}
	if local {
		cmds = append(cmds, fetchGitCmd())
	}
	if fail != "" {
		m.err = fail
		title := "Move failed"
		if len(done) > 0 {
			title = fmt.Sprintf("Moved %d, then failed", len(done))
		}
		return tea.Batch(append(cmds, m.pushToastDetail(toast.Errr, title, fail))...)
	}
	m.err = ""
	msg := "Moved " + filepath.Base(done[0].From)
	if len(done) > 1 {
		msg = fmt.Sprintf("Moved %d items", len(done))
	}
	return tea.Batch(append(cmds, m.pushToastDetail(toast.Info, msg, "into "+m.dropFolderName(filepath.Dir(done[0].To))))...)
}

// remoteExplorerMoves does the moves in the container workspace through
// its FS; a taken name stays.
func (m *Model) remoteExplorerMoves(fsys workspace.FS, moves []git.Move) tea.Cmd {
	label := m.workspace().Host.Label()
	return func() tea.Msg {
		ctx := context.Background()
		var done []git.Move
		fail := ""
		for _, mv := range moves {
			if _, err := fsys.Stat(ctx, mv.To); err == nil {
				fail = filepath.Base(mv.To) + " already exists"
				break
			}
			if err := fsys.Rename(ctx, mv.From, mv.To); err != nil {
				fail = remoteErrText(label, err)
				break
			}
			noteMirrorRename(mv.From, mv.To) // RC-6 (container_lsp.go)
			done = append(done, mv)
		}
		return applyMsg(func(m *Model) tea.Cmd {
			for _, mv := range done {
				m.followRename(mv.From, mv.To)
			}
			return m.finishExplorerMoves(done, fail, false)
		})
	}
}
