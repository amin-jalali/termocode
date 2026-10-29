package app

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/mergeview"
)

// The three-way merge view (G-MRG): base, ours and theirs of one
// conflicted file, read-only, in rows right above the editor; the editor
// below holds the real file, which is the result. Each Take changes the
// result in one step, so one undo takes it back. Opened from a CONFLICTS
// row or "Git: Open Merge View".

// mergeNarrowW is the window width under which the panes show one at a
// time (1 · 2 · 3 switch them).
const mergeNarrowW = 120

// mergeViewState is the open merge view.
type mergeViewState struct {
	open bool
	view mergeview.Model
	rel  string // path from the repo root ("internal/x.go")
	abs  string // the result's path, as the editor has it
	// whole is the binary / deleted conflict the keep-or-delete card is about.
	whole *git.ConflictStages
}

// mergeViewShown reports whether the merge view is on screen.
func (m Model) mergeViewShown() bool { return m.merge != nil && m.merge.open }

// mergeRows is how many rows the merge view takes from an editor content
// area contentH rows tall: a bit under half, leaving the result at least
// 4 rows. Zen mode and tiny windows show no merge view.
func (m Model) mergeRows(contentH int) int {
	if !m.mergeViewShown() || m.zenMode || contentH < 10 {
		return 0
	}
	return min(max(contentH*45/100, 5), contentH-4)
}

// mergeNarrow reports whether the panes show one at a time.
func (m Model) mergeNarrow(paneW int) bool { return m.w < mergeNarrowW || paneW < 90 }

// renderMergeRows draws the merge view h rows tall and w wide.
func (m Model) renderMergeRows(w, h int) []string {
	v := m.merge.view
	v.SetSize(w, h, m.mergeNarrow(w))
	v.SetFocused(m.mergeFocused())
	return strings.Split(v.View(), "\n")
}

// mergeFocused reports whether the merge view has the keys: it was given
// them and the editor column has the focus.
func (m Model) mergeFocused() bool {
	return m.mergeViewShown() && m.merge.view.Focused() && m.focus == FocusEditor
}
