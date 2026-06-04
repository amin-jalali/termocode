package app

// The Source Control panel is an accordion of collapsible sections —
// CHANGES (the working-tree file list, tree or flat) and GRAPH (the current
// branch's commit graph with merge topology). gitPanelRows flattens both
// sections, their headers, and (when expanded) their contents into one row
// list. The cursor, keyboard navigation, mouse hit-testing, and the renderer
// all index into this single list so the two sections share one model.

type gitRowKind int

const (
	gitRowSection   gitRowKind = iota // accordion header: CHANGES / GRAPH
	gitRowDir                         // CHANGES: directory header (tree mode)
	gitRowFile                        // CHANGES: a changed file
	gitRowCommit                      // GRAPH: a commit node
	gitRowConnector                   // GRAPH: a topology-only line (not selectable)
	gitRowNote                        // empty-section note ("No changes") (not selectable)
	gitRowSpacer                      // blank separator between sections (not selectable)
)

type gitSection int

const (
	gitSecStaged gitSection = iota
	gitSecChanges
	gitSecGraph
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
	art     string // graph topology prefix (e.g. "* ", "| * ", "|\\ ")
	hash    string // gitRowCommit → short SHA
	subject string
	refs    string
	age     string // compact relative date ("2h")
	isMerge bool
	isHead  bool

	note string // gitRowNote → the dim placeholder text
}

// selectable reports whether the cursor may land on this row. Topology-only
// connector lines, empty-section notes, and spacers are skipped during
// navigation and ignored on click.
func (r gitPanelRow) selectable() bool {
	switch r.kind {
	case gitRowConnector, gitRowNote, gitRowSpacer:
		return false
	}
	return true
}

// gitPanelRows flattens the accordion into one indexable row list: an optional
// STAGED section, the CHANGES section, then the GRAPH section — each a header
// plus its contents (unless collapsed), separated by spacer rows.
func (m Model) gitPanelRows() []gitPanelRow {
	rows := make([]gitPanelRow, 0, len(m.gitFiles)+len(m.gitGraph)*2+4)

	// Partition files: a file with index-side changes is "staged"; one with
	// worktree changes (or untracked) is "changed". A file edited after staging
	// (e.g. "MM") legitimately appears in both, matching VSCode.
	var staged, changed []gitFileRef
	for i, f := range m.gitFiles {
		if f.Staged() {
			staged = append(staged, gitFileRef{path: f.Path, index: i})
		}
		if f.Unstaged() || f.Untracked() {
			changed = append(changed, gitFileRef{path: f.Path, index: i})
		}
	}

	appendFiles := func(vis []gitVisRow) {
		for _, r := range vis {
			if r.IsDir {
				rows = append(rows, gitPanelRow{kind: gitRowDir, depth: r.Depth, dirPath: r.DirPath})
			} else {
				rows = append(rows, gitPanelRow{kind: gitRowFile, depth: r.Depth, fileIndex: r.FileIndex})
			}
		}
	}

	// STAGED — only when something is staged.
	if len(staged) > 0 {
		rows = append(rows, gitPanelRow{kind: gitRowSection, section: gitSecStaged})
		if !m.gitStagedCollapsed {
			appendFiles(m.gitVisibleRowsFor(staged))
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
		appendFiles(vis)
	}

	// GRAPH.
	rows = append(rows, gitPanelRow{kind: gitRowSpacer})
	rows = append(rows, gitPanelRow{kind: gitRowSection, section: gitSecGraph})
	if !m.gitGraphCollapsed {
		if len(m.gitGraph) == 0 {
			rows = append(rows, gitPanelRow{kind: gitRowNote, note: "No commits"})
		}
		for _, g := range m.gitGraph {
			// The timeline renders each commit as a colour-coded ribbon row, so
			// git's ASCII topology connector lines (the art-only rows) are
			// skipped — the ribbon itself is the continuous rail.
			if g.Hash == "" {
				continue
			}
			rows = append(rows, gitPanelRow{
				kind: gitRowCommit, art: g.Art, hash: g.Hash, subject: g.Subject,
				refs: g.Refs, age: g.Age, isMerge: g.IsMerge, isHead: g.IsHead,
			})
		}
	}
	return rows
}

// gitFileCounts returns how many files have staged (index-side) changes and
// how many have worktree changes (modified-but-unstaged or untracked). A file
// can count toward both.
func (m Model) gitFileCounts() (staged, changed int) {
	for _, f := range m.gitFiles {
		if f.Staged() {
			staged++
		}
		if f.Unstaged() || f.Untracked() {
			changed++
		}
	}
	return staged, changed
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
	}
	m.gitClampCursor()
}

// gitPanelTopOffset is the screen-row index where the first panel row renders,
// below the explorer-style header: title (0), hairline (1), branch+toggle
// sub-row (2), blank spacer (3). Shared by the renderer and the mouse hit-test.
func (m Model) gitPanelTopOffset() int {
	return 4
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
	if m.focus == FocusExplorer {
		footerRows = 2 // key-hint rows, only while focused
	}
	bodyH = h - headerRows - footerRows
	if bodyH < 1 {
		// Too short for the footer — drop it and give the row to the body.
		footerRows = 0
		bodyH = h - headerRows
		if bodyH < 1 {
			bodyH = 1
		}
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
