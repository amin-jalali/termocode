package git

import (
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
