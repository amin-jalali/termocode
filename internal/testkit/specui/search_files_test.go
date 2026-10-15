package specui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/testkit"
)

var searchFiles = map[string]string{
	"main.go":          "package main\n\nfunc main() { Add(1, 2) }\n",
	"pkg/util/util.go": "package util\n\n// Add adds.\nfunc Add(a, b int) int { return a + b }\n",
	"notes.txt":        "nothing here\n",
}

// Spec: Given a workspace where "Add" is in two files,
// when the user runs Find in Files (Alt+F) for "Add",
// then the Find Results tab lists each file once with its hit count and
// every matching line with its line number, and a summary line.
func TestFindInFilesListsResults(t *testing.T) {
	d := editorWith(t, searchFiles, "notes.txt", "nothing here")
	d.Key("alt+f")
	mustSee(t, d, "Find in Files")
	d.Type("Add")
	d.Key("enter")
	mustSee(t, d, "3 matches across 2 files")
	s := screen(d)
	for _, want := range []string{
		filepath.Join("pkg", "util", "util.go"),
		"main.go",
		"3   func main() { Add(1, 2) }",
		"4   func Add(a, b int)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("Find Results lack %q:\n%s", want, s)
		}
	}
	// Each file header ends with its hit count in a pill.
	if l := lineWith(d, "util.go"); !strings.HasSuffix(strings.TrimRight(l, " │"), " 2") {
		t.Errorf("util.go header has no count 2: %q", l)
	}
	page, err := d.Model().NvimLua(`return table.concat(vim.api.nvim_buf_get_lines(vim.g.termocode_find_results_buf, 0, -1, false), '\n')`)
	if err != nil || strings.Contains(page, "notes.txt") {
		t.Errorf("a file without a match is listed (%v):\n%s", err, page)
	}
}

// Spec: Given Find Results for "Add",
// when the user presses F4 (next result) twice and Shift+F4 once,
// then the editor opens each hit's file at the hit's line.
func TestFindInFilesNextResultOpensFileAtLine(t *testing.T) {
	d := editorWith(t, searchFiles, "notes.txt", "nothing here")
	d.Key("alt+f")
	d.Type("Add")
	d.Key("enter")
	mustSee(t, d, "3 matches across 2 files")
	var stops []string
	for _, k := range []string{"f4", "f4", "shift+f4"} {
		d.Key(k)
		d.Settle(300 * time.Millisecond)
		stops = append(stops, activeTab(d)+" "+cursorLine(d))
	}
	// The results are sorted by path: main.go first, then pkg/util/util.go.
	want := []string{"main.go Ln 3", "util.go Ln 3", "main.go Ln 3"}
	for i := range want {
		if stops[i] != want[i] {
			t.Errorf("stop %d: got %q, want %q (all: %q)", i, stops[i], want[i], stops)
		}
	}
}

// Spec: Given three files where two contain "oldName",
// when the user runs Search: Replace in Workspace, enters "oldName" and
// "newName" and confirms,
// then both files change on disk, the other file is untouched, and the
// confirm dialog said how many matches and files.
func TestReplaceInWorkspaceWritesFiles(t *testing.T) {
	slow(t)
	files := map[string]string{
		"a.go":       "var oldName = 1 // oldName\n",
		"sub/b.go":   "func f() { oldName++ }\n",
		"c.txt":      "OldName oldname\n",
		"vendor.txt": "nothing\n",
	}
	d := testkit.New(t, testkit.Options{W: 120, H: 36, Nvim: true, Files: files})
	d.Palette("replace-workspace")
	mustSee(t, d, "Replace in Workspace")
	d.Type("oldName")
	d.Key("enter")
	d.Type("newName")
	d.Key("enter")
	mustSee(t, d, `Replace 3 matches of "oldName" with "newName" in 2`)
	d.Key("enter") // the default button: Replace
	read := func(rel string) string {
		b, _ := os.ReadFile(filepath.Join(d.Dir, rel))
		return string(b)
	}
	d.WaitFor(func() bool { return !strings.Contains(read("a.go"), "oldName") }, wait)
	if got := read("a.go"); got != "var newName = 1 // newName\n" {
		t.Errorf("a.go = %q", got)
	}
	if got := read("sub/b.go"); got != "func f() { newName++ }\n" {
		t.Errorf("sub/b.go = %q", got)
	}
	if got := read("c.txt"); got != files["c.txt"] {
		t.Errorf("c.txt (other case) changed: %q", got)
	}
}

