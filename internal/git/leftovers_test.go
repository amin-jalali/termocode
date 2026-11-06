package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// GITX-39 (B10, B19): an empty commit (--allow-empty) and a commit made
// as another author (--author); a bad author is refused before git runs.
func TestCommitEmptyAndAuthor(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "one\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-q", "-m", "first")

	if err := CommitOpts(r.dir, "nothing", CommitOptions{}); err == nil {
		t.Fatal("a commit with nothing staged passed")
	}
	if err := CommitOpts(r.dir, "ci: run again", CommitOptions{AllowEmpty: true}); err != nil {
		t.Fatalf("empty commit: %v", err)
	}
	if got := strings.TrimSpace(r.git(t, "log", "-1", "--format=%s")); got != "ci: run again" {
		t.Fatalf("subject %q", got)
	}

	for _, bad := range []string{"", "Ann", "Ann <>", "<ann@example.com>", "Ann ann@example.com", "--exec=x <a@b>", "Ann <a@b> x"} {
		if _, err := ParseAuthor(bad); err == nil {
			t.Errorf("ParseAuthor(%q) passed", bad)
		}
	}
	a, err := ParseAuthor("  Ann Lee   <ann@example.com> ")
	if err != nil || a != "Ann Lee <ann@example.com>" {
		t.Fatalf("ParseAuthor = %q, %v", a, err)
	}
	r.write(t, "a.txt", "two\n")
	r.git(t, "add", "a.txt")
	if err := CommitOpts(r.dir, "by Ann", CommitOptions{Author: a}); err != nil {
		t.Fatalf("author commit: %v", err)
	}
	if got := strings.TrimSpace(r.git(t, "log", "-1", "--format=%an <%ae>|%cn")); got != "Ann Lee <ann@example.com>|Test" {
		t.Fatalf("author|committer %q", got)
	}
}

// GITX-39 (B8): CommitPaths commits only the named files (new ones too),
// as they are on disk; what else is staged stays staged.
func TestCommitPaths(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "one\n")
	r.write(t, "gone.txt", "x\n")
	r.git(t, "add", "a.txt", "gone.txt")
	r.git(t, "commit", "-q", "-m", "first")
	r.write(t, "a.txt", "two\n")
	r.write(t, "b.txt", "new\n")
	r.write(t, "c.txt", "staged\n")
	r.git(t, "add", "c.txt")
	if err := os.Remove(filepath.Join(r.dir, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	if err := CommitPaths(r.dir, "a, b and gone", []string{"a.txt", "b.txt", "gone.txt"}, CommitOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(r.git(t, "show", "--name-status", "--format=%s", "HEAD")); got != "a, b and gone\n\nM\ta.txt\nA\tb.txt\nD\tgone.txt" {
		t.Fatalf("commit:\n%s", got)
	}
	if got := strings.TrimSpace(r.git(t, "status", "--porcelain")); got != "A  c.txt" {
		t.Fatalf("status %q", got)
	}
	if err := CommitPaths(r.dir, "none", nil, CommitOptions{}); err == nil {
		t.Error("a commit of no files passed")
	}
}

// GITX-39 (E28): Contributors counts the commits of each author (by
// name and e-mail), most commits first.
func TestContributors(t *testing.T) {
	r := newGitTestRepo(t)
	for i, who := range []string{"Ann <ann@example.com>", "Bo <bo@example.com>", "Ann <ann@example.com>"} {
		r.write(t, "a.txt", strings.Repeat("x", i+1))
		r.git(t, "add", "a.txt")
		r.git(t, "commit", "-q", "--author="+who, "-m", "c")
	}
	got, err := Contributors(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (Contributor{Name: "Ann", Email: "ann@example.com", Commits: 2}) || got[1] != (Contributor{Name: "Bo", Email: "bo@example.com", Commits: 1}) {
		t.Fatalf("contributors %+v", got)
	}
}

// GITX-39 (M11): a shallow clone (Depth) has only the last commits;
// IsShallow sees it and Unshallow fetches the rest of the history.
func TestShallowCloneAndUnshallow(t *testing.T) {
	r := newGitTestRepo(t)
	for i := 0; i < 3; i++ {
		r.write(t, "a.txt", strings.Repeat("x", i+1))
		r.git(t, "add", "a.txt")
		r.git(t, "commit", "-q", "-m", "c")
	}
	if IsShallow(r.dir) {
		t.Fatal("a full repository is shallow")
	}
	dest := filepath.Join(t.TempDir(), "clone")
	if err := Clone(CloneOptions{URL: "file://" + filepath.ToSlash(r.dir), Dest: dest, Depth: 1}); err != nil {
		t.Fatal(err)
	}
	count := func() string {
		out, err := runOut(dest, "git", "rev-list", "--count", "HEAD")
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out)
	}
	if !IsShallow(dest) || count() != "1" {
		t.Fatalf("shallow %v, %s commits", IsShallow(dest), count())
	}
	if err := Unshallow(dest); err != nil {
		t.Fatal(err)
	}
	if IsShallow(dest) || count() != "3" {
		t.Fatalf("after unshallow: shallow %v, %s commits", IsShallow(dest), count())
	}
}

// GITX-39 (P6): StagedCRLF names the staged files that bring Windows
// line endings (CRLF) into a file that had none (or a new file); a file
// that had CRLF already, an LF change and a binary file are left out.
func TestStagedCRLF(t *testing.T) {
	r := newGitTestRepo(t)
	r.git(t, "config", "core.autocrlf", "false")
	r.write(t, "lf.txt", "one\n")
	r.write(t, "win.bat", "echo\r\n")
	r.write(t, "ok.txt", "a\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "first")
	r.write(t, "lf.txt", "one\r\ntwo\r\n")
	r.write(t, "win.bat", "echo\r\necho 2\r\n")
	r.write(t, "ok.txt", "a\nb\n")
	r.write(t, "new.txt", "x\r\n")
	r.write(t, "bin.dat", "\x00\x01\r\n")
	r.write(t, "unstaged.txt", "u\r\n")
	r.git(t, "add", "lf.txt", "win.bat", "ok.txt", "new.txt", "bin.dat")
	got, err := StagedCRLF(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "lf.txt,new.txt" {
		t.Fatalf("CRLF files %v", got)
	}
}
