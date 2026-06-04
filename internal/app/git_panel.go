package app

// The Source Control panel is an accordion of two collapsible sections —
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
)

type gitSection int

const (
	gitSecChanges gitSection = iota
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

	note string // gitRowNote → the dim placeholder text
}

// selectable reports whether the cursor may land on this row. Topology-only
// connector lines and empty-section notes are skipped during navigation and
// ignored on click.
func (r gitPanelRow) selectable() bool {
	return r.kind != gitRowConnector && r.kind != gitRowNote
}

// gitPanelRows flattens the accordion into one indexable row list: the
// CHANGES header, its file/dir rows (unless collapsed), then the GRAPH header
// and its commit/connector rows (unless collapsed).
func (m Model) gitPanelRows() []gitPanelRow {
	rows := make([]gitPanelRow, 0, len(m.gitFiles)+len(m.gitGraph)+2)

	rows = append(rows, gitPanelRow{kind: gitRowSection, section: gitSecChanges})
	if !m.gitChangesCollapsed {
		vis := m.gitVisibleRows()
		if len(vis) == 0 {
			rows = append(rows, gitPanelRow{kind: gitRowNote, note: "No changes"})
		}
		for _, r := range vis {
			if r.IsDir {
				rows = append(rows, gitPanelRow{kind: gitRowDir, depth: r.Depth, dirPath: r.DirPath})
			} else {
				rows = append(rows, gitPanelRow{kind: gitRowFile, depth: r.Depth, fileIndex: r.FileIndex})
			}
		}
	}

	rows = append(rows, gitPanelRow{kind: gitRowSection, section: gitSecGraph})
	if !m.gitGraphCollapsed {
		if len(m.gitGraph) == 0 {
			rows = append(rows, gitPanelRow{kind: gitRowNote, note: "No commits"})
		}
		for _, g := range m.gitGraph {
			if g.Hash == "" {
				rows = append(rows, gitPanelRow{kind: gitRowConnector, art: g.Art})
			} else {
				rows = append(rows, gitPanelRow{
					kind: gitRowCommit, art: g.Art, hash: g.Hash, subject: g.Subject, refs: g.Refs,
				})
			}
		}
	}
	return rows
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
	case gitSecChanges:
		m.gitChangesCollapsed = !m.gitChangesCollapsed
	case gitSecGraph:
		m.gitGraphCollapsed = !m.gitGraphCollapsed
	}
	m.gitClampCursor()
}

// gitPanelTopOffset is the screen-row index where the first panel row renders,
// below the SOURCE CONTROL title, the optional branch line, and a blank
// spacer. Shared by the renderer and the mouse hit-test.
func (m Model) gitPanelTopOffset() int {
	off := 2 // title + blank spacer
	if m.gitBranch.Name != "" {
		off++
	}
	return off
}

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
