package search

import (
	"path/filepath"
	"testing"
)

func TestDisplayPathRelativeToRoot(t *testing.T) {
	root := filepath.FromSlash("/w/proj")
	m := New()
	m.SetRoots([]string{root})
	if got, want := m.displayPath(filepath.Join(root, "internal", "a.go")), filepath.FromSlash("internal/a.go"); got != want {
		t.Errorf("one root: got %q, want %q", got, want)
	}
	other := filepath.FromSlash("/elsewhere/b.go")
	if got := m.displayPath(other); got != other {
		t.Errorf("outside root: got %q, want %q", got, other)
	}
	m.SetRoots([]string{root, filepath.FromSlash("/w/lib")})
	if got, want := m.displayPath(filepath.FromSlash("/w/lib/c.go")), filepath.FromSlash("lib/c.go"); got != want {
		t.Errorf("two roots: got %q, want %q", got, want)
	}
}
