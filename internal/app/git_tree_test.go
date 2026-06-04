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
	m := Model{
		gitViewTree:  true,
		gitCollapsed: map[string]bool{},
		gitFiles:     gitFiles("internal/app/view.go"),
		gitCursor:    0, // the "internal" dir header
	}
	if _, ok := m.gitCurrentFileIndex(); ok {
		t.Error("cursor on a dir header should not resolve to a file index")
	}
	m.gitCursor = 2 // the file row (internal > app > view.go)
	if fi, ok := m.gitCurrentFileIndex(); !ok || fi != 0 {
		t.Errorf("cursor on file row = (%d,%v), want (0,true)", fi, ok)
	}
}
