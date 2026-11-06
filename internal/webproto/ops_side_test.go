package webproto

import (
	"encoding/json"
	"testing"
)

// Spec (WEBP-9, AI: Side Question…): chat.side is a controller's quick
// question beside the main chat (it spends the user's tokens like
// chat.send, so a viewer may not); it is audited by size and needs no
// laptop card; one runs at a time per connection.
func TestChatSideRow(t *testing.T) {
	p, ok := Lookup(OpChatSide)
	if !ok || p.Role != RoleController || p.LaptopCard || p.Audit != AuditYes || p.Live != 1 {
		t.Fatalf("chat.side row %+v", p)
	}
	for _, b := range []string{`{"text":""}`, `{"text":"x","chat_id":"a\u0007"}`, `{"text":"x","model":"big"}`} {
		if _, err := DecodeArgs(OpChatSide, json.RawMessage(b)); err == nil {
			t.Errorf("%s accepted", b)
		}
	}
	if _, err := DecodeArgs(OpChatSide, json.RawMessage(`{"text":"why?","chat_id":"c-1"}`)); err != nil {
		t.Error(err)
	}
}
