package aimemory

import (
	"reflect"
	"strings"
	"testing"
)

// CMP-73: a lesson has a kind (lesson, decision, correction). The kind is
// one "kind:" line before "paths:", and it survives a round trip.
func TestKindRoundTrip(t *testing.T) {
	ls := []Lesson{
		{ID: NewID("Use pgx", "We picked pgx over lib/pq."), Date: "2026-11-07", Title: "Use pgx", Text: "We picked pgx over lib/pq.", Kind: KindDecision, Paths: []string{"go.mod"}},
		{ID: NewID("Tabs", "Use tabs in Go files."), Date: "2026-11-07", Title: "Tabs", Text: "Use tabs in Go files.", Kind: KindCorrection},
		{ID: NewID("Plain", "No kind."), Date: "2026-11-07", Title: "Plain", Text: "No kind."},
	}
	b := string(Marshal(ls))
	if !strings.Contains(b, "We picked pgx over lib/pq.\nkind: decision\npaths: go.mod\n") ||
		!strings.Contains(b, "Use tabs in Go files.\nkind: correction\n") || strings.Contains(b, "No kind.\nkind:") {
		t.Fatalf("file:\n%s", b)
	}
	if got := Parse([]byte(b)); !reflect.DeepEqual(got, ls) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, ls)
	}
	// Meta lines in the other order are read too; an unknown kind is a
	// plain lesson.
	hand := "## 2026-11-07 · Hand {#abcd1234}\nText.\npaths: a.go\nkind: Decision\n\n## 2026-11-07 · Odd {#abcd1235}\nMore.\nkind: whatever\n"
	got := Parse([]byte(hand))
	if len(got) != 2 || got[0].Kind != KindDecision || !reflect.DeepEqual(got[0].Paths, []string{"a.go"}) || got[0].Text != "Text." ||
		got[1].Kind != "" || got[1].Text != "More." {
		t.Fatalf("hand file: %+v", got)
	}
}

// CMP-73: the quick model's reply may set a kind; unknown kinds are a
// plain lesson.
func TestParseReplyKind(t *testing.T) {
	reply := "```json\n" + `{"lessons":[{"title":"Not npm","text":"Use pnpm here, the user said npm was wrong.","kind":"correction"},` +
		`{"title":"Chose SQLite","text":"The user chose SQLite for tests.","kind":"decision"},` +
		`{"title":"Fact","text":"Make builds docs.","kind":"weird"}]}` + "\n```"
	ls, err := ParseReply(reply)
	if err != nil || len(ls) != 3 {
		t.Fatalf("ls %+v err %v", ls, err)
	}
	if ls[0].Kind != KindCorrection || ls[1].Kind != KindDecision || ls[2].Kind != "" {
		t.Fatalf("kinds %q %q %q", ls[0].Kind, ls[1].Kind, ls[2].Kind)
	}
	if NormKind(" Correction ") != KindCorrection || NormKind("lesson") != "" {
		t.Fatal("NormKind")
	}
}
