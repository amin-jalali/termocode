package explorer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newModelAt(t *testing.T, dir string) Model {
	t.Helper()
	root, err := NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := Model{root: root}
	m.SetSize(40, 20)
	return m
}

// AddRoot takes folders only: a file (or a missing path) is refused and
// the roots stay as they were.
func TestAddRootRefusesFilesAndMissingPaths(t *testing.T) {
	dir := t.TempDir()
	m := newModelAt(t, dir)
	file := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.AddRoot(file); err == nil || !strings.Contains(err.Error(), "not a folder") {
		t.Fatalf("file root: err = %v", err)
	}
	if err := m.AddRoot(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing root accepted")
	}
	if got := m.Roots(); len(got) != 1 || got[0] != m.PrimaryRoot() {
		t.Fatalf("roots changed: %v", got)
	}
}
