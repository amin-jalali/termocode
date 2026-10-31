package aimemory

import (
	"os"
	"path/filepath"
	"testing"
)

// Fixtures describe Git's linked-worktree metadata; no git process or user
// repository is needed to prove membership and read-only inheritance.
func memoryWorktree(t *testing.T) (string, string) {
	t.Helper()
	main, child := t.TempDir(), t.TempDir()
	common := filepath.Join(main, ".git")
	admin := filepath.Join(common, "worktrees", "agent")
	if err := os.MkdirAll(admin, 0700); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(child, ".git"):      "gitdir: " + admin + "\n",
		filepath.Join(admin, "commondir"): "../..\n",
		filepath.Join(admin, "gitdir"):    filepath.Join(child, ".git") + "\n",
		filepath.Join(common, "HEAD"):     "ref: refs/heads/main\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return main, child
}

func TestWorktreeMemoryReadsMainAndKeepsWritesLocal(t *testing.T) {
	main, child := memoryWorktree(t)
	shared := Lesson{ID: "shared", Date: "2026-10-31", Title: "Shared lesson", Text: "Use focused tests"}
	local := Lesson{ID: "local", Date: "2026-10-31", Title: "Local lesson", Text: "Check worktree files"}
	if err := Save(main, []Lesson{shared}); err != nil {
		t.Fatal(err)
	}
	if err := Save(child, []Lesson{local}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(Path(main))
	got, err := LoadForWorktree(child)
	if err != nil || len(got) != 2 {
		t.Fatalf("shared lessons: %v %v", got, err)
	}
	Save(child, nil)
	after, _ := os.ReadFile(Path(main))
	if string(after) != string(before) {
		t.Fatal("main lessons changed")
	}
	got, _ = LoadForWorktree(child)
	if len(got) != 1 || got[0].ID != "shared" {
		t.Fatal("main memory missing after local clear")
	}
}

func TestWorktreeMemoryRejectsUnregisteredAndLinkedMemory(t *testing.T) {
	main, child := memoryWorktree(t)
	Save(main, []Lesson{{ID: "shared", Date: "2026-10-31", Title: "Shared", Text: "Secret text"}})
	// A forged .git pointer cannot read another worktree's main lessons.
	fake := t.TempDir()
	b, _ := os.ReadFile(filepath.Join(child, ".git"))
	os.WriteFile(filepath.Join(fake, ".git"), b, 0600)
	if ls, _ := LoadForWorktree(fake); len(ls) != 0 {
		t.Fatal("unregistered worktree inherited memory")
	}
	// Do not follow a repo-controlled memory directory to a private folder.
	out := t.TempDir()
	Save(out, []Lesson{{ID: "outside", Date: "2026-10-31", Title: "Private", Text: "Private"}})
	os.RemoveAll(Dir(main))
	os.Symlink(Dir(out), Dir(main))
	if ls, _ := LoadForWorktree(child); len(ls) != 0 {
		t.Fatal("followed linked shared memory")
	}
}
