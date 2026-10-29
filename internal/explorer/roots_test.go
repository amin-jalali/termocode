package explorer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

func mkTree(t *testing.T, root string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		p := filepath.Join(root, rel)
		if strings.HasSuffix(rel, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(rel), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNewFollowsWorkingFolderOrReportsError(t *testing.T) {
	dir := t.TempDir()
	mkTree(t, dir, "a.txt")
	t.Chdir(dir)
	m := New()
	if m.PrimaryRoot() == "" || len(m.Roots()) != 1 || m.Init() != nil {
		t.Fatalf("roots %v", m.Roots())
	}
	// The working folder was deleted: the explorer shows an error.
	gone := t.TempDir()
	t.Chdir(gone)
	if err := os.Remove(gone); err != nil {
		t.Skip("cannot remove the working folder here")
	}
	bad := New()
	bad.SetSize(40, 10)
	if bad.PrimaryRoot() != "" || !strings.Contains(bad.View(true, "", false, nil), "explorer error") {
		t.Fatalf("view %q", bad.View(true, "", false, nil))
	}
	// Nothing to show: keys, reload and reveal do nothing.
	bad.Reload()
	bad.ExpandPaths([]string{"/x"})
	bad.RevealPath("/x")
	if n, cmd := bad.Update(tea.KeyMsg{Type: tea.KeyDown}); cmd != nil || n.CursorPath() != "" {
		t.Fatal("keys on an empty tree")
	}
}

func TestReloadDropsVanishedRootsAndKeepsCursor(t *testing.T) {
	dir := t.TempDir()
	mkTree(t, dir, "a/x.go", "b.txt", "c.txt")
	extra := t.TempDir()
	mkTree(t, extra, "e.txt")
	t.Chdir(dir)
	m := newModelAt(t, dir)
	if err := m.AddRoot(extra); err != nil {
		t.Fatal(err)
	}
	if err := m.AddRoot(extra); err != nil || len(m.Roots()) != 2 {
		t.Fatalf("second add of the same root: %v %v", err, m.Roots())
	}
	if err := m.AddRoot(dir); err != nil || len(m.Roots()) != 2 {
		t.Fatal("adding the primary again")
	}
	m.ExpandPaths([]string{filepath.Join(dir, "a")})
	m.RevealPath(filepath.Join(dir, "c.txt"))
	if m.CursorPath() != filepath.Join(dir, "c.txt") {
		t.Fatalf("cursor %q", m.CursorPath())
	}
	// A file appears above the cursor and the extra root vanishes.
	mkTree(t, dir, "a0.txt")
	if err := os.RemoveAll(extra); err != nil {
		t.Fatal(err)
	}
	m.Reload()
	if m.CursorPath() != filepath.Join(dir, "c.txt") {
		t.Fatalf("cursor moved to %q", m.CursorPath())
	}
	if len(m.Roots()) != 1 {
		t.Fatalf("vanished root kept: %v", m.Roots())
	}
	if exp := m.ExpandedPaths(); len(exp) != 2 {
		t.Fatalf("expanded %v", exp)
	}
	// Everything but the root goes away: the cursor stays in range.
	m.RevealPath(filepath.Join(dir, "c.txt"))
	for _, n := range []string{"a", "a0.txt", "b.txt", "c.txt"} {
		os.RemoveAll(filepath.Join(dir, n))
	}
	m.Reload()
	if m.CursorPath() != dir {
		t.Fatalf("cursor %q after the files went", m.CursorPath())
	}
}

func TestRemoveRootAndReveal(t *testing.T) {
	dir := t.TempDir()
	mkTree(t, dir, "sub/deep/f.txt", "file.txt")
	extra := t.TempDir()
	mkTree(t, extra, "x.txt")
	m := newModelAt(t, dir)
	if m.RemoveRoot(dir) {
		t.Fatal("primary root removed")
	}
	if m.RemoveRoot(extra) {
		t.Fatal("removed a root that was never added")
	}
	_ = m.AddRoot(extra)
	m.RevealPath(filepath.Join(extra, "x.txt"))
	if !m.RemoveRoot(extra) || len(m.Roots()) != 1 {
		t.Fatal("remove extra")
	}
	if m.CursorPath() == "" {
		t.Fatal("cursor out of range after removing the root it was in")
	}
	m.RevealPath(filepath.Join(dir, "sub", "deep", "f.txt"))
	if m.CursorPath() != filepath.Join(dir, "sub", "deep", "f.txt") {
		t.Fatalf("reveal deep: %q", m.CursorPath())
	}
	before := m.CursorPath()
	for _, p := range []string{"", "/elsewhere/x", filepath.Join(dir, "file.txt", "below"), filepath.Join(dir, "sub", "nope.txt")} {
		m.RevealPath(p)
		if m.CursorPath() != before {
			t.Fatalf("reveal %q moved the cursor to %q", p, m.CursorPath())
		}
	}
}

func TestTreeLoadErrors(t *testing.T) {
	if _, err := NewRoot(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing root")
	}
	file := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(file, nil, 0o644)
	n, err := NewRoot(file)
	if err != nil || n.IsDir || n.Load() != nil || n.Toggle() != nil {
		t.Fatalf("file root %+v %v", n, err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads any folder")
	}
	locked := filepath.Join(t.TempDir(), "locked")
	os.Mkdir(locked, 0o000)
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	if _, err := NewRoot(locked); err == nil {
		t.Fatal("unreadable root")
	}
	d := &Node{Path: locked, IsDir: true}
	if d.Toggle() == nil || d.Expanded {
		t.Fatal("toggle of an unreadable folder")
	}
}

func TestRenameEdgeCases(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	mkTree(t, dir, "a", "sub/x")
	if err := ApplyRename([]Rename{{a, filepath.Join(dir, "sub", "a")}}, nil); err == nil || !strings.Contains(err.Error(), "same folder") {
		t.Fatalf("cross-folder: %v", err)
	}
	if err := ApplyRename([]Rename{{a, filepath.Join(dir, "sub")}}, nil); err == nil || !strings.Contains(err.Error(), "name exists") {
		t.Fatalf("onto an existing name: %v", err)
	}
	p := PlanRename([]string{filepath.Join(dir, "sub"), filepath.Join(dir, "sub", "x")}, []string{"s2", "y"}, nil)
	if len(p.Conflicts) == 0 || !strings.Contains(strings.Join(p.Conflicts, ","), "folder or its files") {
		t.Fatalf("folder and child: %+v", p)
	}
	if p := CheckRename([]string{a}, []string{"bad\nname"}); len(p.Conflicts) != 1 {
		t.Fatalf("newline in name: %+v", p)
	}

	// Stamps change with content and with a link's target.
	s1, err := RenameStamp(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a", filepath.Join(dir, "link")); err == nil {
		s2, _ := RenameStamp(dir)
		os.Remove(filepath.Join(dir, "link"))
		os.Symlink("sub", filepath.Join(dir, "link"))
		s3, _ := RenameStamp(dir)
		if s1 == s2 || s2 == s3 {
			t.Fatal("stamp ignores links")
		}
	}
	if _, err := RenameStamp(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("stamp of a missing path")
	}
	if os.Geteuid() != 0 {
		secret := filepath.Join(dir, "secret")
		os.WriteFile(secret, []byte("s"), 0o000)
		if _, err := RenameStamp(secret); err == nil {
			t.Fatal("stamp of an unreadable file")
		}
		os.Chmod(secret, 0o644)
	}
}
