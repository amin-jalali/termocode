package webproto

import (
	"strings"
	"testing"
)

// Spec (WEB-15): the cards data is typed for the page.
func TestCardsDataGenerated(t *testing.T) {
	b, err := GenerateTS()
	if err != nil {
		t.Fatal(err)
	}
	ts := string(b)
	for _, want := range []string{
		`  "cards": { snap: CardsSnap; delta: CardsDelta };`,
		`  "answered": Answered;`,
		`  kind: "tool" | "mcp" | "plan" | "review" | "trust";`,
		`  k: "ctx" | "add" | "del" | "hunk" | "file";`,
		`  needs_laptop?: boolean;`,
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("proto.d.ts misses %q", want)
		}
	}
}
