package app

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Multi-select rows of the Source Control panel (GITX-20, A22). Space
// marks the file row under the cursor, Shift+Up / Shift+Down mark a
// range, Ctrl+click marks one row and Shift+click the rows from the last
// one. While rows are marked, Stage (s) stages every marked row of
// CHANGES and unstages every marked row of STAGED, and Discard (x) asks
// once, on a Cancel-first card, for all of them; the discard keeps a
// safety copy in the stash list. Esc or a plain click drops the marks;
// so does every action.

// gitSelKey is one marked row: a file in a section (a partly staged file
// is in STAGED and in CHANGES, and the two rows are marked apart).
type gitSelKey struct {
	sec  gitSection
	path string
}

func init() {
	registerGitx(gitxFeature{key: func(m *Model, k tea.KeyMsg) (tea.Cmd, bool) { return m.gitSelKeyPress(k) }})
}

func (m Model) gitSelKeyOf(r gitPanelRow) (gitSelKey, bool) {
	if r.kind != gitRowFile || r.section == gitSecConflicts || r.fileIndex < 0 || r.fileIndex >= len(m.gitFiles) {
		return gitSelKey{}, false
	}
	return gitSelKey{sec: r.section, path: m.gitFiles[r.fileIndex].Path}, true
}

// gitRowSelected reports whether row r is marked.
func (m Model) gitRowSelected(r gitPanelRow) bool {
	if len(m.gitSel) == 0 {
		return false
	}
	k, ok := m.gitSelKeyOf(r)
	return ok && m.gitSel[k]
}

// gitSelRows is the marked rows on show, top to bottom.
func (m Model) gitSelRows() []gitSelKey {
	if len(m.gitSel) == 0 {
		return nil
	}
	var out []gitSelKey
	for _, r := range m.gitPanelRows() {
		if k, ok := m.gitSelKeyOf(r); ok && m.gitSel[k] {
			out = append(out, k)
		}
	}
	return out
}

// gitSelCount is how many rows on show are marked.
func (m Model) gitSelCount() int { return len(m.gitSelRows()) }

// gitSelClear drops every mark.
func (m *Model) gitSelClear() {
	m.gitSel, m.gitSelAnchor = nil, -1
}

// gitSelPrune drops the marks of rows that are gone (a new git status).
func (m *Model) gitSelPrune() {
	if len(m.gitSel) == 0 {
		return
	}
	keep := map[gitSelKey]bool{}
	for _, k := range m.gitSelRows() {
		keep[k] = true
	}
	if len(keep) == 0 {
		m.gitSelClear()
		return
	}
	m.gitSel = keep
}

// gitSelToggleRow marks or unmarks row idx and makes it the anchor.
func (m *Model) gitSelToggleRow(idx int) bool {
	rows := m.gitPanelRows()
	if idx < 0 || idx >= len(rows) {
		return false
	}
	k, ok := m.gitSelKeyOf(rows[idx])
	if !ok {
		return false
	}
	if m.gitSel == nil {
		m.gitSel = map[gitSelKey]bool{}
	}
	if m.gitSel[k] {
		delete(m.gitSel, k)
	} else {
		m.gitSel[k] = true
	}
	m.gitSelAnchor = idx
	return true
}

// gitSelFrom is the row a range starts at: the anchor, or def when
// nothing is marked.
func (m Model) gitSelFrom(def int) int {
	if len(m.gitSel) == 0 || m.gitSelAnchor < 0 || m.gitSelAnchor >= len(m.gitPanelRows()) {
		return def
	}
	return m.gitSelAnchor
}

// gitSelRange marks every file row from row from to row idx.
func (m *Model) gitSelRange(from, idx int) {
	rows := m.gitPanelRows()
	lo, hi := min(from, idx), max(from, idx)
	if m.gitSel == nil {
		m.gitSel = map[gitSelKey]bool{}
	}
	for i := lo; i <= hi && i < len(rows); i++ {
		if k, ok := m.gitSelKeyOf(rows[i]); ok {
			m.gitSel[k] = true
		}
	}
	m.gitSelAnchor = from
}

// gitSelKeyPress handles Space, Shift+Up / Shift+Down and Esc.
func (m *Model) gitSelKeyPress(k tea.KeyMsg) (tea.Cmd, bool) {
	switch k.Type {
	case tea.KeySpace:
		return nil, m.gitSelToggleRow(m.gitCursor)
	case tea.KeyShiftDown, tea.KeyShiftUp:
		from := m.gitSelFrom(m.gitCursor)
		if k.Type == tea.KeyShiftDown {
			m.gitMoveCursor(1)
		} else {
			m.gitMoveCursor(-1)
		}
		m.gitSelRange(from, m.gitCursor)
		return nil, true
	case tea.KeyEsc:
		if m.gitSelCount() > 0 {
			m.gitSelClear()
			return nil, true
		}
	}
	return nil, false
}

// gitSelClick handles a Ctrl+click or Shift+click on body row idx.
// ok=false: not a file row (the click is a plain one then).
func (m *Model) gitSelClick(idx int, ctrl, shift bool) bool {
	rows := m.gitPanelRows()
	if idx < 0 || idx >= len(rows) {
		return false
	}
	if _, ok := m.gitSelKeyOf(rows[idx]); !ok {
		return false
	}
	if shift {
		m.gitSelRange(m.gitSelFrom(m.gitCursor), idx)
	} else {
		m.gitSelToggleRow(idx)
	}
	m.gitCursor = idx
	return true
}

