package testkit

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/search"
)

// SCN-19: Replace in Workspace and Find in Files on the hard cases: a
// target file open with unsaved edits (then undo), regex errors, binary
// and ignored files, and a result set past the search_max_results cap.

// replaceAll runs Search: Replace in Workspace for find -> repl, checks the
// confirm dialog says want, and picks Replace.
func replaceAll(t *testing.T, d *Driver, find, repl, want string) {
	t.Helper()
	d.Palette("replace-workspace")
	if !d.WaitForText("Replace in Workspace", 3*time.Second) {
		t.Fatalf("no Replace in Workspace prompt:\n%s", screen(d))
	}
	d.Type(find)
	d.Key("enter")
	d.Type(repl)
	d.Key("enter")
	if !d.WaitForText(want, 10*time.Second) {
		t.Fatalf("confirm dialog does not say %q:\n%s", want, screen(d))
	}
	clickButton(d, "Replace")
}

// bufText is the text of the loaded buffer for a workspace file ("" when
// it is not loaded).
func bufText(d *Driver, rel string) string {
	d.T.Helper()
	return lua(d, `local p = ...
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.api.nvim_buf_is_loaded(b) and vim.api.nvim_buf_get_name(b) == p then
    return table.concat(vim.api.nvim_buf_get_lines(b, 0, -1, false), '\n')
  end
end
return ''`, filepath.Join(d.Dir, rel))
}

// Spec: Given b.txt open and clean and a.txt open with an unsaved edit,
// both holding "needle" on disk,
// when the user replaces "needle" with "pin" in the workspace,
// then the dialog warns that 1 file has unsaved changes, b.txt changes on
// disk and in its tab, a.txt changes only in the editor (the user's edit
// is kept, the tab stays modified, the disk copy is untouched);
// and when the user presses Ctrl+Z in a.txt, the replace is undone and
// the user's edit stays; Ctrl+Z in b.txt brings the old text back too.
func TestReplaceAllDirtyBufferThenUndo(t *testing.T) {
	d := editorDriver(t, map[string]string{
		"a.txt": "alpha needle\nbravo\n",
		"b.txt": "needle two\n",
		"c.txt": "nothing\n",
	})
	open(d, "b.txt")
	open(d, "a.txt")
	d.Type("X")
	if !eventually(d, func() bool { got, _ := tabs(d); return strings.Contains(strings.Join(got, " "), "a.txt*") }) {
		got, _ := tabs(d)
		t.Fatalf("a.txt is not modified after typing: %v", got)
	}

	replaceAll(t, d, "needle", "pin", "1 of them has unsaved changes")

	if !eventually(d, func() bool { return diskFile(d, "b.txt") == "pin two\n" }) {
		t.Fatalf("b.txt on disk = %q, want the replace", diskFile(d, "b.txt"))
	}
	if !eventually(d, func() bool { return bufText(d, "a.txt") == "Xalpha pin\nbravo" }) {
		t.Fatalf("a.txt in the editor = %q, want the replace on top of the unsaved edit", bufText(d, "a.txt"))
	}
	if got := diskFile(d, "a.txt"); got != "alpha needle\nbravo\n" {
		t.Errorf("a.txt on disk = %q: the replace wrote a file with unsaved edits", got)
	}
	if got, _ := tabs(d); !strings.Contains(strings.Join(got, " "), "a.txt*") {
		t.Errorf("a.txt lost its modified mark: %v", got)
	}
	if !eventually(d, func() bool { return bufText(d, "b.txt") == "pin two" }) {
		t.Errorf("b.txt tab was not reloaded: %q", bufText(d, "b.txt"))
	}
	if got := diskFile(d, "c.txt"); got != "nothing\n" {
		t.Errorf("c.txt changed: %q", got)
	}
	if !eventually(d, func() bool { return toastsSay(d, "Replaced 2 matches in 2 files") }) {
		t.Errorf("no outcome toast: %v", d.Model().ToastTexts())
	}

	d.Key("ctrl+z")
	if !eventually(d, func() bool { return bufText(d, "a.txt") == "Xalpha needle\nbravo" }) {
		t.Fatalf("Ctrl+Z in a.txt: buffer = %q, want the replace undone and the edit kept", bufText(d, "a.txt"))
	}

	open(d, "b.txt")
	d.Key("ctrl+z")
	if !eventually(d, func() bool { return bufText(d, "b.txt") == "needle two" }) {
		t.Errorf("Ctrl+Z in b.txt: buffer = %q, want the old text", bufText(d, "b.txt"))
	}
}

// Spec: Given a.txt open with "needle" typed into it (not saved; the disk
// copy has no match),
// when the user replaces "needle" with "pin" in the workspace,
// then the match in the unsaved text is counted and replaced in the
// editor, and the disk copy stays as it was.
func TestReplaceAllCountsUnsavedOnlyMatch(t *testing.T) {
	d := editorDriver(t, map[string]string{"a.txt": "alpha\n", "b.txt": "needle\n"})
	open(d, "a.txt")
	d.Type("needle ")
	replaceAll(t, d, "needle", "pin", `Replace 2 matches of "needle" with "pin" in 2 files?`)
	if !eventually(d, func() bool { return bufText(d, "a.txt") == "pin alpha" }) {
		t.Fatalf("a.txt in the editor = %q", bufText(d, "a.txt"))
	}
	if got := diskFile(d, "a.txt"); got != "alpha\n" {
		t.Errorf("a.txt on disk = %q", got)
	}
	if !eventually(d, func() bool { return diskFile(d, "b.txt") == "pin\n" }) {
		t.Errorf("b.txt on disk = %q", diskFile(d, "b.txt"))
	}
}

