package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (MOB-16): chat.home is a controller's send to the phone's own
// background chat (it spends the user's tokens like chat.send); audited by
// size, no laptop card, one at a time per connection. {} only reads it.
func TestChatHomeRow(t *testing.T) {
	p, ok := Lookup(OpChatHome)
	if !ok || p.Role != RoleController || p.LaptopCard || p.Audit != AuditYes || p.Live != 1 || p.Phase != PhaseV2 {
		t.Fatalf("chat.home row %+v", p)
	}
	big := `{"text":"` + strings.Repeat("a", MaxChatText+1) + `"}`
	for _, b := range []string{`{"text":"x","chat_id":"c-1"}`, big, `{"text":"a\u0000b"}`} {
		if _, err := DecodeArgs(OpChatHome, json.RawMessage(b)); err == nil {
			t.Errorf("%.40s accepted", b)
		}
	}
	for _, b := range []string{`{}`, `{"text":"why did the build fail?"}`} {
		if _, err := DecodeArgs(OpChatHome, json.RawMessage(b)); err != nil {
			t.Errorf("%s: %v", b, err)
		}
	}
}
