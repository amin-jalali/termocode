package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"termocode/internal/git"
)

// TestGitSidebarRowsFitWidth guards against the editor-shift bug: every row
// renderGitSidebar emits must be <= the content width. A wider row grows the
// sidebar column on JoinHorizontal and shoves the editor right — which the
// focus-only footer hints did when they outgrew the panel.
func TestGitSidebarRowsFitWidth(t *testing.T) {
	const w, h = 29, 24 // contentW for the default 30-col sidebar (minus divider)
	m := Model{
		gitIsRepo:    true,
		gitBranch:    git.Branch{Name: "main"},
		gitViewTree:  true,
		gitCollapsed: map[string]bool{},
		focus:        FocusExplorer, // focused → footer hints render
		gitFiles: gitFiles(
			"internal/app/view.go",
			"internal/app/git.go",
			"deeply/nested/path/that/keeps/going/file.go",
			"README.md",
		),
		gitGraph: []git.GraphLine{
			{Art: "* ", Hash: "a1b2c3d", Subject: "a very long commit subject that should be truncated hard", Refs: "(HEAD -> main, origin/main)"},
			{Art: "|\\ "},
			{Art: "| * ", Hash: "e5f6a7b", Subject: "feature work on the side branch"},
		},
	}
	for i, row := range strings.Split(m.renderGitSidebar(w, h), "\n") {
		if got := lipgloss.Width(row); got > w {
			t.Errorf("git sidebar row %d width = %d exceeds %d: %q", i, got, w, row)
		}
	}
}

func gitFiles(paths ...string) []git.FileStatus {
	out := make([]git.FileStatus, len(paths))
	for i, p := range paths {
		out[i] = git.FileStatus{Code: " M", Path: p}
	}
	return out
}

// rowSig renders one visible row as a compact "depth|kind:name" signature so
// tests can assert on the flattened layout without caring about styling.
func rowSig(r gitVisRow) string {
	kind := "f"
	if r.IsDir {
		kind = "d"
	}
	return string(rune('0'+r.Depth)) + "|" + kind + ":" + r.Name
}

