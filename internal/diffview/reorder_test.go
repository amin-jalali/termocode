package diffview

import (
	"strings"
	"testing"
)

// Spec (CMP-58): Reorder moves the files, keeps the shown file shown and
// sets each file's reason, which the header shows after the name. A
// file's id stays with it, and the FinishMsg names it.
func TestReorderKeepsShownFileAndIDs(t *testing.T) {
	m := New("Review AI changes", []File{
		{Name: "a.go", Old: "a\n", New: "A\n"},
		{Name: "b.go", Old: "b\n", New: "B\n"},
		{Name: "c.md", Old: "c\n", New: "C\n"},
	}, true)
	m.SetSize(140, 30)
	m.Select(1) // b.go
	ids := []int{m.FileID(0), m.FileID(1), m.FileID(2)}
	if ids[0] == 0 || ids[0] == ids[1] || ids[1] == ids[2] {
		t.Fatalf("ids must be unique and not 0: %v", ids)
	}
	if !m.Reorder([]int{2, 0, 1}, []string{"read first: says what the change is for", "", "defines B, used by a.go"}) {
		t.Fatal("Reorder refused a permutation")
	}
	if f, _ := m.FileAt(0); f.Name != "c.md" || f.Why == "" {
		t.Errorf("first file: %+v", f)
	}
	if m.Current() != 2 || m.FileID(2) != ids[1] || m.IndexOfID(ids[2]) != 0 {
		t.Errorf("shown %d, ids %v → %d %d %d", m.Current(), ids, m.FileID(0), m.FileID(1), m.FileID(2))
	}
	if s := plain(m); !strings.Contains(s, "defines B, used by a.go") {
		t.Errorf("reason not in the header:\n%s", s)
	}
	m, msg := key(m, "A")
	fin, ok := msg.(FinishMsg)
	if !ok || fin.File != 2 || fin.ID != ids[1] {
		t.Fatalf("finish: %#v", msg)
	}
	for _, bad := range [][]int{{0, 1}, {0, 0, 1}, {0, 1, 3}} {
		if m.Reorder(bad, nil) {
			t.Errorf("Reorder(%v) must refuse", bad)
		}
	}
	if m.Reorder([]int{0, 1, 2}, []string{"x"}) {
		t.Error("whys of the wrong length must be refused")
	}
}

// A file loaded again (SetFileText) keeps its id and reason.
func TestSetFileTextKeepsIDAndWhy(t *testing.T) {
	m := New("", []File{{Name: "a.go", Pending: true, Why: "w"}}, false)
	id := m.FileID(0)
	m.SetFileText(0, "a\n", "b\n")
	if f, _ := m.FileAt(0); m.FileID(0) != id || f.Why != "w" {
		t.Errorf("id %d→%d, why %q", id, m.FileID(0), f.Why)
	}
}
