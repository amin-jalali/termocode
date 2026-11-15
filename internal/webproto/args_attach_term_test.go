package webproto

import (
	"encoding/json"
	"testing"
)

// Terminal lines go to the chat by reference (phone Term page, long
// press → Ask AI / Add to chat): the shell's buffer and its rows from 1,
// never the text; anything else is refused.
func TestChatSendAttachTermLines(t *testing.T) {
	send := func(a string) json.RawMessage { return json.RawMessage(`{"text":"look","attach":[` + a + `]}`) }
	for _, a := range []string{
		`{"kind":"term","id":"3","from":1,"to":1}`,
		`{"kind":"term","id":"12","from":4,"to":2003,"label":"3 lines · terminal web 2"}`,
	} {
		if _, err := DecodeArgs(OpChatSend, send(a)); err != nil {
			t.Errorf("%s refused: %v", a, err)
		}
	}
	for _, a := range []string{
		`{"kind":"term","id":"3"}`,
		`{"kind":"term","id":"3","from":0,"to":2}`,
		`{"kind":"term","id":"3","from":5,"to":4}`,
		`{"kind":"term","id":"3","from":1,"to":2001}`,
		`{"kind":"term","id":"03","from":1,"to":1}`,
		`{"kind":"term","id":"-3","from":1,"to":1}`,
		`{"kind":"term","id":"x","from":1,"to":1}`,
		`{"kind":"term","from":1,"to":1}`,
		`{"kind":"term","id":"3","path":"a.go","from":1,"to":1}`,
		`{"kind":"term","id":"3","spec":"{}","from":1,"to":1}`,
	} {
		if _, err := DecodeArgs(OpChatSend, send(a)); err == nil {
			t.Errorf("%s accepted", a)
		}
	}
}
