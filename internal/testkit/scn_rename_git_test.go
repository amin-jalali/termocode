package testkit

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// SCN-4: renaming in the explorer a file that is open with unsaved
// edits and has staged changes in git. Nothing may be lost: the tab
// follows the new name with its unsaved text, git sees a rename (the
// staged text moves to the new name), and saving writes the new path.

// scn4Setup stages a change to main.go and to pkg/util.go.
func scn4Setup(r *repo) {
	r.write("main.go", "package main\n\n// staged\nfunc main() {}\n")
	r.write("pkg/util.go", "package pkg\n\n// staged\nfunc Add(a, b int) int { return a + b }\n")
	r.git("add", "main.go", "pkg/util.go")
}

// bufText is the text of the loaded buffer whose name ends in suffix.
func bufText(d *Driver, suffix string) string {
	d.T.Helper()
	return lua(d, `local s = ...
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  local n = vim.api.nvim_buf_get_name(b)
  if vim.api.nvim_buf_is_loaded(b) and n:sub(-#s) == s then
    return table.concat(vim.api.nvim_buf_get_lines(b, 0, -1, false), "\n")
  end
end
return "<no buffer>"`, "/"+suffix)
}

// renameInExplorer renames the explorer row old to name with the
// one-item Rename prompt.
func renameInExplorer(d *Driver, old, name string) {
	d.T.Helper()
	rightClickExplorer(d, old)
	clickText(d, "Rename...")
	for range len(old) {
		d.Key("backspace")
	}
	d.Type(name)
	d.Key("enter")
}

// waitPorcelain waits until `git status` reads want ("|"-joined lines,
// in any order: git lists renames apart from the other entries).
func waitPorcelain(d *Driver, r *repo, want string) bool {
	d.T.Helper()
	sorted := func(s string) string {
		l := strings.Split(s, "|")
		slices.Sort(l)
		return strings.Join(l, "|")
	}
	return d.WaitFor(func() bool { return sorted(r.porcelain()) == sorted(want) }, 5*time.Second)
}

// Spec: Given main.go with a staged change, open with unsaved text,
// when the user renames it to app.go in the explorer,
// then the tab shows app.go (still unsaved, same text), git status shows
// main.go renamed to app.go with the staged text kept, and saving writes
// app.go (main.go does not come back).
func TestSCN4RenameDirtyStagedFile(t *testing.T) {
	d, r := scmNvimDriver(t, scn4Setup)
	open(d, "main.go")
	d.Type("X")
	if !waitTabs(d, "main.go*") {
		got, _ := tabs(d)
		t.Fatalf("main.go not modified: %v", got)
	}
	d.Action("view:files")
	renameInExplorer(d, "main.go", "app.go")
	if !waitTabs(d, "app.go*") {
		got, _ := tabs(d)
		t.Fatalf("tab did not follow the rename with its edits: %v", got)
	}
	if got := bufText(d, "app.go"); !strings.HasPrefix(got, "Xpackage main") {
		t.Errorf("unsaved text lost: %q", got)
	}
	if !waitPorcelain(d, r, "R  main.go -> app.go|M  pkg/util.go") {
		t.Errorf("git status = %q, want main.go renamed to app.go", r.porcelain())
	}
	if got := r.git("show", ":app.go"); !strings.Contains(got, "// staged") {
		t.Errorf("staged text lost: index app.go = %q", got)
	}
	d.Palette("focus-editor")
	d.Key("ctrl+s")
	if !eventually(d, func() bool { return strings.HasPrefix(diskFile(d, "app.go"), "Xpackage main") }) {
		t.Fatalf("save did not write app.go: %q", diskFile(d, "app.go"))
	}
	if exists(d, "main.go") {
		t.Errorf("saving wrote the old name main.go again")
	}
	if !waitPorcelain(d, r, "RM main.go -> app.go|M  pkg/util.go") {
		t.Errorf("git status after save = %q", r.porcelain())
	}
}

// Spec: Given pkg/util.go with a staged change, open with unsaved text,
// when the user renames the folder pkg to lib,
// then the tab shows lib/util.go with the text, git status shows the
// rename with the staged text kept, and saving writes lib/util.go.
func TestSCN4RenameFolderOfDirtyStagedFile(t *testing.T) {
	d, r := scmNvimDriver(t, scn4Setup)
	open(d, "pkg/util.go")
	d.Type("Y")
	if !waitTabs(d, "pkg/util.go*") {
		got, _ := tabs(d)
		t.Fatalf("util.go not modified: %v", got)
	}
	d.Action("view:files")
	renameInExplorer(d, "pkg", "lib")
	if !waitTabs(d, "lib/util.go*") {
		got, _ := tabs(d)
		t.Fatalf("tab did not follow the folder rename: %v", got)
	}
	if got := bufText(d, "lib/util.go"); !strings.HasPrefix(got, "Ypackage pkg") {
		t.Errorf("unsaved text lost: %q", got)
	}
	if !waitPorcelain(d, r, "M  main.go|R  pkg/util.go -> lib/util.go") {
		t.Errorf("git status = %q, want pkg/util.go renamed to lib/util.go", r.porcelain())
	}
	d.Palette("focus-editor")
	d.Key("ctrl+s")
	if !eventually(d, func() bool { return strings.HasPrefix(diskFile(d, "lib/util.go"), "Ypackage pkg") }) {
		t.Fatalf("save did not write lib/util.go: %q", diskFile(d, "lib/util.go"))
	}
	if exists(d, "pkg") {
		t.Errorf("saving re-created the old folder pkg")
	}
}