// gitStageSelected stages the marked CHANGES rows and unstages the
// marked STAGED rows.
func (m *Model) gitStageSelected() tea.Cmd {
	sel := m.gitSelRows()
	cwd, err := m.gitRootDir()
	if err != nil || len(sel) == 0 {
		return nil
	}
	staged, unstaged := 0, 0
	var errs []error
	for _, k := range sel {
		if k.sec == gitSecStaged {
			if err := git.Unstage(cwd, k.path); err != nil {
				errs = append(errs, err)
				continue
			}
			unstaged++
		} else {
			if err := git.Stage(cwd, k.path); err != nil {
				errs = append(errs, err)
				continue
			}
			staged++
		}
	}
	m.gitSelClear()
	var c tea.Cmd
	switch {
	case len(errs) > 0:
		m.toast, c = m.toast.PushDetail(toast.Errr, "Git error", errors.Join(errs...).Error())
	default:
		var parts []string
		if staged > 0 {
			parts = append(parts, fmt.Sprintf("Staged %d file%s", staged, plural(staged)))
		}
		if unstaged > 0 {
			parts = append(parts, fmt.Sprintf("Unstaged %d file%s", unstaged, plural(unstaged)))
		}
		m.toast, c = m.toast.Push(toast.Info, strings.Join(parts, ", "))
	}
	return tea.Batch(fetchGitCmd(), c)
}

// openGitDiscardSelectedCard asks once before discarding the marked
// files. Cancel is first and focused; the marks stay on Cancel.
func (m *Model) openGitDiscardSelectedCard() {
	var paths []string
	seen := map[string]bool{}
	newFiles := 0
	for _, k := range m.gitSelRows() {
		if seen[k.path] {
			continue
		}
		seen[k.path] = true
		paths = append(paths, k.path)
		for _, f := range m.gitFiles {
			if f.Path == k.path && f.Untracked() {
				newFiles++
			}
		}
	}
	if len(paths) == 0 {
		return
	}
	n := len(paths)
	text := fmt.Sprintf("%d file%s go back to the last commit; staged changes too.", n, plural(n))
	if newFiles > 0 {
		text += fmt.Sprintf(" %d of them %s new and %s deleted.", newFiles, map[bool]string{true: "is", false: "are"}[newFiles == 1], map[bool]string{true: "is", false: "are"}[newFiles == 1])
	}
	text += " A safety copy goes to the stash list (Git: Show Stash List…)."
	m.gitxCard(fmt.Sprintf("Discard changes in %d file%s?", n, plural(n)), text, func(m *Model, id string) tea.Cmd {
		if id != "discard" {
			return nil
		}
		return m.gitDiscardPaths(paths)
	}, confirm.Button{ID: "discard", Title: fmt.Sprintf("Discard %d File%s", n, plural(n)), Style: confirm.StyleDestructive})
}

// gitDiscardPaths moves the changes of paths (new files too) into a
// safety stash, which takes them out of the work tree.
func (m *Model) gitDiscardPaths(paths []string) tea.Cmd {
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	err = git.StashPush(cwd, git.StashOptions{Message: git.DiscardStashMessage, Untracked: true, Paths: paths})
	m.gitSelClear()
	var c tea.Cmd
	if err != nil {
		m.toast, c = m.toast.PushDetail(toast.Errr, "Discard failed", err.Error())
	} else {
		m.toast, c = m.toast.PushDetail(toast.Info, fmt.Sprintf("Discarded %d file%s", len(paths), plural(len(paths))), "safety copy: stash@{0}")
	}
	if m.nvim != nil {
		_ = m.nvim.Command("silent! checktime")
	}
	return tea.Batch(fetchGitCmd(), c)
}

// renderGitSelBar replaces the branch sub-row while rows are marked:
// "N selected · s stage · x discard · Esc clears" and the view toggle.
func renderGitSelBar(n int, viewTree bool, w int) string {
	rowStyle := lipgloss.NewStyle().Background(sidebarBg)
	strong := lipgloss.NewStyle().Background(sidebarBg).Foreground(theme.LG(theme.AccentBlue)).Bold(true)
	dim := lipgloss.NewStyle().Background(sidebarBg).Foreground(theme.LG(theme.TextMuted))
	label := fmt.Sprintf("%d selected", n)
	hint := " · s stage · x discard · Esc clears"
	toggle := gitViewToggleGlyph(viewTree)
	room := w - 1 - textWidth(label) - textWidth(toggle) - 1
	if textWidth(hint) > room {
		hint = " · Esc clears"
		if textWidth(hint) > room {
			hint = ""
		}
	}
	used := 1 + textWidth(label) + textWidth(hint)
	var sb strings.Builder
	sb.WriteString(rowStyle.Render(" "))
	sb.WriteString(strong.Render(label))
	sb.WriteString(dim.Render(hint))
	if pad := w - used - textWidth(toggle) - 1; pad > 0 {
		sb.WriteString(rowStyle.Render(strings.Repeat(" ", pad)))
		used += pad
	}
	sb.WriteString(strong.Render(toggle))
	sb.WriteString(rowStyle.Render(" "))
	used += textWidth(toggle) + 1
	return clampSidebarRow(sb.String(), used, w, rowStyle)
}
