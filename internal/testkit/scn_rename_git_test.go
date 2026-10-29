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

// scn4Setup stages a change to README.md and to docs/guide.md. (Text
// files: a .go file would start a language server when one is installed.)
// The files are long enough for git to see the renames as renames.
func scn4Setup(r *repo) {
	body := "\none\ntwo\nthree\nfour\nfive\nsix\nseven\neight\n"
	r.write("README.md", "# Demo\n"+body)
	r.write("docs/guide.md", "# Guide\n"+body)
	r.commit("longer docs")
	r.write("README.md", "# Demo\n"+body+"staged\n")
	r.write("docs/guide.md", "# Guide\n"+body+"staged\n")
	r.git("add", "README.md", "docs/guide.md")
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

// Spec: Given README.md with a staged change, open with unsaved text,
// when the user renames it to INTRO.md in the explorer,
// then the tab shows INTRO.md (still unsaved, same text), git status
// shows README.md renamed to INTRO.md with the staged text kept, and
// saving writes INTRO.md (README.md does not come back).
func TestSCN4RenameDirtyStagedFile(t *testing.T) {
	d, r := scmNvimDriver(t, scn4Setup)
	open(d, "README.md")
	d.Type("X")
	if !waitTabs(d, "README.md*") {
		got, _ := tabs(d)
		t.Fatalf("README.md not modified: %v", got)
	}
	d.Action("view:files")
	renameInExplorer(d, "README.md", "INTRO.md")
	if !waitTabs(d, "INTRO.md*") {
		got, _ := tabs(d)
		t.Fatalf("tab did not follow the rename with its edits: %v", got)
	}
	if got := bufText(d, "INTRO.md"); !strings.HasPrefix(got, "X# Demo") {
		t.Errorf("unsaved text lost: %q", got)
	}
	if !waitPorcelain(d, r, "R  README.md -> INTRO.md|M  docs/guide.md") {
		t.Errorf("git status = %q, want README.md renamed to INTRO.md", r.porcelain())
	}
	if got := r.git("show", ":INTRO.md"); !strings.Contains(got, "staged") {
		t.Errorf("staged text lost: index INTRO.md = %q", got)
	}
	d.Palette("focus-editor")
	d.Key("ctrl+s")
	if !eventually(d, func() bool { return strings.HasPrefix(diskFile(d, "INTRO.md"), "X# Demo") }) {
		t.Fatalf("save did not write INTRO.md: %q", diskFile(d, "INTRO.md"))
	}
	if exists(d, "README.md") {
		t.Errorf("saving wrote the old name README.md again")
	}
	if !waitPorcelain(d, r, "RM README.md -> INTRO.md|M  docs/guide.md") {
		t.Errorf("git status after save = %q", r.porcelain())
	}
}

// Spec: Given docs/guide.md with a staged change, open with unsaved
// text, when the user renames the folder docs to manual,
// then the tab shows manual/guide.md with the text, git status shows the
// renames with the staged text kept, and saving writes manual/guide.md.
func TestSCN4RenameFolderOfDirtyStagedFile(t *testing.T) {
	d, r := scmNvimDriver(t, scn4Setup)
	open(d, "docs/guide.md")
	d.Type("Y")
	if !waitTabs(d, "docs/guide.md*") {
		got, _ := tabs(d)
		t.Fatalf("guide.md not modified: %v", got)
	}
	d.Action("view:files")
	renameInExplorer(d, "docs", "manual")
	if !waitTabs(d, "manual/guide.md*") {
		got, _ := tabs(d)
		t.Fatalf("tab did not follow the folder rename: %v", got)
	}
	if got := bufText(d, "manual/guide.md"); !strings.HasPrefix(got, "Y# Guide") {
		t.Errorf("unsaved text lost: %q", got)
	}
	if !waitPorcelain(d, r, "M  README.md|R  docs/guide.md -> manual/guide.md|R  docs/remove.txt -> manual/remove.txt") {
		t.Errorf("git status = %q, want docs/guide.md renamed to manual/guide.md", r.porcelain())
	}
	d.Palette("focus-editor")
	d.Key("ctrl+s")
	if !eventually(d, func() bool { return strings.HasPrefix(diskFile(d, "manual/guide.md"), "Y# Guide") }) {
		t.Fatalf("save did not write manual/guide.md: %q", diskFile(d, "manual/guide.md"))
	}
	if exists(d, "docs") {
		t.Errorf("saving re-created the old folder docs")
	}
}
