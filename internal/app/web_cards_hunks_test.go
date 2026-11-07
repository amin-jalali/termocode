package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (WEB-F4): a review card with two or more changes lists each
// change's rows, and the web's Accept may name the changes it keeps:
// only those are applied, the rest is rejected. No list keeps the old
// meaning (the whole file); a change that is not on the card, or a list
// with Reject, is refused and nothing is applied.
func TestWebReviewAcceptsChosenHunks(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	var done map[int]bool
	webTestReview(&m, "a.go", "a\nb\nc\n", "A\nb\nC\n", &done)
	c := webTestSub(t, &m, "cards")
	cards := webTestCards(t, c)
	d := cards[0].Diff
	if d == nil || len(d.Hunks) != 2 {
		t.Fatalf("hunks: %+v", d)
	}
	for i, h := range d.Hunks {
		if h.First < 0 || h.End <= h.First || h.End > len(d.Rows) {
			t.Fatalf("hunk %d rows %+v of %d", i, h, len(d.Rows))
		}
		for _, r := range d.Rows[h.First:h.End] {
			if r.K == "ctx" {
				t.Errorf("hunk %d holds a context row", i)
			}
		}
	}
	for _, bad := range []string{`"choice":"accept","hunks":[2]`, `"choice":"reject","hunks":[0]`} {
		if f, _ := webTestCall(t, &m, c, webproto.OpCardsAnswer, `{"id":"`+cards[0].ID+`",`+bad+`}`); f["t"] != "err" || done != nil {
			t.Fatalf("%s: %v %v", bad, f, done)
		}
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpCardsAnswer, `{"id":"`+cards[0].ID+`","choice":"accept","hunks":[1]}`); f["t"] != "res" {
		t.Fatalf("accept one: %v", f)
	}
	if len(done) != 1 || !done[1] {
		t.Fatalf("accepted %v, want only the second change", done)
	}
}

// Spec (WEB-F4): a one-change card lists no hunks (Accept is the whole
// change, as before).
func TestWebReviewOneHunkListsNone(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	var done map[int]bool
	webTestReview(&m, "a.go", "a\nb\n", "a\nc\n", &done)
	c := webTestSub(t, &m, "cards")
	if d := webTestCards(t, c)[0].Diff; d == nil || len(d.Hunks) != 0 {
		t.Fatalf("one change: %+v", d)
	}
}
