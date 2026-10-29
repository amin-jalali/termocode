package explorer

import (
	"context"
	"testing"

	"github.com/amin-jalali/termocode/internal/containerhost"
	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
)

// fakeContainer is a container host on the fake engine with /app holding
// two folders and two files.
func fakeContainer(t *testing.T, noShell bool) (*containerhost.Host, *fakedocker.Docker, *fakedocker.Container) {
	t.Helper()
	d := fakedocker.New()
	c := d.Add(t, "api-1")
	c.Mkdir(t, "/app/zsub")
	c.Mkdir(t, "/app/asub")
	c.WriteFile(t, "/app/b.txt", []byte("b"), 0o644)
	c.WriteFile(t, "/app/asub/a.go", []byte("package a"), 0o644)
	c.WriteFile(t, "/app/main.go", []byte("package main"), 0o644)
	if noShell {
		c.NoShell()
	}
	h, err := containerhost.New(context.Background(), containerhost.Config{Container: "api-1", Root: "/app", X: d})
	if err != nil {
		t.Fatal(err)
	}
	return h, d, c
}

func childNames(n *Node) []string {
	var out []string
	for _, c := range n.Children {
		out = append(out, c.Name)
	}
	return out
}

func TestNewRootFSListsTheContainer(t *testing.T) {
	for _, noShell := range []bool{false, true} {
		h, d, _ := fakeContainer(t, noShell)
		before := len(d.Calls())
		root, err := NewRootFS(h.FS(), "/app")
		if err != nil {
			t.Fatal(err)
		}
		if !root.IsDir || !root.Expanded || root.Path != "/app" || root.Name != "app" {
			t.Fatalf("root = %+v", root)
		}
		got := childNames(root)
		want := []string{"asub", "zsub", "b.txt", "main.go"}
		if len(got) != len(want) {
			t.Fatalf("noShell %v: children = %v, want %v", noShell, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("noShell %v: children = %v, want %v", noShell, got, want)
			}
		}
		if root.Children[0].Path != "/app/asub" || !root.Children[0].IsDir {
			t.Fatalf("asub = %+v", root.Children[0])
		}
		// One stat of the root and one listing: never one call per entry
		// (without sh each call reads a whole tar).
		if n := len(d.Calls()) - before; n > 2 {
			t.Fatalf("noShell %v: %d engine calls for one folder, want <= 2", noShell, n)
		}
		// Children list through the same FS.
		if err := root.Children[0].Toggle(); err != nil {
			t.Fatal(err)
		}
		if got := childNames(root.Children[0]); len(got) != 1 || got[0] != "a.go" || root.Children[0].Children[0].Path != "/app/asub/a.go" {
			t.Fatalf("asub children = %v", got)
		}
	}
}

func TestNewFSReloadStaysInTheContainer(t *testing.T) {
	h, _, c := fakeContainer(t, false)
	m := NewFS(h.FS(), "/app")
	if m.err != "" || m.PrimaryRoot() != "/app" {
		t.Fatalf("model err %q root %q", m.err, m.PrimaryRoot())
	}
	m.ExpandPaths([]string{"/app/asub"})
	c.WriteFile(t, "/app/new.txt", []byte(""), 0o644)
	c.WriteFile(t, "/app/asub/b.go", []byte(""), 0o644)
	m.Reload()
	if m.PrimaryRoot() != "/app" {
		t.Fatalf("Reload moved the root to %q", m.PrimaryRoot())
	}
	if got := childNames(m.root); len(got) != 5 || got[4] != "new.txt" {
		t.Fatalf("root after reload = %v", got)
	}
	asub := m.root.Children[0]
	if !asub.Expanded || len(asub.Children) != 2 || asub.FS() == nil {
		t.Fatalf("asub after reload = %+v", asub)
	}
}

func TestNewFSShowsAnError(t *testing.T) {
	h, _, c := fakeContainer(t, false)
	c.Stop()
	m := NewFS(h.FS(), "/app")
	if m.err == "" {
		t.Fatal("no error for a stopped container")
	}
}
