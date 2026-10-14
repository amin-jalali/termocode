package testkit

import (
	"strings"
	"testing"
)

// findResults is the text of the Find Results buffer ("" when none).
func findResults(d *Driver) string {
	d.T.Helper()
	return lua(d, `local b = vim.g.termocode_find_results_buf
if not b or not vim.api.nvim_buf_is_valid(b) then return '' end
return table.concat(vim.api.nvim_buf_get_lines(b, 0, -1, false), '\n')`)
}

// searchNeedle starts the app with "needle" in d.txt and e.txt and runs
// Find in Files for it.
func searchNeedle(t *testing.T) *Driver {
	t.Helper()
	files := textFiles()
	files["d.txt"] = "needle\n"
	files["e.txt"] = "x\nneedle\n"
	d := editorDriver(t, files)
	d.Palette("find-files")
	d.Type("needle")
	d.Key("enter")
	if !eventually(d, func() bool {
		r := findResults(d)
		return strings.Contains(r, "d.txt (1):") && strings.Contains(r, "e.txt (1):")
	}) {
		t.Fatalf("first search:\n%s", findResults(d))
	}
	return d
}

// Spec (N2): Given Find in Files results for "needle" in d.txt and e.txt,
// when the user renames d.txt to z.txt in the explorer,
// then the results list z.txt and no longer d.txt;
// and when the user deletes e.txt in the explorer,
// then the results drop e.txt.
func TestExplorerRenameDeleteRefreshFindResults(t *testing.T) {
	d := searchNeedle(t)

	rightClickExplorer(d, "d.txt")
	clickText(d, "Rename...")
	for range len("d.txt") {
		d.Key("backspace")
	}
	d.Type("z.txt")
	d.Key("enter")
	if !eventually(d, func() bool {
		r := findResults(d)
		return strings.Contains(r, "z.txt (1):") && !strings.Contains(r, "d.txt")
	}) {
		t.Fatalf("results after rename d.txt -> z.txt:\n%s", findResults(d))
	}

	clickExplorer(d, "e.txt")
	d.Key("delete")
	d.Key("enter")
	if !eventually(d, func() bool { return !exists(d, "e.txt") && !strings.Contains(findResults(d), "e.txt") }) {
		t.Fatalf("results after deleting e.txt (exists=%v):\n%s", exists(d, "e.txt"), findResults(d))
	}
	if !strings.Contains(findResults(d), "z.txt (1):") {
		t.Errorf("z.txt dropped from the results:\n%s", findResults(d))
	}
}

// Spec (N2): Given Find in Files results for "needle" in d.txt and e.txt,
// when the user selects both in the explorer and deletes them,
// then the results list neither.
func TestExplorerBulkDeleteRefreshFindResults(t *testing.T) {
	d := searchNeedle(t)
	clickExplorer(d, "d.txt")
	d.Key("shift+down")
	d.Key("delete")
	d.Key("enter")
	if !eventually(d, func() bool {
		r := findResults(d)
		return !exists(d, "d.txt") && !exists(d, "e.txt") &&
			!strings.Contains(r, "d.txt") && !strings.Contains(r, "e.txt")
	}) {
		t.Fatalf("results after bulk delete (d=%v e=%v):\n%s", exists(d, "d.txt"), exists(d, "e.txt"), findResults(d))
	}
}
