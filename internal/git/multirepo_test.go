package git

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// GITX-31 (M15, M16): FindRepos lists the repositories of a folder: the
// folder itself when it is one, and repos nested below it. Build and
// package folders are not walked, and the depth is capped.
func TestFindRepos(t *testing.T) {
	root := newGitTestRepo(t)
	root.write(t, "a.txt", "a\n")
	mk := func(rel string) {
		t.Helper()
		d := filepath.Join(root.dir, rel)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		r := &gitTestRepo{dir: d}
		r.git(t, "init", "-q")
	}
	mk("libs/one")
	mk("libs/two")
	mk("node_modules/pkg") // skipped
	mk("a/b/c/d/deep")     // deeper than the cap

	got := FindRepos(root.dir, 3)
	want := []string{root.dir, filepath.Join(root.dir, "libs/one"), filepath.Join(root.dir, "libs/two")}
	if !slices.Equal(got, want) {
		t.Fatalf("FindRepos = %q, want %q", got, want)
	}

	// A folder that is no repo, with repos inside: only the inside ones.
	plain := t.TempDir()
	for _, n := range []string{"zeta", "alpha"} {
		d := filepath.Join(plain, n)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		(&gitTestRepo{dir: d}).git(t, "init", "-q")
	}
	got = FindRepos(plain, 3)
	want = []string{filepath.Join(plain, "alpha"), filepath.Join(plain, "zeta")}
	if !slices.Equal(got, want) {
		t.Fatalf("plain folder: %q, want %q", got, want)
	}
	if got := FindRepos(t.TempDir(), 3); len(got) != 0 {
		t.Errorf("empty folder: %q", got)
	}
}

// GITX-31 (M8): parseSubmoduleStatus reads `git submodule status`.
func TestParseSubmoduleStatus(t *testing.T) {
	out := " 1111111111111111111111111111111111111111 libs/ok (v1.0)\n" +
		"-2222222222222222222222222222222222222222 libs/new\n" +
		"+3333333333333333333333333333333333333333 libs/moved (heads/main)\n" +
		"U4444444444444444444444444444444444444444 libs/conflict\n" +
		"\n"
	got := parseSubmoduleStatus(out)
	want := []Submodule{
		{Path: "libs/ok", SHA: "1111111111111111111111111111111111111111", State: SubmoduleOK, Describe: "v1.0"},
		{Path: "libs/new", SHA: "2222222222222222222222222222222222222222", State: SubmoduleNotInit},
		{Path: "libs/moved", SHA: "3333333333333333333333333333333333333333", State: SubmoduleChanged, Describe: "heads/main"},
		{Path: "libs/conflict", SHA: "4444444444444444444444444444444444444444", State: SubmoduleConflict},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

// GITX-31 (M8): Submodules lists a repo's submodules with their state,
// and SubmoduleUpdate checks out the recorded commit (with init for a
// new one).
func TestSubmodulesAndUpdate(t *testing.T) {
	lib := newGitTestRepo(t)
	lib.write(t, "lib.txt", "1\n")
	lib.git(t, "add", ".")
	lib.git(t, "commit", "-q", "-m", "lib one")

	r := newGitTestRepo(t)
	r.write(t, "a.txt", "a\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "first")
	if got, err := Submodules(r.dir); err != nil || len(got) != 0 {
		t.Fatalf("no submodules yet: %+v %v", got, err)
	}
	r.git(t, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib.dir, "libs/lib")
	r.git(t, "commit", "-q", "-m", "add lib")

	got, err := Submodules(r.dir)
	if err != nil || len(got) != 1 || got[0].Path != "libs/lib" || got[0].State != SubmoduleOK {
		t.Fatalf("after add: %+v %v", got, err)
	}

	// Move the submodule's HEAD: it shows as changed; update puts it back.
	sub := &gitTestRepo{dir: filepath.Join(r.dir, "libs/lib")}
	sub.write(t, "lib.txt", "2\n")
	sub.git(t, "commit", "-q", "-am", "lib two")
	if got, _ := Submodules(r.dir); len(got) != 1 || got[0].State != SubmoduleChanged {
		t.Fatalf("after moving HEAD: %+v", got)
	}
	if err := SubmoduleUpdate(r.dir, "libs/lib"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Submodules(r.dir); len(got) != 1 || got[0].State != SubmoduleOK {
		t.Fatalf("after update: %+v", got)
	}

	// Deinit: not initialized; update all inits it again.
	r.git(t, "submodule", "deinit", "-q", "-f", "libs/lib")
	if got, _ := Submodules(r.dir); len(got) != 1 || got[0].State != SubmoduleNotInit {
		t.Fatalf("after deinit: %+v", got)
	}
	if err := SubmoduleUpdate(r.dir, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := Submodules(r.dir); len(got) != 1 || got[0].State != SubmoduleOK {
		t.Fatalf("after update all: %+v", got)
	}

	// A path that is no submodule is refused before git runs.
	if err := SubmoduleUpdate(r.dir, "-x"); err == nil {
		t.Error("an option as path was accepted")
	}
}