// Spec: Given a workspace with pkg/util/util.go,
// when the user opens Go to File (Ctrl+P) and types the fuzzy query
// "utl", then util.go is the match, and Enter opens it in the editor.
func TestQuickOpenFuzzyMatchOpensFile(t *testing.T) {
	d := editorWith(t, searchFiles, "notes.txt", "nothing here")
	d.Key("ctrl+p")
	d.Type("utl")
	mustSee(t, d, "1 match")
	if !strings.Contains(screen(d), "pkg/util/util.go") {
		t.Fatalf("quick open does not list pkg/util/util.go:\n%s", screen(d))
	}
	d.Key("enter")
	mustSee(t, d, "func Add(a, b int)")
	if got := activeTab(d); got != "util.go" {
		t.Errorf("active tab = %q, want util.go", got)
	}
}

// Spec: Given a workspace where "Add" is in pkg/util/util.go,
// when the user runs Find in Files for "Add",
// then the Find Results list the file by its path relative to the
// workspace (pkg/util/util.go), never by its absolute path.
func TestFindInFilesShowsRelativePaths(t *testing.T) {
	d := editorWith(t, searchFiles, "notes.txt", "nothing here")
	d.Key("alt+f")
	mustSee(t, d, "Find in Files")
	d.Type("Add")
	d.Key("enter")
	mustSee(t, d, "3 matches across 2 files")
	if !strings.Contains(lineWith(d, "util.go"), " pkg/util/util.go") {
		t.Errorf("util.go header is not workspace-relative: %q", lineWith(d, "util.go"))
	}
	if s := screen(d); strings.Contains(s, d.Dir) {
		t.Errorf("Find Results show an absolute path:\n%s", s)
	}
}

