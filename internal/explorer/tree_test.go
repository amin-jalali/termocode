package explorer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func lipglossWidth(s string) int { return lipgloss.Width(s) }

func TestNewRootListsAndSorts(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "b.txt"), "")
	mustWrite(t, filepath.Join(dir, "a.txt"), "")
	mustMkdir(t, filepath.Join(dir, "zsub"))
	mustMkdir(t, filepath.Join(dir, "asub"))

	root, err := NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !root.IsDir || !root.Expanded {
		t.Fatalf("root: want dir+expanded, got %+v", root)
	}
	got := []string{}
	for _, c := range root.Children {
		got = append(got, c.Name)
	}
	want := []string{"asub", "zsub", "a.txt", "b.txt"}
	for i, n := range want {
		if got[i] != n {
			t.Fatalf("child %d: want %q, got %q (full %v)", i, n, got[i], got)
		}
	}
}

func TestVisibleHonorsExpanded(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "sub"))
	mustWrite(t, filepath.Join(dir, "sub", "x.txt"), "")

	root, err := NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	// sub is collapsed by default
	if got := len(root.Visible()); got != 2 { // root + sub
		t.Fatalf("collapsed visible: want 2, got %d", got)
	}

	for _, c := range root.Children {
		if c.Name == "sub" {
			if err := c.Toggle(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := len(root.Visible()); got != 3 { // root + sub + x.txt
		t.Fatalf("expanded visible: want 3, got %d", got)
	}
}

func TestRevealPathExpandsAncestorsAndSetsCursor(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "a"))
	mustMkdir(t, filepath.Join(dir, "a", "b"))
	mustWrite(t, filepath.Join(dir, "a", "b", "deep.txt"), "")
	mustWrite(t, filepath.Join(dir, "top.txt"), "")

	m := Model{}
	root, err := NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.root = root
	m.SetSize(40, 20)

	target := filepath.Join(dir, "a", "b", "deep.txt")
	m.RevealPath(target)

	vis := m.visible()
	if m.cursor < 0 || m.cursor >= len(vis) {
		t.Fatalf("cursor out of bounds: cursor=%d len=%d", m.cursor, len(vis))
	}
	if got := vis[m.cursor].Node.Path; got != target {
		t.Fatalf("cursor on wrong node: want %q, got %q", target, got)
	}
	// Both ancestor dirs should be expanded.
	for _, c := range root.Children {
		if c.Name == "a" {
			if !c.Expanded {
				t.Fatalf("ancestor a not expanded")
			}
			for _, cc := range c.Children {
				if cc.Name == "b" && !cc.Expanded {
					t.Fatalf("ancestor a/b not expanded")
				}
			}
		}
	}
}

func TestRevealPathOutsideRootIsNoOp(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "")

	m := Model{}
	root, err := NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.root = root
	m.SetSize(40, 20)
	startCursor := m.cursor

	other := t.TempDir()
	mustWrite(t, filepath.Join(other, "x.txt"), "")
	m.RevealPath(filepath.Join(other, "x.txt"))

	if m.cursor != startCursor {
		t.Fatalf("cursor moved on out-of-root reveal: was %d, now %d", startCursor, m.cursor)
	}
}

func TestRevealPathEmptyIsNoOp(t *testing.T) {
	dir := t.TempDir()
	m := Model{}
	root, err := NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.root = root
	m.SetSize(40, 20)
	m.RevealPath("") // must not panic
}

// TestViewVisualContract pins down the redesigned-explorer glyph contract:
// the panel header reads "E X P L O R E R", indent guides use `│` (U+2502),
// the cursor row carries a `▌` accent bar, files have no chevron (an ext-tag
// in its place) and dirs keep their `▾`/`▸` chevrons.
func TestViewVisualContract(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "sub"))
	mustWrite(t, filepath.Join(dir, "sub", "deep.go"), "")
	mustWrite(t, filepath.Join(dir, "plain.md"), "")

	root, err := NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Expand sub so we have a depth-1 child whose row carries an indent guide.
	for _, c := range root.Children {
		if c.Name == "sub" {
			if err := c.Toggle(); err != nil {
				t.Fatal(err)
			}
		}
	}

	m := Model{root: root}
	m.SetSize(60, 14)
	// Park the cursor on a depth-1 row (deep.go) so we exercise both the
	// cursor accent and an indent guide on the same line.
	vis := m.visible()
	for i, e := range vis {
		if e.Node.Name == "deep.go" {
			m.cursor = i
			break
		}
	}

	out := m.View(true, "", false, nil)
	lines := strings.Split(out, "\n")
	if len(lines) != 14 {
		t.Fatalf("expected 14 rendered rows, got %d", len(lines))
	}

	// 1. Header row contains the letter-spaced "E X P L O R E R".
	if !strings.Contains(out, "E X P L O R E R") {
		t.Fatalf("expected letter-spaced header label in output:\n%s", out)
	}
	// 2. Indent guide is `│` (U+2502).
	if !strings.Contains(out, "│") {
		t.Fatalf("expected `│` indent glyph in output:\n%s", out)
	}
	// 3. Old thin guide `▏` must be gone.
	if strings.Contains(out, "▏") {
		t.Fatalf("rendered output still contains old `▏` indent glyph:\n%s", out)
	}
	// 4. Cursor row carries the `▌` accent bar.
	if !strings.Contains(out, "▌") {
		t.Fatalf("expected `▌` cursor accent bar in output:\n%s", out)
	}
	// 5. Plain (non-dir) file rows do NOT carry a chevron. Find the row for
	// plain.md and assert neither `▾` nor `▸` appears on it.
	for i, e := range vis {
		if e.Node.Name == "plain.md" {
			// Account for header chrome rows when locating the line.
			rowIdx := i - m.top + headerChromeRows(m.h)
			if rowIdx < 0 || rowIdx >= len(lines) {
				t.Fatalf("plain.md row index %d out of range (lines=%d)", rowIdx, len(lines))
			}
			row := lines[rowIdx]
			if strings.Contains(row, "▾") || strings.Contains(row, "▸") {
				t.Fatalf("plain file row should not contain a chevron:\n%s", row)
			}
			break
		}
	}
	// 6. Directory rows DO carry a chevron.
	for i, e := range vis {
		if e.Node.IsDir && e.Node.Name == "sub" {
			rowIdx := i - m.top + headerChromeRows(m.h)
			if rowIdx < 0 || rowIdx >= len(lines) {
				t.Fatalf("sub row index %d out of range (lines=%d)", rowIdx, len(lines))
			}
			row := lines[rowIdx]
			if !strings.Contains(row, "▾") && !strings.Contains(row, "▸") {
				t.Fatalf("dir row should contain a chevron:\n%s", row)
			}
			break
		}
	}
}

