package aimemory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covWorktree lays out what git makes for a linked worktree: the main
// checkout, its .git with a registration, and the linked folder.
func covWorktree(t *testing.T) (main, wt, admin string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	main, wt = filepath.Join(base, "main"), filepath.Join(base, "wt")
	admin = filepath.Join(main, ".git", "worktrees", "wt")
	for _, d := range []string{admin, wt} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w(filepath.Join(main, ".git", "HEAD"), "ref: refs/heads/main\n")
	w(filepath.Join(admin, "gitdir"), filepath.Join(wt, ".git")+"\n")
	w(filepath.Join(admin, "commondir"), "../..\n")
	w(filepath.Join(wt, ".git"), "gitdir: "+admin+"\n")
	return main, wt, admin
}

func TestSharedRootFindsMainOnlyWhenGitAgrees(t *testing.T) {
	main, wt, _ := covWorktree(t)
	if got := SharedRoot(wt); got != main {
		t.Fatalf("SharedRoot = %q, want %q", got, main)
	}
	if got := SharedRoot(main); got != "" {
		t.Errorf("the main checkout has no shared root, got %q", got)
	}
	if got := SharedRoot(filepath.Join(wt, "missing")); got != "" {
		t.Errorf("a missing folder: %q", got)
	}
}

func TestSharedRootRefusesForgedPointers(t *testing.T) {
	cases := map[string]func(t *testing.T, main, wt, admin string){
		"no backlink": func(t *testing.T, _, _, admin string) { _ = os.Remove(filepath.Join(admin, "gitdir")) },
		"backlink elsewhere": func(t *testing.T, _, _, admin string) {
			_ = os.WriteFile(filepath.Join(admin, "gitdir"), []byte("/somewhere/else/.git"), 0o644)
		},
		"no commondir": func(t *testing.T, _, _, admin string) { _ = os.Remove(filepath.Join(admin, "commondir")) },
		"commondir not a .git": func(t *testing.T, main, _, admin string) {
			_ = os.WriteFile(filepath.Join(admin, "commondir"), []byte("../../.."), 0o644)
		},
		"no HEAD in the common dir": func(t *testing.T, main, _, _ string) { _ = os.Remove(filepath.Join(main, ".git", "HEAD")) },
		"admin dir gone":            func(t *testing.T, _, _, admin string) { _ = os.RemoveAll(admin) },
		"pointer is not gitdir": func(t *testing.T, _, wt, _ string) {
			_ = os.WriteFile(filepath.Join(wt, ".git"), []byte("something else"), 0o644)
		},
		"pointer too big": func(t *testing.T, _, wt, admin string) {
			_ = os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+admin+strings.Repeat(" ", 70<<10)), 0o644)
		},
		"git file is a directory": func(t *testing.T, _, wt, _ string) {
			_ = os.Remove(filepath.Join(wt, ".git"))
			_ = os.Mkdir(filepath.Join(wt, ".git"), 0o755)
		},
		"relative pointer to nowhere": func(t *testing.T, _, wt, _ string) {
			_ = os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: ../nowhere"), 0o644)
		},
	}
	for name, mutate := range cases {
		main, wt, admin := covWorktree(t)
		mutate(t, main, wt, admin)
		if got := SharedRoot(wt); got != "" {
			t.Errorf("%s: SharedRoot = %q, want none", name, got)
		}
	}
	// the unreadable-file path of metadataText
	main, wt, _ := covWorktree(t)
	_ = main
	if metadataText(filepath.Join(wt, "nope")) != "" {
		t.Error("a missing file has no text")
	}
}

func TestLoadForWorktreeKeepsLocalOnSharedTrouble(t *testing.T) {
	main, wt, _ := covWorktree(t)
	// the main checkout's memory folder is a file: shared lessons can not
	// be read, the worktree's own are still returned
	if err := os.MkdirAll(filepath.Join(main, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Dir(main), []byte("x"), 0o644); err != nil {
		t.Skip("memory dir of this layout is not a plain path")
	}
	got, err := LoadForWorktree(wt)
	if err != nil || len(got) != 0 {
		t.Errorf("got %v %v", got, err)
	}
}

func TestEnsureIgnoredReportsWriteProblems(t *testing.T) {
	dir := t.TempDir()
	// the .gitignore is a directory: it can not be read as a file
	if err := os.Mkdir(filepath.Join(dir, ".gitignore"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureIgnored(dir); err == nil {
		t.Error("a directory named .gitignore must be an error")
	}
	// a read-only file the line is missing from
	dir = t.TempDir()
	p := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(p, []byte("node_modules"), 0o444); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 {
		if _, err := EnsureIgnored(dir); err == nil {
			t.Error("a read-only .gitignore must be an error")
		}
	}
	b, _ := os.ReadFile(p)
	if string(b) != "node_modules" {
		t.Errorf("the file changed: %q", b)
	}
}
