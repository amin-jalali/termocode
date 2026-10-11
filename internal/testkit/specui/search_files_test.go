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
		filepath.Join("pkg", "util", "util.go") + " (2):",
		"main.go (1):",
		"3:    func main() { Add(1, 2) }",
		"4:    func Add(a, b int)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("Find Results lack %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "notes.txt (") {
		t.Errorf("a file without a match is listed:\n%s", s)
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
	if !strings.Contains(lineWith(d, "util.go (2):"), " pkg/util/util.go (2):") {
		t.Errorf("util.go header is not workspace-relative: %q", lineWith(d, "util.go (2):"))
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
	for _, want := range []string{" otherproj/lib/x.go (1):", " " + ws + "/main.go (1):", " " + ws + "/pkg/util/util.go (2):"} {
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
