package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// MOB-12: cards.answer may carry a form's answers: at most MaxCardForm,
// each at most MaxCardFormValue bytes, with plain field ids.
func TestCardsAnswerFormArgs(t *testing.T) {
	var many []string
	for i := 0; i <= MaxCardForm; i++ {
		many = append(many, `"f`+strings.Repeat("x", i)+`":"v"`)
	}
	for _, bad := range []string{`{"":"v"}`, `{"a\u0001":"v"}`, `{"a":"` + strings.Repeat("x", MaxCardFormValue+1) + `"}`, "{" + strings.Join(many, ",") + "}"} {
		if _, err := DecodeArgs(OpCardsAnswer, json.RawMessage(`{"id":"c1","choice":"send","form":`+bad+`}`)); err == nil {
			t.Errorf("form %.40s decoded", bad)
		}
	}
	a, err := DecodeArgs(OpCardsAnswer, json.RawMessage(`{"id":"c1","choice":"send","form":{"repo":"a/b","count":"3"}}`))
	if err != nil || a.(*CardsAnswerArgs).Form["repo"] != "a/b" {
		t.Fatalf("form: %v %+v", err, a)
	}
}
