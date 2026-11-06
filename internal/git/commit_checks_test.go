package git

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// GITX-27: the before-commit checks read what is staged (sizes and added
// lines), a commit can skip the hooks (--no-verify), and the app knows
// when the repository has commit hooks.
func TestStagedFilesAndAddedLines(t *testing.T) {
	r := newGitTestRepo(t)
	dir := r.dir
	r.write(t, "a.txt", "one\ntwo\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-q", "-m", "first")

	r.write(t, "a.txt", "one\nTWO\nthree\n")
	r.write(t, "big.bin", strings.Repeat("x", 3000))
	r.write(t, "sub/new file.go", "package sub\n")
	r.write(t, "unstaged.txt", "not staged\n")
	r.git(t, "add", "a.txt", "big.bin", "sub/new file.go")

	files, err := StagedFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, f := range files {
		got[f.Path] = f.Size
	}
	if len(got) != 3 || got["a.txt"] != int64(len("one\nTWO\nthree\n")) || got["big.bin"] != 3000 || got["sub/new file.go"] != int64(len("package sub\n")) {
		t.Fatalf("staged files %v", got)
	}

	lines, err := StagedAddedLines(dir)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.Path + ":" + strconv.Itoa(l.Line) + ":" + l.Text + "\n")
	}
	want := "a.txt:2:TWO\na.txt:3:three\nbig.bin:1:" + strings.Repeat("x", 3000) + "\nsub/new file.go:1:package sub\n"
	if b.String() != want {
		t.Fatalf("added lines:\n%s\nwant:\n%s", b.String(), want)
	}

	// A file with unstaged edits too is "partly staged".
	r.write(t, "a.txt", "one\nTWO\nthree\nfour\n")
	part, err := UnstagedPaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(part) != 1 || part[0] != "a.txt" {
		t.Fatalf("unstaged %v", part)
	}
}

func TestCommitNoVerifySkipsHooks(t *testing.T) {
	r := newGitTestRepo(t)
	dir := r.dir
	if HasCommitHooks(dir) {
		t.Fatal("a new repository has no commit hooks")
	}
	hooks := filepath.Join(dir, ".git", "hooks")
	_ = os.MkdirAll(hooks, 0o755)
	// A sample hook is not a hook.
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit.sample"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if HasCommitHooks(dir) {
		t.Fatal("a .sample file counts as a hook")
	}
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\necho 'lint: 2 problems in a.txt'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !HasCommitHooks(dir) {
		t.Fatal("the pre-commit hook is not seen")
	}
	r.write(t, "a.txt", "one\n")
	r.git(t, "add", "a.txt")

	err := CommitOpts(dir, "first", CommitOptions{})
	var ce *CmdError
	if !errors.As(err, &ce) || !strings.Contains(ce.Output, "lint: 2 problems in a.txt") {
		t.Fatalf("hook failure: %v", err)
	}
	if err := CommitOpts(dir, "first", CommitOptions{NoVerify: true}); err != nil {
		t.Fatalf("--no-verify: %v", err)
	}
	if out := r.git(t, "log", "--format=%s"); strings.TrimSpace(out) != "first" {
		t.Fatalf("log %q", out)
	}

	// core.hooksPath is honoured.
	other := t.TempDir()
	r.git(t, "config", "core.hooksPath", other)
	if HasCommitHooks(dir) {
		t.Fatal("hooks in .git/hooks count while core.hooksPath points elsewhere")
	}
	if err := os.WriteFile(filepath.Join(other, "commit-msg"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !HasCommitHooks(dir) {
		t.Fatal("the core.hooksPath hook is not seen")
	}
}
