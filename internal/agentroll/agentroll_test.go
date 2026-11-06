package agentroll

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteListRemove(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents")
	a := Rollup{Workspace: "/w/a", PID: 11, Items: []Item{{Name: "Claude Code", State: "waiting"}, {Name: "run x", State: "working"}}}
	b := Rollup{Workspace: "/w/b", PID: 22, Items: []Item{{Name: "run y", State: "working"}}}
	for _, r := range []Rollup{a, b, {Workspace: "/w/me", PID: 33, Items: []Item{{Name: "z", State: "working"}}}} {
		if err := Write(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	if fi, err := os.Stat(filepath.Join(dir, "11.json")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file %v %v", fi, err)
	}
	got := List(dir, 33, func(pid int) bool { return pid != 22 })
	if len(got) != 1 || got[0].Workspace != "/w/a" || got[0].Line() != "1 waiting for you · 1 working" {
		t.Fatalf("list %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "22.json")); !os.IsNotExist(err) {
		t.Fatal("a dead window's file stays")
	}
	if err := Write(dir, Rollup{Workspace: "/w/a", PID: 11, Items: []Item{{Name: "x", State: "done"}}}); err != nil {
		t.Fatal(err)
	}
	if got := List(dir, 33, nil); len(got) != 0 {
		t.Fatalf("after idle %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "11.json")); !os.IsNotExist(err) {
		t.Fatal("an idle window keeps its file")
	}
}

// An item carries the folder its agent works in (WTX-2), so the WORKTREES
// view of another window can show the agent on that worktree.
func TestItemWorktree(t *testing.T) {
	dir := t.TempDir()
	in := Rollup{Workspace: "/w/a", PID: 44, Items: []Item{{Name: "Codex", State: "waiting", Worktree: "/w/a.worktrees/fix"}}}
	if err := Write(dir, in); err != nil {
		t.Fatal(err)
	}
	got := List(dir, 0, nil)
	if len(got) != 1 || got[0].Items[0].Worktree != "/w/a.worktrees/fix" {
		t.Fatalf("got %+v", got)
	}
}
