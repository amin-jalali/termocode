package explorer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandPathsReadsOnlyNeededFolders(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"a/b/c", "d/e"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	root, err := NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "a", "b")
	root.ExpandPaths(map[string]bool{target: true})
	find := func(n *Node, name string) *Node {
		for _, c := range n.Children {
			if c.Name == name {
				return c
			}
		}
		t.Fatalf("%s not found under %s", name, n.Path)
		return nil
	}
	a := find(root, "a")
	if !a.Loaded || a.Expanded {
		t.Errorf("a: loaded=%v expanded=%v, want loaded and collapsed", a.Loaded, a.Expanded)
	}
	b := find(a, "b")
	if !b.Loaded || !b.Expanded {
		t.Errorf("a/b: loaded=%v expanded=%v, want both", b.Loaded, b.Expanded)
	}
	if c := find(b, "c"); c.Loaded {
		t.Error("a/b/c was read but is not needed")
	}
	if d := find(root, "d"); d.Loaded {
		t.Error("d was read but is not needed")
	}
}
