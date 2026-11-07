package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (WEB-F4): cards.answer may name the review changes it accepts:
// indexes from 0, at most MaxCardHunks of them.
func TestCardsAnswerHunksArgs(t *testing.T) {
	many := "[" + strings.TrimSuffix(strings.Repeat("0,", MaxCardHunks+1), ",") + "]"
	for _, bad := range []string{`[-1]`, many} {
		if _, err := DecodeArgs(OpCardsAnswer, json.RawMessage(`{"id":"c1","choice":"accept","hunks":`+bad+`}`)); err == nil {
			t.Errorf("hunks %.40s decoded", bad)
		}
	}
	a, err := DecodeArgs(OpCardsAnswer, json.RawMessage(`{"id":"c1","choice":"accept","hunks":[0,2]}`))
	if err != nil || len(a.(*CardsAnswerArgs).Hunks) != 2 {
		t.Fatalf("hunks [0,2]: %v %+v", err, a)
	}
}
