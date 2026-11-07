package chatdocs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Plan":            "plan",
		"API design v2":   "api-design-v2",
		"  release notes": "release-notes",
		"a/../b":          "a-b",
	} {
		got, err := Slug(in)
		if err != nil || got != want {
			t.Fatalf("Slug(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "  ", "...", "///"} {
		if _, err := Slug(bad); err == nil {
			t.Fatalf("Slug(%q) took a name with no letters", bad)
		}
	}
	if s, _ := Slug(strings.Repeat("x", 200)); len(s) > NameMax {
		t.Fatalf("slug too long: %d", len(s))
	}
}

func TestWriteKeepsRevisions(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.Write("plan", "one", AuthorAI); err != nil {
		t.Fatal(err)
	}
	n, err := s.Write("plan", "two", AuthorAI)
	if err != nil || n != 2 {
		t.Fatalf("rev %d err %v", n, err)
	}
	if txt, _ := s.Read("plan"); txt != "two" {
		t.Fatalf("read %q", txt)
	}
	revs := s.Revisions("plan")
	if len(revs) != 2 || revs[0].N != 1 || revs[1].Author != AuthorAI {
		t.Fatalf("revs %+v", revs)
	}
	if txt, err := s.ReadRev("plan", 1); err != nil || txt != "one" {
		t.Fatalf("rev 1 %q %v", txt, err)
	}
}

func TestSameTextIsNoNewRevision(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	_, _ = s.Write("plan", "one", AuthorAI)
	n, err := s.Write("plan", "one", AuthorAI)
	if err != nil || n != 1 || len(s.Revisions("plan")) != 1 {
		t.Fatalf("an unchanged write made a revision (n %d)", n)
	}
}

func TestUserEditBecomesItsOwnRevision(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	_, _ = s.Write("plan", "ai text", AuthorAI)
	// the user edits the file in the editor
	if err := os.WriteFile(s.Path("plan"), []byte("ai text\nmy line"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write("plan", "ai text\nmy line\nmore", AuthorAI); err != nil {
		t.Fatal(err)
	}
	revs := s.Revisions("plan")
	if len(revs) != 3 || revs[1].Author != AuthorYou || revs[2].Author != AuthorAI {
		t.Fatalf("the user's edit must be kept as its own revision: %+v", revs)
	}
}

func TestRestoreIsANewRevision(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	_, _ = s.Write("plan", "one", AuthorAI)
	_, _ = s.Write("plan", "two", AuthorAI)
	n, err := s.Restore("plan", 1)
	if err != nil || n != 3 {
		t.Fatalf("restore rev %d %v", n, err)
	}
	if txt, _ := s.Read("plan"); txt != "one" {
		t.Fatalf("read %q", txt)
	}
	if _, err := s.Restore("plan", 9); err == nil {
		t.Fatal("restored a revision that is not there")
	}
}

func TestListAndLimits(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	_, _ = s.Write("b", "x", AuthorAI)
	_, _ = s.Write("a", "y", AuthorYou)
	docs := s.List()
	if len(docs) != 2 || docs[0].Name != "a" || docs[1].Name != "b" || docs[0].Revs != 1 {
		t.Fatalf("list %+v", docs)
	}
	if _, err := s.Write("big", strings.Repeat("x", TextMax+1), AuthorAI); err == nil {
		t.Fatal("a doc over TextMax was written")
	}
	if _, err := s.Read("nope"); err == nil {
		t.Fatal("read a doc that is not there")
	}
	if (Store{}).List() != nil {
		t.Fatal("a store with no folder lists nothing")
	}
}

func TestOldRevisionsArePruned(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for i := 0; i < RevsMax+5; i++ {
		_, _ = s.Write("plan", strings.Repeat("x", i+1), AuthorAI)
	}
	revs := s.Revisions("plan")
	if len(revs) != RevsMax || revs[len(revs)-1].N != RevsMax+5 {
		t.Fatalf("kept %d revs, last %d", len(revs), revs[len(revs)-1].N)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "plan.revs")); err != nil {
		t.Fatal(err)
	}
}
