package git

import (
	"os"
	"path/filepath"
	"testing"
)

// GITX-30: AddIgnore appends a pattern to .gitignore (made when missing,
// a newline added to a last line without one, a pattern already there
// not added twice).
func TestAddIgnore(t *testing.T) {
	r := newGitTestRepo(t)
	if err := AddIgnore(r.dir, "/build/"); err != nil {
		t.Fatal(err)
	}
	r.write(t, ".gitignore", "/build/\n*.log")
	for _, p := range []string{"*.tmp", "/build/", "*.tmp"} {
		if err := AddIgnore(r.dir, p); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := os.ReadFile(filepath.Join(r.dir, ".gitignore"))
	if string(got) != "/build/\n*.log\n*.tmp\n" {
		t.Errorf(".gitignore is %q", got)
	}
	for _, bad := range []string{"", "  ", "a\nb"} {
		if err := AddIgnore(r.dir, bad); err == nil {
			t.Errorf("%q added", bad)
		}
	}
}

// A tracked file stays tracked after it is ignored; Untrack stops that
// and keeps the file on disk.
func TestUntrackKeepsFile(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "secret.env", "x\n")
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "first")
	if !IsTracked(r.dir, "secret.env") || IsTracked(r.dir, "nope.txt") {
		t.Fatal("IsTracked")
	}
	if err := Untrack(r.dir, "secret.env"); err != nil {
		t.Fatal(err)
	}
	if IsTracked(r.dir, "secret.env") {
		t.Error("still tracked")
	}
	if _, err := os.Stat(filepath.Join(r.dir, "secret.env")); err != nil {
		t.Error("the file was deleted")
	}
}

// GITX-30: the identity is read and set for this repository, or for all
// of them (the global config).
func TestIdentity(t *testing.T) {
	r := newGitTestRepo(t)
	if n, e := Identity(r.dir); n != "Test" || e != "test@example.com" {
		t.Errorf("identity %q %q", n, e)
	}
	if err := SetIdentity(r.dir, "Ann Lee", "ann@example.com", false); err != nil {
		t.Fatal(err)
	}
	if n, e := Identity(r.dir); n != "Ann Lee" || e != "ann@example.com" {
		t.Errorf("local identity %q %q", n, e)
	}
	global := filepath.Join(t.TempDir(), "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	if err := SetIdentity(r.dir, "Glo Bal", "g@example.com", true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(global)
	if want := "[user]\n\tname = Glo Bal\n\temail = g@example.com\n"; string(b) != want {
		t.Errorf("global config %q", b)
	}
	for _, bad := range [][2]string{{"", "a@b"}, {"A", ""}, {"A\nB", "a@b"}, {"A", "no-at"}} {
		if err := SetIdentity(r.dir, bad[0], bad[1], false); err == nil {
			t.Errorf("%q set", bad)
		}
	}
}
