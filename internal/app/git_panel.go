package app

import (
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The Source Control panel is an accordion of collapsible sections —
// CHANGES (the working-tree file list, tree or flat) and GRAPH (the current
// branch's history in date groups). gitPanelRows flattens both
// sections, their headers, and (when expanded) their contents into one row
// list. The cursor, keyboard navigation, mouse hit-testing, and the renderer
// all index into this single list so the two sections share one model.

type gitRowKind int

const (
	gitRowSection gitRowKind = iota // accordion header: CHANGES / GRAPH
	gitRowDir                       // CHANGES: directory header (tree mode)
	gitRowFile                      // CHANGES: a changed file
	gitRowCommit                    // GRAPH: a commit node
	gitRowGroup                     // GRAPH: a date group header (Today, …); folds
	gitRowMore                      // GRAPH: "Show 15 more" / "Load older commits"
	gitRowNote                      // empty-section note ("No changes") (not selectable)
	gitRowSpacer                    // blank separator between sections (not selectable)
)

type gitSection int

const (
	gitSecStaged gitSection = iota
	gitSecChanges
	gitSecGraph
	gitSecConflicts // merge conflicts (unmerged paths); shown first, only when non-empty
)

// gitPanelRow is one rendered line of the Source Control panel.
type gitPanelRow struct {
	kind    gitRowKind
	section gitSection // gitRowSection: which section this header is for

	// CHANGES rows:
	depth     int
	fileIndex int    // gitRowFile → index into m.gitFiles
	dirPath   string // gitRowDir → collapse key

	// GRAPH rows:
	ci      int    // gitRowCommit → index into m.gitCommits
	hash    string // gitRowCommit → short SHA
	subject string
	group   string // gitRowGroup → its label (also the fold key)
	groupN  int    // gitRowGroup → commits loaded in the group
	older   bool   // gitRowMore → "Load older commits" (else "Show 15 more")

	note string // gitRowNote → the dim placeholder text
}

// selectable reports whether the cursor may land on this row. Empty-section
// notes and spacers are skipped during navigation and ignored on click.
func (r gitPanelRow) selectable() bool {
	switch r.kind {
	case gitRowNote, gitRowSpacer:
		return false
	}
	return true
}

// gitPanelRows flattens the accordion into one indexable row list: an optional
// STAGED section, the CHANGES section, then the GRAPH section — each a header
// plus its contents (unless collapsed), separated by spacer rows.
func (m Model) gitPanelRows() []gitPanelRow {
	rows := make([]gitPanelRow, 0, len(m.gitFiles)+len(m.gitCommits)+16)

	// Partition files: a file with index-side changes is "staged"; one with
	// worktree changes (or untracked) is "changed". A file edited after staging
	// (e.g. "MM") legitimately appears in both, matching VSCode.
	// Unmerged paths ("UU", "AA", …) go to CONFLICTS only — their two status
	// columns would otherwise put them in both STAGED and CHANGES.
	var staged, changed, conflicts []gitFileRef
	for i, f := range m.gitFiles {
		if f.Conflicted() {
			conflicts = append(conflicts, gitFileRef{path: f.Path, index: i})
			continue
		}
		if f.Staged() {
			staged = append(staged, gitFileRef{path: f.Path, index: i})
		}
		if f.Unstaged() || f.Untracked() {
			changed = append(changed, gitFileRef{path: f.Path, index: i})
		}
	}

	appendFiles := func(vis []gitVisRow, sec gitSection) {
		for _, r := range vis {
			if r.IsDir {
				rows = append(rows, gitPanelRow{kind: gitRowDir, section: sec, depth: r.Depth, dirPath: r.DirPath})
			} else {
				rows = append(rows, gitPanelRow{kind: gitRowFile, section: sec, depth: r.Depth, fileIndex: r.FileIndex})
			}
		}
	}

	// CONFLICTS — only during a merge/rebase with unmerged paths.
	if len(conflicts) > 0 {
		rows = append(rows, gitPanelRow{kind: gitRowSection, section: gitSecConflicts})
		if !m.gitConflictsCollapsed {
			appendFiles(m.gitVisibleRowsFor(conflicts), gitSecConflicts)
		}
		rows = append(rows, gitPanelRow{kind: gitRowSpacer})
	}

	// STAGED — only when something is staged.
	if len(staged) > 0 {
		rows = append(rows, gitPanelRow{kind: gitRowSection, section: gitSecStaged})
		if !m.gitStagedCollapsed {
			appendFiles(m.gitVisibleRowsFor(staged), gitSecStaged)
		}
		rows = append(rows, gitPanelRow{kind: gitRowSpacer})
	}

	// CHANGES.
	rows = append(rows, gitPanelRow{kind: gitRowSection, section: gitSecChanges})
	if !m.gitChangesCollapsed {
		vis := m.gitVisibleRowsFor(changed)
		if len(vis) == 0 {
			rows = append(rows, gitPanelRow{kind: gitRowNote, note: "No changes"})
		}
		appendFiles(vis, gitSecChanges)
	}

	// GRAPH.
	rows = append(rows, gitPanelRow{kind: gitRowSpacer})
	rows = append(rows, gitPanelRow{kind: gitRowSection, section: gitSecGraph})
	if !m.gitGraphCollapsed {
		rows = m.appendGraphRows(rows)
	}
	return rows
}

// gitGraphStep is how many commits "Show more" adds.
const gitGraphStep = 15

// gitGraphVisible is how many commits GRAPH shows: all of Today and
// Yesterday, at least 8, at most 15 — until "Show more" raises it.
func (m Model) gitGraphVisible() int {
	n := len(m.gitCommits)
	if m.gitGraphShown > 0 {
		return min(m.gitGraphShown, n)
	}
	recent := 0
	for _, g := range m.gitCommitGroups {
		if g != "Today" && g != "Yesterday" {
			break
		}
		recent++
	}
	return min(n, max(8, min(gitGraphStep, recent)))
}

// appendGraphRows adds the GRAPH body: date group headers, the commits of
// unfolded groups, then "Show 15 more" or "Load older commits".
func (m Model) appendGraphRows(rows []gitPanelRow) []gitPanelRow {
	if len(m.gitCommits) == 0 {
		return append(rows, gitPanelRow{kind: gitRowNote, note: "No commits"})
	}
	counts := map[string]int{}
	for _, g := range m.gitCommitGroups {
		counts[g]++
	}
	vis := m.gitGraphVisible()
	last := ""
	for i := 0; i < vis && i < len(m.gitCommitGroups); i++ {
		g := m.gitCommitGroups[i]
		if g != last {
			rows = append(rows, gitPanelRow{kind: gitRowGroup, group: g, groupN: counts[g]})
			last = g
		}
		if m.gitGroupFolded[g] {
			continue
		}
		c := m.gitCommits[i]
		rows = append(rows, gitPanelRow{kind: gitRowCommit, ci: i, hash: c.Hash, subject: c.Subject})
	}
	switch {
	case vis < len(m.gitCommits):
		rows = append(rows, gitPanelRow{kind: gitRowMore})
	case m.gitGraphMore:
		rows = append(rows, gitPanelRow{kind: gitRowMore, older: true})
	}
	return rows
}

// setGitCommits stores a new history and its date groups.
func (m *Model) setGitCommits(cs []git.GraphCommit, more bool) {
	m.gitCommits, m.gitGraphMore = cs, more
	groups := make([]string, len(cs))
	for i, c := range cs {
		groups[i] = git.GroupLabel(c.Committed)
	}
	m.gitCommitGroups = groups
}

// gitToggleGroup folds / unfolds a GRAPH date group.
func (m *Model) gitToggleGroup(g string) {
	if m.gitGroupFolded == nil {
		m.gitGroupFolded = map[string]bool{}
	}
	m.gitGroupFolded[g] = !m.gitGroupFolded[g]
	m.gitClampCursor()
}

// gitShowMore shows gitGraphStep more commits, loading the next page
// from git when every loaded one is already on show.
func (m *Model) gitShowMore() tea.Cmd {
	vis := m.gitGraphVisible()
	if vis < len(m.gitCommits) {
		m.gitGraphShown = vis + gitGraphStep
		return nil
	}
	if !m.gitGraphMore {
		return nil
	}
	return gitLoadOlderCmd(len(m.gitCommits))
}

// gitOlderMsg carries the next page of commits.
type gitOlderMsg struct {
	skip    int
	commits []git.GraphCommit
	more    bool
}

// gitLoadOlderCmd reads gitGraphLimit commits after the first skip.
func gitLoadOlderCmd(skip int) tea.Cmd {
	return func() tea.Msg {
		cwd, err := procRootDir()
		if err != nil {
			return nil
		}
		cs, more, _ := git.Commits(cwd, skip, gitGraphLimit)
		return gitOlderMsg{skip: skip, commits: cs, more: more}
	}
}

// onGitOlder appends a page of older commits (when it still follows the
// loaded ones) and shows the next gitGraphStep of them.
func (m *Model) onGitOlder(msg gitOlderMsg) {
	if msg.skip != len(m.gitCommits) {
		return
	}
	m.setGitCommits(append(slices.Clip(m.gitCommits), msg.commits...), msg.more)
	m.gitGraphShown = msg.skip + gitGraphStep
}

// gitFileCounts returns how many files have staged (index-side) changes and
// how many have worktree changes (modified-but-unstaged or untracked). A file
// can count toward both.
func (m Model) gitFileCounts() (staged, changed int) {
	for _, f := range m.gitFiles {
		if f.Conflicted() {
			continue
		}
		if f.Staged() {
			staged++
		}
		if f.Unstaged() || f.Untracked() {
			changed++
		}
	}
	return staged, changed
}

// gitConflictCount returns how many files are unmerged (CONFLICTS section).
func (m Model) gitConflictCount() int {
	n := 0
	for _, f := range m.gitFiles {
		if f.Conflicted() {
			n++
		}
	}
	return n
}

// gitCurrentFileIndex resolves the cursor to an index into m.gitFiles, or
// (-1, false) when the cursor is on anything other than a file row. The file
// actions (stage / diff / discard / open) call this so they no-op on section
// headers, directory headers, and commits.
func (m Model) gitCurrentFileIndex() (int, bool) {
	rows := m.gitPanelRows()
	if m.gitCursor >= 0 && m.gitCursor < len(rows) && rows[m.gitCursor].kind == gitRowFile {
		return rows[m.gitCursor].fileIndex, true
	}
	return -1, false
}

// gitCurrentRow returns the row under the cursor, or (zero, false).
func (m Model) gitCurrentRow() (gitPanelRow, bool) {
	rows := m.gitPanelRows()
	if m.gitCursor >= 0 && m.gitCursor < len(rows) {
		return rows[m.gitCursor], true
	}
	return gitPanelRow{}, false
}

// gitMoveCursor moves the cursor to the next selectable row in direction dir
// (+1 down / -1 up), skipping connector lines. Stays put if there is none.
func (m *Model) gitMoveCursor(dir int) {
	rows := m.gitPanelRows()
	for i := m.gitCursor + dir; i >= 0 && i < len(rows); i += dir {
		if rows[i].selectable() {
			m.gitCursor = i
			return
		}
	}
}

// gitNearestSelectable finds the selectable row closest to idx, preferring the
// row above on ties. Used to bump the cursor off a connector line.
func gitNearestSelectable(rows []gitPanelRow, idx int) int {
	for off := 0; off < len(rows); off++ {
		if i := idx - off; i >= 0 && rows[i].selectable() {
			return i
		}
		if i := idx + off; i < len(rows) && rows[i].selectable() {
			return i
		}
	}
	return 0
}

// gitToggleSection folds/unfolds an accordion section and re-clamps the cursor
// (collapsing a section above the cursor shrinks the list).
func (m *Model) gitToggleSection(s gitSection) {
	switch s {
	case gitSecStaged:
		m.gitStagedCollapsed = !m.gitStagedCollapsed
	case gitSecChanges:
		m.gitChangesCollapsed = !m.gitChangesCollapsed
	case gitSecGraph:
		m.gitGraphCollapsed = !m.gitGraphCollapsed
	case gitSecConflicts:
		m.gitConflictsCollapsed = !m.gitConflictsCollapsed
	}
	m.gitClampCursor()
}

// gitPanelTopOffset is the screen-row index where the first accordion row
// renders, below the fixed Source Control chrome: title (0), hairline (1),
// branch sub-row (2), spacer (3), commit box (4), action bar (5), spacer (6).
// Shared by the renderer and the mouse hit-test — see git_commit_box.go for the
// row map.
func (m Model) gitPanelTopOffset() int {
	return 8
}

// gitSubheaderRow is the screen-row index of the branch + tree·flat sub-row,
// where the toggle is hit-tested.
const gitSubheaderRow = 2

// gitPanelLayout computes the scrollable body geometry for the accordion at
// panel height h: the scroll offset (top) that keeps the cursor visible, the
// number of body rows, and how many footer (hint) rows render. The renderer
// and the mouse hit-test both call this so a click maps to the same row that
// was drawn.
func (m Model) gitPanelLayout(h int) (top, bodyH, footerRows int) {
	headerRows := m.gitPanelTopOffset()
	// No key-hint footer: the same actions (stage/diff/discard/commit/refresh)
	// now live in the right-click context menu, so the rows go to the body.
	footerRows = 0
	bodyH = h - headerRows
	if bodyH < 1 {
		bodyH = 1
	}
	rows := m.gitPanelRows()
	if m.gitCursor >= bodyH {
		top = m.gitCursor - bodyH + 1
	}
	if maxTop := len(rows) - bodyH; top > maxTop {
		top = maxTop
	}
	if top < 0 {
		top = 0
	}
	return top, bodyH, footerRows
}

// gitCopyCommit copies GRAPH commit ci's full SHA (or, with message, its
// whole message) and says so in a toast.
func (m *Model) gitCopyCommit(ci int, message bool) tea.Cmd {
	if ci < 0 || ci >= len(m.gitCommits) {
		return nil
	}
	c := m.gitCommits[ci]
	text, what := c.FullHash, c.Hash
	if message {
		cwd, _ := m.rootDir()
		msg, err := git.CommitMessage(cwd, c.FullHash)
		if err != nil {
			var cmd tea.Cmd
			m.toast, cmd = m.toast.PushDetail(toast.Errr, "Could not read the message", err.Error())
			return cmd
		}
		text, what = msg, "the message of "+c.Hash
	}
	m.copyToClipboard(text)
	var cmd tea.Cmd
	m.toast, cmd = m.toast.Push(toast.Info, "Copied "+what)
	return cmd
}