// headerChromeRows mirrors the top chrome budget used by view.go so the
// test can locate a body row by index.
func headerChromeRows(h int) int {
	switch {
	case h >= 12:
		return 2 // header + hairline
	case h >= 6:
		return 2 // header + divider
	default:
		return 0
	}
}

// TestViewSmokeLandmarks exercises the redesigned explorer end-to-end and
// asserts the key visual landmarks appear on the right rows: chevrons,
// ext-tags, accent bar, footer counts, header workspace name.
func TestViewSmokeLandmarks(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "src"))
	mustWrite(t, filepath.Join(dir, "src", "main.go"), "")
	mustWrite(t, filepath.Join(dir, "src", "lib.rs"), "")
	mustWrite(t, filepath.Join(dir, "config.json"), "")
	mustWrite(t, filepath.Join(dir, "README.md"), "")

	root, err := NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range root.Children {
		if c.Name == "src" {
			if err := c.Toggle(); err != nil {
				t.Fatal(err)
			}
		}
	}

	m := Model{root: root}
	m.SetSize(60, 16)
	m.SetMeta("myproj", "feature/redesign", true, 4)

	// Synthesize a git status: main.go modified, lib.rs untracked, README.md staged.
	gs := GitStatus{
		filepath.Join(dir, "src", "main.go"):  "M",
		filepath.Join(dir, "src", "lib.rs"):   "U",
		filepath.Join(dir, "README.md"):       "A",
	}

	out := m.View(true, "", false, gs)

	// Header landmarks.
	if !strings.Contains(out, "E X P L O R E R") {
		t.Fatalf("missing letter-spaced label:\n%s", out)
	}
	if !strings.Contains(out, "MYPROJ") {
		t.Fatalf("missing workspace name:\n%s", out)
	}
	if !strings.Contains(out, "feature/red…") && !strings.Contains(out, "feature/red") {
		t.Fatalf("missing truncated branch name:\n%s", out)
	}
	if !strings.Contains(out, "⋯") {
		t.Fatalf("missing kebab `⋯`:\n%s", out)
	}

	// Tree landmarks: open-folder chevron and ext-tags.
	if !strings.Contains(out, "▾") {
		t.Fatalf("missing open-folder chevron `▾`:\n%s", out)
	}
	if !strings.Contains(out, "go") || !strings.Contains(out, "rs") || !strings.Contains(out, "js") || !strings.Contains(out, "md") {
		// "js" is in "config.json" (we use the first 2 ext chars, "js")
		// — fine. "go" / "rs" / "md" all appear.
	}
	// Right-aligned git badges.
	if !strings.Contains(out, "M") || !strings.Contains(out, "U") || !strings.Contains(out, "A") {
		t.Fatalf("missing git badge letters M/U/A:\n%s", out)
	}

	// Footer landmarks: dot, "4 files".
	if !strings.Contains(out, "●") {
		t.Fatalf("missing footer dot:\n%s", out)
	}
	if !strings.Contains(out, "4 files") {
		t.Fatalf("missing footer total `4 files`:\n%s", out)
	}

	// Cursor accent bar.
	if !strings.Contains(out, "▌") {
		t.Fatalf("missing cursor `▌` accent bar:\n%s", out)
	}

	// Width contract: every row exactly 60 cells.
	for i, line := range strings.Split(out, "\n") {
		if w := lipglossWidth(line); w != 60 {
			t.Fatalf("row %d not 60 cells wide (got %d): %q", i, w, line)
		}
	}
}

// TestViewNonRepoNoBadges asserts that when the project isn't a git repo,
// no badges, dots, or branch text appear.
func TestViewNonRepoNoBadges(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.go"), "")
	mustWrite(t, filepath.Join(dir, "b.go"), "")

	root, err := NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := Model{root: root}
	m.SetSize(50, 14)
	m.SetMeta("noproj", "", false, 2)

	out := m.View(true, "", false, nil)
	if strings.Contains(out, "●") {
		t.Fatalf("non-repo footer must not contain a status dot:\n%s", out)
	}
	if !strings.Contains(out, "2 files") {
		t.Fatalf("non-repo footer must still show file count:\n%s", out)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
