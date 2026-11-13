//go:build unix

package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (MOB-18): board.get shows a waiting card in "needs you" with the
// id the cards topic gave it (so a tap opens that card), and asking for
// the board changes nothing the cards topic sends.
func TestWebBoardGetShowsWaitingCards(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	webTestToolCard(&m, ai.Tool{Name: "write_file", Class: "write"})
	c := webTestSub(t, &m, "cards")
	cards := webTestCards(t, c)
	if len(cards) != 1 {
		t.Fatalf("cards %+v", cards)
	}
	res, _ := webTestCall(t, &m, c, webproto.OpBoardGet, `{}`)
	var b webproto.Board
	webResData(t, res, &b)
	if b.Counts.NeedsYou != 1 || len(b.Items) == 0 {
		t.Fatalf("board %+v", b)
	}
	it := b.Items[0]
	if it.Kind != webproto.BoardKindCard || it.Column != webproto.BoardNeedsYou || it.State != webproto.BoardWait || it.Ref != cards[0].ID || it.ID != "card:"+cards[0].ID {
		t.Errorf("tile %+v, card %s", it, cards[0].ID)
	}
	if !strings.Contains(it.Title, "AI wants to") {
		t.Errorf("title %q", it.Title)
	}
	m.webAfterUpdate(nil)
	for _, f := range webTestNow(t, c) {
		if f["topic"] == "cards" {
			t.Errorf("board.get made the cards topic send %v", f)
		}
	}
}

// Spec (MOB-18): a viewer may read the board; with nothing going on it
// is empty (a list, not null).
func TestWebBoardGetEmptyForAViewer(t *testing.T) {
	m := aiTestModel(t)
	h := webTestHub(t, &m)
	c := webPolicyConn(h, "v-1", webproto.RoleViewer)
	res, _ := webTestCall(t, &m, c, webproto.OpBoardGet, `{}`)
	if res["t"] != "res" {
		t.Fatalf("viewer: %v", res)
	}
	items, ok := res["data"].(map[string]any)["items"].([]any)
	if !ok || len(items) != 0 {
		t.Errorf("items %v", res["data"])
	}
}