// Spec: Given a second workspace folder "otherproj" where lib/x.go has
// "Add",
// when the user runs Find in Files for "Add",
// then each header starts with its folder's name, like VSCode
// (otherproj/lib/x.go, <workspace>/main.go), and F4 still opens
// otherproj's file at the hit line.
func TestFindInFilesMultiRootPaths(t *testing.T) {
	d := editorWith(t, searchFiles, "notes.txt", "nothing here")
	other := filepath.Join(filepath.Dir(d.Dir), "otherproj")
	if err := os.MkdirAll(filepath.Join(other, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "lib", "x.go"), []byte("package lib\n\nvar _ = Add\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.Palette("workspace-add-folder")
	for range len(os.Getenv("HOME")) + 1 {
		d.Key("backspace")
	}
	d.Type(other)
	d.Key("enter")
	mustSee(t, d, "otherproj")

	d.Key("alt+f")
	mustSee(t, d, "Find in Files")
	d.Type("Add")
	d.Key("enter")
	mustSee(t, d, "4 matches across 3 files")
	s := screen(d)
	ws := filepath.Base(d.Dir)
	for _, want := range []string{" otherproj/lib/x.go ", " " + ws + "/main.go ", " " + ws + "/pkg/util/util.go "} {
		if !strings.Contains(s, want) {
			t.Errorf("Find Results lack %q:\n%s", want, s)
		}
	}
	var stops []string
	for range 4 {
		d.Key("f4")
		d.Settle(300 * time.Millisecond)
		stops = append(stops, activeTab(d)+" "+cursorLine(d))
	}
	found := false
	for _, s := range stops {
		found = found || s == "x.go Ln 3"
	}
	if !found {
		t.Errorf("F4 never opened otherproj/lib/x.go at Ln 3: %q", stops)
	}
}

// Spec: Given Find in Files results for "Add",
// then the page opens with the cursor on the first match (not on the
// query row), with no line-number gutter, and Enter opens that match.
func TestFindResultsPageStartsOnFirstMatch(t *testing.T) {
	d := editorWith(t, searchFiles, "notes.txt", "nothing here")
	d.Key("alt+f")
	d.Type("Add")
	d.Key("enter")
	mustSee(t, d, "3 matches across 2 files")
	got, err := d.Model().NvimLua(`return vim.api.nvim_get_current_line() .. "|" .. tostring(vim.wo.number)`)
	if err != nil || got != "      3   func main() { Add(1, 2) }|false" {
		t.Fatalf("current row|number = %q (%v)", got, err)
	}
	d.Key("enter")
	d.Settle(300 * time.Millisecond)
	if tab, ln := activeTab(d), cursorLine(d); tab != "main.go" || ln != "Ln 3" {
		t.Errorf("Enter opened %q %q, want main.go Ln 3", tab, ln)
	}
}

// findPage is the text of the Find Results page.
func findPage(t *testing.T, d *testkit.Driver) string {
	t.Helper()
	s, err := d.Model().NvimLua(`local b = vim.g.termocode_find_results_buf
if not b or not vim.api.nvim_buf_is_valid(b) then return '' end
return table.concat(vim.api.nvim_buf_get_lines(b, 0, -1, false), '\n')`)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Spec: Given Find Results for "Add" (main.go first, then util.go),
// when the user presses Tab on main.go's match, its group folds: the
// row is hidden and F4 goes to util.go; Tab again opens it.
func TestFindResultsTabFoldsAGroup(t *testing.T) {
	d := editorWith(t, searchFiles, "notes.txt", "nothing here")
	d.Key("alt+f")
	d.Type("Add")
	d.Key("enter")
	mustSee(t, d, "3 matches across 2 files")
	d.Key("tab")
	d.Settle(200 * time.Millisecond)
	if strings.Contains(screen(d), "func main() { Add(1, 2) }") {
		t.Fatalf("Tab did not fold main.go's group:\n%s", screen(d))
	}
	d.Key("f4")
	d.Settle(300 * time.Millisecond)
	if tab := activeTab(d); tab != "util.go" {
		t.Errorf("F4 with main.go folded opened %q, want util.go", tab)
	}
}

// Spec: Given Find Results for "Add",
// when the user presses R, Replace in Workspace opens with "Add" filled;
// F8 opens Find in Files with "Add" filled; q closes the page.
func TestFindResultsPageKeys(t *testing.T) {
	d := editorWith(t, searchFiles, "notes.txt", "nothing here")
	d.Key("alt+f")
	d.Type("Add")
	d.Key("enter")
	mustSee(t, d, "3 matches across 2 files")

	d.Key("R")
	mustSee(t, d, "Replace in Workspace")
	if !strings.Contains(screen(d), "│  Add▏") {
		t.Errorf("Replace prompt not filled:\n%s", screen(d))
	}
	d.Key("esc")
	d.Settle(200 * time.Millisecond)

	d.Key("f8")
	mustSee(t, d, "Find in Files")
	if !strings.Contains(screen(d), "│  Add▏") {
		t.Errorf("Find prompt not filled:\n%s", screen(d))
	}
	d.Key("esc")
	d.Settle(200 * time.Millisecond)

	d.Key("q")
	start := time.Now()
	d.WaitFor(func() bool { return findPage(t, d) == "" && !strings.Contains(screen(d), "Find Results") }, wait)
	if strings.Contains(screen(d), "Find Results") || findPage(t, d) != "" {
		t.Errorf("q left the Find Results tab:\n%s", screen(d))
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("the tab strip took %v to drop the closed page", took)
	}
}

// Spec: Given Find Results for "add" (smart-case: the lines with "Add"
// and "add" match),
// when the user clicks the Aa chip, the search runs again case-sensitive
// (only the "add" line); a second click turns it off again.
func TestFindResultsCaseChipClick(t *testing.T) {
	files := map[string]string{"a.go": "package a\n\n// Add it.\nfunc add() {}\n"}
	d := editorWith(t, files, "a.go", "package a")
	d.Key("alt+f")
	d.Type("add")
	d.Key("enter")
	mustSee(t, d, "2 matches across 1 file")
	clickText(t, d, "Aa")
	mustSee(t, d, "1 match across 1 file")
	if p := findPage(t, d); !strings.Contains(p, "func add()") || strings.Contains(p, "// Add it.") {
		t.Errorf("case-sensitive results:\n%s", p)
	}
	clickText(t, d, "Aa")
	mustSee(t, d, "2 matches across 1 file")
}
