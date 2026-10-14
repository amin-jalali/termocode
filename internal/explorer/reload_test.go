package explorer

import (
	"os"
	"path/filepath"
	"testing"
)

// Spec: Reload keeps the cursor on the same file when entries above it
// appear or vanish (an AI tool or a shell command created a file); it
// used to keep the row number, so the cursor jumped to another file.
func TestReloadKeepsCursorOnSamePath(t *testing.T) {
	m, paths := buildModelWithFiles(t, []string{"b.txt", "c.txt", "d.txt"})
	dir := filepath.Dir(paths[1])
	t.Chdir(dir)
	m.RevealPath(filepath.Join(dir, "c.txt"))
	mustWrite(t, filepath.Join(dir, "a.txt"), "")
	m.Reload()
	if got := m.CursorPath(); got != filepath.Join(dir, "c.txt") {
		t.Fatalf("after a file appeared above: cursor %q, want c.txt", got)
	}
	_ = os.Remove(filepath.Join(dir, "a.txt"))
	_ = os.Remove(filepath.Join(dir, "b.txt"))
	m.Reload()
	if got := m.CursorPath(); got != filepath.Join(dir, "c.txt") {
		t.Fatalf("after files above vanished: cursor %q, want c.txt", got)
	}
	// The cursor's own file vanishes: the cursor stays in range.
	_ = os.Remove(filepath.Join(dir, "c.txt"))
	m.Reload()
	if got := m.CursorPath(); got == "" {
		t.Fatal("cursor fell off the tree")
	}
}
