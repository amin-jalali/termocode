package textdiff

import "testing"

// GITX-26 (C7): ComputeIgnoreSpace treats lines that differ only in
// spaces and tabs as unchanged (shown with their new text and both line
// numbers); real changes stay changes.
func TestComputeIgnoreSpace(t *testing.T) {
	old := "func f() {\n\treturn 1\n}\nx := 1\n"
	new := "func f()  {\n    return 1\n}\nx := 2\n"
	if d := Compute(old, new); d.Removed != 3 || d.Added != 3 {
		t.Fatalf("plain diff: -%d +%d", d.Removed, d.Added)
	}
	d := ComputeIgnoreSpace(old, new)
	if d.Removed != 1 || d.Added != 1 || len(d.Hunks) != 1 {
		t.Fatalf("ignore-space diff: -%d +%d hunks %d", d.Removed, d.Added, len(d.Hunks))
	}
	r := d.Rows[1]
	if r.Kind != Context || r.Text != "    return 1" || r.OldLine != 2 || r.NewLine != 2 {
		t.Errorf("a whitespace-only change: %+v", r)
	}
	if d.Rows[3].Kind != Removed || d.Rows[3].Text != "x := 1" || d.Rows[4].Text != "x := 2" {
		t.Errorf("the real change: %+v %+v", d.Rows[3], d.Rows[4])
	}
	if d := ComputeIgnoreSpace("a b\n", "a  b \n"); d.HasChanges() {
		t.Error("spaces inside and at the end still count")
	}
	if d := ComputeIgnoreSpace("ab\n", "a b\n"); !d.HasChanges() {
		t.Error("a space that splits a word is a change")
	}
}
