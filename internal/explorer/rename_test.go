package explorer

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRenamePlan(t *testing.T) {
	dir := t.TempDir()
	a, b, c := filepath.Join(dir, "a"), filepath.Join(dir, "b"), filepath.Join(dir, "c")
	for _, tt := range []struct {
		name               string
		names              []string
		changes, conflicts int
	}{
		{"swaps", []string{"b", "a"}, 2, 0}, {"unchanged", []string{"a", "b"}, 0, 0},
		{"one changed", []string{"d", "b"}, 1, 0}, {"duplicate", []string{"d", "d"}, 2, 1},
		{"unchanged target", []string{"b", "b"}, 1, 1}, {"exists", []string{"c", "b"}, 1, 1},
		{"empty", []string{"", "b"}, 0, 1}, {"slash", []string{"x/y", "b"}, 0, 1},
		{"backslash", []string{"x\\y", "b"}, 0, 1}, {"parent", []string{"..", "b"}, 0, 1},
		{"missing line", []string{"a"}, 0, 1}, {"extra line", []string{"a", "b", "c"}, 0, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := PlanRename([]string{a, b}, tt.names, map[string]bool{a: true, b: true, c: true})
			if len(p.Changes) != tt.changes || len(p.Conflicts) != tt.conflicts {
				t.Fatalf("%+v", p)
			}
		})
	}
}
func TestRenameApplySwapUndoAndRollback(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	mustWrite(t, a, "alpha")
	mustWrite(t, b, "bravo")
	changes := []Rename{{a, b}, {b, a}}
	var followed []Rename
	if err := ApplyRename(changes, func(a, b string) { followed = append(followed, Rename{a, b}) }); err != nil {
		t.Fatal(err)
	}
	if len(followed) != 4 {
		t.Fatalf("follow phases: %v", followed)
	}
	data, _ := os.ReadFile(a)
	if string(data) != "bravo" {
		t.Fatal(string(data))
	}
	if err := ApplyRename(changes, nil); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(a)
	if string(data) != "alpha" {
		t.Fatal(string(data))
	}
	// Missing second source forces rollback after the first was staged.
	if err := ApplyRename([]Rename{{a, filepath.Join(dir, "c")}, {filepath.Join(dir, "missing"), filepath.Join(dir, "d")}}, nil); err == nil {
		t.Fatal("want error")
	}
	data, _ = os.ReadFile(a)
	if string(data) != "alpha" {
		t.Fatal("rollback lost a")
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !reflect.DeepEqual(names, []string{"a", "b"}) {
		t.Fatal(names)
	}
}
func TestRenameStampDetectsChanges(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a")
	mustWrite(t, p, "one")
	before, err := RenameStamp(dir)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, p, "two")
	after, err := RenameStamp(dir)
	if err != nil || before == after {
		t.Fatal("change missed", err)
	}
	p1 := PlanRename([]string{dir, p}, []string{"folder", "b"}, nil)
	if !strings.Contains(strings.Join(p1.Conflicts, " "), "folder or its files") {
		t.Fatal(p1)
	}
}