func TestGitVisibleRows_Tree(t *testing.T) {
	m := Model{
		gitViewTree:  true,
		gitCollapsed: map[string]bool{},
		gitFiles: gitFiles(
			"internal/app/view.go",
			"internal/app/git.go",
			"internal/git/git.go",
			"README.md",
		),
	}
	got := make([]string, 0)
	for _, r := range m.gitVisibleRows() {
		got = append(got, rowSig(r))
	}
	// Dirs first (sorted), files after; "internal" expands to app/ then git/,
	// each with its files; the root-level README.md comes last.
	want := []string{
		"0|d:internal",
		"1|d:app",
		"2|f:git.go",
		"2|f:view.go",
		"1|d:git",
		"2|f:git.go",
		"0|f:README.md",
	}
	if len(got) != len(want) {
		t.Fatalf("row count = %d, want %d\n got=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGitVisibleRows_Collapse(t *testing.T) {
	m := Model{
		gitViewTree:  true,
		gitCollapsed: map[string]bool{"internal/app": true},
		gitFiles: gitFiles(
			"internal/app/view.go",
			"internal/app/git.go",
			"README.md",
		),
	}
	rows := m.gitVisibleRows()
	// A collapsed "internal/app" hides its two files: internal, app, README.
	if len(rows) != 3 {
		t.Fatalf("collapsed row count = %d, want 3: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if !r.IsDir && r.Name != "README.md" {
			t.Errorf("collapsed dir leaked file row %q", r.Name)
		}
	}
}

func TestGitVisibleRows_Flat(t *testing.T) {
	m := Model{
		gitViewTree: false,
		gitFiles:    gitFiles("internal/app/view.go", "README.md"),
	}
	rows := m.gitVisibleRows()
	if len(rows) != 2 {
		t.Fatalf("flat row count = %d, want 2", len(rows))
	}
	for i, r := range rows {
		if r.IsDir || r.Depth != 0 || r.FileIndex != i {
			t.Errorf("flat row %d unexpected: %+v", i, r)
		}
	}
}

func TestGitCurrentFileIndex_OnDir(t *testing.T) {
	// Panel rows: [0 CHANGES header][1 internal dir][2 app dir][3 view.go file]
	// [4 GRAPH header][5 "No commits" note].
	m := Model{
		gitViewTree:  true,
		gitCollapsed: map[string]bool{},
		gitFiles:     gitFiles("internal/app/view.go"),
		gitCursor:    0, // the CHANGES section header
	}
	if _, ok := m.gitCurrentFileIndex(); ok {
		t.Error("cursor on a section header should not resolve to a file index")
	}
	m.gitCursor = 1 // the "internal" dir header
	if _, ok := m.gitCurrentFileIndex(); ok {
		t.Error("cursor on a dir header should not resolve to a file index")
	}
	m.gitCursor = 3 // the file row
	if fi, ok := m.gitCurrentFileIndex(); !ok || fi != 0 {
		t.Errorf("cursor on file row = (%d,%v), want (0,true)", fi, ok)
	}
}

func TestGitPanelRows_Structure(t *testing.T) {
	m := Model{
		gitViewTree:  false, // flat changes for simpler indexing
		gitCollapsed: map[string]bool{},
		gitFiles:     gitFiles("a.go", "b.go"),
		gitGraph: []git.GraphLine{
			{Art: "* ", Hash: "a1b2", Subject: "fix", Refs: "(HEAD -> main)"},
			{Art: "|\\ "},
			{Art: "* ", Hash: "c3d4", Subject: "feat"},
		},
	}
	rows := m.gitPanelRows()
	// CHANGES hdr, a.go, b.go, spacer, GRAPH hdr, commit, connector, commit = 8.
	if len(rows) != 8 {
		t.Fatalf("rows = %d, want 8: %+v", len(rows), rows)
	}
	if rows[0].kind != gitRowSection || rows[0].section != gitSecChanges {
		t.Errorf("row0 should be CHANGES header: %+v", rows[0])
	}
	if rows[3].kind != gitRowSpacer || rows[3].selectable() {
		t.Errorf("row3 should be a non-selectable spacer: %+v", rows[3])
	}
	if rows[4].kind != gitRowSection || rows[4].section != gitSecGraph {
		t.Errorf("row4 should be GRAPH header: %+v", rows[4])
	}
	if rows[6].kind != gitRowConnector || rows[6].selectable() {
		t.Errorf("row6 should be a non-selectable connector: %+v", rows[6])
	}
	if rows[5].kind != gitRowCommit || rows[5].hash != "a1b2" {
		t.Errorf("row5 should be commit a1b2: %+v", rows[5])
	}

	// Collapsing CHANGES hides its file rows but keeps the GRAPH section.
	m.gitChangesCollapsed = true
	rows = m.gitPanelRows()
	// CHANGES hdr, spacer, GRAPH hdr, commit, connector, commit = 6.
	if len(rows) != 6 {
		t.Fatalf("collapsed rows = %d, want 6: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.kind == gitRowFile {
			t.Errorf("collapsed CHANGES leaked a file row: %+v", r)
		}
	}
}

func TestGitPanelRows_StagedSplit(t *testing.T) {
	m := Model{
		gitViewTree:  false,
		gitCollapsed: map[string]bool{},
		gitFiles: []git.FileStatus{
			{Code: "M ", Path: "staged.go"},   // index-side change → STAGED
			{Code: " M", Path: "unstaged.go"}, // worktree change → CHANGES
			{Code: "MM", Path: "both.go"},     // appears in BOTH sections
		},
	}
	var sections []gitSection
	var fileNames []string
	for _, r := range m.gitPanelRows() {
		switch r.kind {
		case gitRowSection:
			sections = append(sections, r.section)
		case gitRowFile:
			fileNames = append(fileNames, m.gitFiles[r.fileIndex].Path)
		}
	}
	// STAGED first, then CHANGES, then GRAPH.
	want := []gitSection{gitSecStaged, gitSecChanges, gitSecGraph}
	if len(sections) != 3 || sections[0] != want[0] || sections[1] != want[1] || sections[2] != want[2] {
		t.Fatalf("sections = %v, want %v", sections, want)
	}
	// both.go appears twice (staged + changes); staged.go once; unstaged.go once.
	count := map[string]int{}
	for _, n := range fileNames {
		count[n]++
	}
	if count["both.go"] != 2 {
		t.Errorf("both.go should appear in STAGED and CHANGES, got %d", count["both.go"])
	}
	if count["staged.go"] != 1 || count["unstaged.go"] != 1 {
		t.Errorf("unexpected file counts: %v", count)
	}

	staged, changed := m.gitFileCounts()
	if staged != 2 || changed != 2 { // staged: staged.go+both.go; changed: unstaged.go+both.go
		t.Errorf("counts staged=%d changed=%d, want 2/2", staged, changed)
	}
}

func TestGitMoveCursorSkipsConnectors(t *testing.T) {
	m := Model{
		gitViewTree:         false,
		gitChangesCollapsed: true, // hide files so cursor starts in the graph quickly
		gitGraph: []git.GraphLine{
			{Art: "* ", Hash: "a1", Subject: "x"},
			{Art: "|\\ "}, // connector — must be skipped
			{Art: "* ", Hash: "b2", Subject: "y"},
		},
	}
	// Rows: [0 CHANGES hdr][1 spacer][2 GRAPH hdr][3 commit a1][4 connector][5 commit b2].
	m.gitCursor = 3 // commit a1
	m.gitMoveCursor(1)
	if m.gitCursor != 5 {
		t.Errorf("down from commit a1 landed on %d, want 5 (skipping the connector)", m.gitCursor)
	}
	m.gitMoveCursor(-1)
	if m.gitCursor != 3 {
		t.Errorf("up from commit b2 landed on %d, want 3", m.gitCursor)
	}
}
