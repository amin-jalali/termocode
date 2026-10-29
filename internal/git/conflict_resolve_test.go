package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func unmerged(t *testing.T, r *gitTestRepo) string {
	t.Helper()
	return strings.TrimSpace(r.git(t, "ls-files", "-u"))
}

func TestMarkResolvedRefusesMarkers(t *testing.T) {
	r := forkRepo(t, "a\nx\nb\n", "a\nours\nb\n", "a\ntheirs\nb\n")
	if r.try("merge", "--no-edit", "feature") == nil {
		t.Fatal("merge did not conflict")
	}
	err := MarkResolved(r.dir, "f.txt")
	if !errors.Is(err, ErrMarkersLeft) || !strings.Contains(err.Error(), "1 left") {
		t.Fatalf("err = %v", err)
	}
	if unmerged(t, r) == "" {
		t.Fatal("file was staged with markers")
	}
	r.write(t, "f.txt", "a\nfixed\nb\n")
	if err := MarkResolved(r.dir, "f.txt"); err != nil {
		t.Fatal(err)
	}
	if u := unmerged(t, r); u != "" {
		t.Errorf("still unmerged: %s", u)
	}
	if got := r.git(t, "show", ":f.txt"); got != "a\nfixed\nb\n" {
		t.Errorf("index = %q", got)
	}
}

func TestResolveWithSideBinary(t *testing.T) {
	r := forkRepo(t, "a\x00b", "a\x00ours", "a\x00theirs")
	if r.try("merge", "--no-edit", "feature") == nil {
		t.Fatal("merge did not conflict")
	}
	if err := ResolveWithSide(r.dir, "f.txt", "theirs"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(r.dir, "f.txt"))
	if string(b) != "a\x00theirs" || unmerged(t, r) != "" {
		t.Errorf("file = %q, unmerged %q", b, unmerged(t, r))
	}
}

func TestResolveWithSideDeleted(t *testing.T) {
	setup := func() *gitTestRepo {
		r := forkRepo(t, "a\n", "a\nours\n", "a\ntheirs\n")
		r.git(t, "checkout", "-q", "feature")
		r.git(t, "rm", "-q", "f.txt")
		r.git(t, "commit", "-q", "-m", "delete")
		r.git(t, "checkout", "-q", "main")
		if r.try("merge", "--no-edit", "feature") == nil {
			t.Fatal("merge did not conflict")
		}
		return r
	}
	// Keeping ours keeps the file.
	r := setup()
	if err := ResolveWithSide(r.dir, "f.txt", "ours"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(r.dir, "f.txt")); string(b) != "a\nours\n" || unmerged(t, r) != "" {
		t.Errorf("keep ours: %q", b)
	}
	// Keeping theirs (they deleted it) deletes it.
	r = setup()
	if err := ResolveWithSide(r.dir, "f.txt", "theirs"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "f.txt")); !os.IsNotExist(err) || unmerged(t, r) != "" {
		t.Errorf("keep theirs: stat %v", err)
	}
	// Delete.
	r = setup()
	if err := ResolveDelete(r.dir, "f.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "f.txt")); !os.IsNotExist(err) || unmerged(t, r) != "" {
		t.Errorf("delete: stat %v", err)
	}
	if err := ResolveWithSide(r.dir, "f.txt", "mine"); err == nil {
		t.Error("unknown side accepted")
	}
}