// Spec: Given a workspace with "f(x" in a.txt,
// when the user runs Find in Files (regex, ripgrep) for the bad pattern
// "f(x",
// then a "Find failed" error shows and the app keeps working: a next
// search finds results, and Replace in Workspace takes "f(x" literally
// and replaces it.
func TestFindRegexErrorThenLiteralReplace(t *testing.T) {
	if !search.Available() {
		t.Skip("ripgrep not installed: the built-in search is literal, so no regex error")
	}
	files := textFiles()
	files["a.txt"] = "call f(x)\n"
	files["d.txt"] = "needle\n"
	d := editorDriver(t, files)
	d.Palette("find-files")
	d.Type("f(x")
	d.Key("enter")
	if !eventually(d, func() bool { return toastsSay(d, "Find failed") || strings.Contains(screen(d), "Search failed") }) {
		t.Fatalf("no regex error for %q: toasts %v\n%s", "f(x", d.Model().ToastTexts(), screen(d))
	}

	d.Palette("find-files")
	d.Type("needle")
	d.Key("enter")
	if !eventually(d, func() bool { return strings.Contains(findResults(d), findHeader("d.txt")) }) {
		t.Fatalf("search after the error found nothing:\n%s", findResults(d))
	}

	replaceAll(t, d, "f(x", "g(y", `Replace 1 match of "f(x" with "g(y" in 1 file?`)
	if !eventually(d, func() bool { return diskFile(d, "a.txt") == "call g(y)\n" }) {
		t.Errorf("a.txt = %q, want the literal replace", diskFile(d, "a.txt"))
	}
}

// Spec: Given "needle" in a text file, a binary file (NUL bytes) and a
// file the .gitignore lists,
// when the user replaces "needle" with "pin" in the workspace,
// then the dialog counts only the text file, and the binary and ignored
// files are byte-for-byte unchanged.
func TestReplaceAllSkipsBinaryAndIgnored(t *testing.T) {
	if !search.Available() {
		t.Skip("ripgrep not installed: the built-in walker does not read .gitignore")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	if testing.Short() {
		t.Skip("starts nvim")
	}
	bin := "PK\x00\x01needle\x00\xff\xfe"
	files := map[string]string{
		"a.txt":         "needle\n",
		"blob.bin":      bin,
		".gitignore":    "build/\n",
		"build/out.txt": "needle\n",
	}
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: files, Setup: func(tb testing.TB, dir string) {
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			tb.Fatalf("git init: %v\n%s", err, out)
		}
	}})
	replaceAll(t, d, "needle", "pin", `Replace 1 match of "needle" with "pin" in 1 file?`)
	if !eventually(d, func() bool { return diskFile(d, "a.txt") == "pin\n" }) {
		t.Fatalf("a.txt = %q", diskFile(d, "a.txt"))
	}
	if got := diskFile(d, "blob.bin"); got != bin {
		t.Errorf("binary file changed: %q", got)
	}
	if got := diskFile(d, "build/out.txt"); got != "needle\n" {
		t.Errorf("ignored file changed: %q", got)
	}
}

// Spec: Given 300 files with 10 "needle" lines each (3000 matches, more
// than the default 2000 cap),
// when the user runs Find in Files for "needle",
// then the page says it shows 2000 of 3000 matches;
// and when the user replaces "needle" with "pin" in the workspace,
// then the dialog counts all 3000 matches in 300 files and every file
// changes on disk.
func TestHugeResultSetFindCapAndReplaceAll(t *testing.T) {
	skipShortScale(t)
	files := map[string]string{}
	body := strings.Repeat("a needle here\n", 10)
	for i := range 300 {
		files[fmt.Sprintf("f/%03d.txt", i)] = body
	}
	d := editorDriver(t, files)
	d.Palette("find-files")
	d.Type("needle")
	d.Key("enter")
	if !d.WaitFor(func() bool { return strings.Contains(screen(d), "Showing 2000 of 3000 matches") }, 20*time.Second) {
		t.Fatalf("no capped summary:\n%s", screen(d))
	}

	replaceAll(t, d, "needle", "pin", `Replace 3000 matches of "needle" with "pin" in 300 files?`)
	want := strings.Repeat("a pin here\n", 10)
	if !d.WaitFor(func() bool { return diskFile(d, "f/299.txt") == want }, 20*time.Second) {
		t.Fatalf("f/299.txt = %q", diskFile(d, "f/299.txt"))
	}
	for i := range 300 {
		rel := fmt.Sprintf("f/%03d.txt", i)
		if got := diskFile(d, rel); got != want {
			t.Fatalf("%s = %q", rel, got)
		}
	}
	if !eventually(d, func() bool { return toastsSay(d, "Replaced 3000 matches in 300 files") }) {
		t.Errorf("no outcome toast: %v", d.Model().ToastTexts())
	}
}
