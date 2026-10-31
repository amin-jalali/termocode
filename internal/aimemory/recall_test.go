package aimemory

import "testing"

// Spec (OMX-18): only real matches are recalled (a path or two shared
// words), path matches first, seen lessons never again, at most maxN.
func TestRecall(t *testing.T) {
	ls := []Lesson{
		{ID: "a", Title: "Migrations", Text: "Run make migrate before tests", Paths: []string{"db/schema.sql"}},
		{ID: "b", Title: "Docker", Text: "The compose file needs the env file"},
		{ID: "c", Title: "Tests", Text: "Run tests with race detector"},
		{ID: "d", Title: "Unrelated", Text: "Nothing here"},
	}
	got := Recall(ls, Hints{Text: "run_command go test -race tests detector", Paths: []string{"/w/db/schema.sql"}}, nil, 5)
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("recall %+v", got)
	}
	got = Recall(ls, Hints{Text: "tests detector", Paths: []string{"db/schema.sql"}}, map[string]bool{"a": true}, 5)
	if len(got) != 1 || got[0].ID != "c" {
		t.Fatalf("seen not skipped: %+v", got)
	}
	if got := Recall(ls, Hints{Text: "hello world"}, nil, 5); len(got) != 0 {
		t.Fatalf("no newest fill expected: %+v", got)
	}
	if got := Recall(ls, Hints{Paths: []string{"db/schema.sql"}, Text: "tests detector"}, nil, 1); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("maxN: %+v", got)
	}
	if (Lesson{Date: "2026-01-01", Title: "x"}).Key() == (Lesson{Date: "2026-01-02", Title: "x"}).Key() {
		t.Error("key without id must differ by date")
	}
}
