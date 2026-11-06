package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// GITX-6 (B11): RecentMessages lists the last full messages, newest
// first, without repeats.
func TestRecentMessages(t *testing.T) {
	r := newGitTestRepo(t)
	for i, m := range []string{"one", "two\n\nbody of two", "one"} {
		r.write(t, "f.txt", strings.Repeat("x", i+1))
		r.git(t, "add", "f.txt")
		r.git(t, "commit", "-q", "-m", m)
	}
	got, err := RecentMessages(r.dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "one|two\n\nbody of two" {
		t.Errorf("messages %q", got)
	}
	if got, _ := RecentMessages(t.TempDir(), 5); got != nil {
		t.Errorf("no repo: %q", got)
	}
}

// GITX-6 (B12): CommitTemplate reads commit.template (relative to the
// repo, or ~) without its comment lines; no template is "".
func TestCommitTemplate(t *testing.T) {
	r := newGitTestRepo(t)
	if got, err := CommitTemplate(r.dir); err != nil || got != "" {
		t.Fatalf("no template: %q %v", got, err)
	}
	r.write(t, ".gitmessage", "feat: \n\n# Why is this change needed?\nRefs: \n")
	r.git(t, "config", "commit.template", ".gitmessage")
	got, err := CommitTemplate(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "feat: \n\nRefs: " { // the space after the prefix stays: the caret goes there
		t.Errorf("template %q", got)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "tpl.txt"), []byte("fix: "), 0o644); err != nil {
		t.Fatal(err)
	}
	r.git(t, "config", "commit.template", "~/tpl.txt")
	if got, _ := CommitTemplate(r.dir); got != "fix: " {
		t.Errorf("home template %q", got)
	}
	r.git(t, "config", "commit.template", "missing.txt")
	if _, err := CommitTemplate(r.dir); err == nil {
		t.Error("no error for a missing template file")
	}
}

// GITX-6 (B15): CleanMessage drops comment lines and the scissors part,
// and trims blank lines at the ends (spaces at the end of the last line
// stay).
func TestCleanMessage(t *testing.T) {
	in := "\n# top\nfeat: x\n\nbody\n# note\n\n# ------------------------ >8 ------------------------\ndiff --git a b\n"
	if got := CleanMessage(in); got != "feat: x\n\nbody" {
		t.Errorf("clean %q", got)
	}
}

// GITX-6 (B16): Authors lists who committed, newest first, one row per
// email; AddTrailer appends a trailer once, after a blank line.
func TestAuthorsAndTrailer(t *testing.T) {
	r := newGitTestRepo(t)
	for _, a := range []string{"Ann <ann@x.io>", "Bo <bo@x.io>", "Ann Again <ann@x.io>"} {
		r.write(t, "f.txt", a)
		r.git(t, "add", "f.txt")
		r.git(t, "commit", "-q", "--author", a, "-m", "by "+a)
	}
	got := Authors(r.dir, 100)
	if strings.Join(got, "|") != "Ann Again <ann@x.io>|Bo <bo@x.io>" {
		t.Errorf("authors %q", got)
	}
	msg := AddTrailer("feat: x", "Co-authored-by", "Bo <bo@x.io>")
	if msg != "feat: x\n\nCo-authored-by: Bo <bo@x.io>" {
		t.Errorf("first trailer %q", msg)
	}
	msg = AddTrailer(msg, "Co-authored-by", "Ann <ann@x.io>")
	if msg != "feat: x\n\nCo-authored-by: Bo <bo@x.io>\nCo-authored-by: Ann <ann@x.io>" {
		t.Errorf("second trailer %q", msg)
	}
	if again := AddTrailer(msg, "Co-authored-by", "Bo <bo@x.io>"); again != msg {
		t.Errorf("trailer added twice %q", again)
	}
}

// GITX-8: ChangesDiff is the staged diff, or every change against HEAD,
// cut at max bytes; untracked files are named.
func TestChangesDiff(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "a\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "first")
	r.write(t, "a.txt", "a\nstaged\n")
	r.git(t, "add", "a.txt")
	r.write(t, "a.txt", "a\nstaged\nnot staged\n")
	r.write(t, "new.txt", "n\n")
	d, cut, err := ChangesDiff(r.dir, true, 1<<20)
	if err != nil || cut {
		t.Fatal(err, cut)
	}
	if !strings.Contains(d, "+staged") || strings.Contains(d, "not staged") {
		t.Errorf("staged diff %q", d)
	}
	d, _, _ = ChangesDiff(r.dir, false, 1<<20)
	if !strings.Contains(d, "+not staged") || !strings.Contains(d, "new.txt") {
		t.Errorf("all changes %q", d)
	}
	if d, cut, _ := ChangesDiff(r.dir, false, 40); !cut || len(d) > 40 {
		t.Errorf("not cut: %d %v", len(d), cut)
	}
}
