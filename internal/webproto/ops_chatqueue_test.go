package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (CMP-46): chat.queue lets the phone line up messages for the
// laptop's open chat while its AI works ({text}), take one back
// ({remove}) or steer with one ({now}: stop the turn, send it next).
// A controller's op like chat.send, audited, no laptop card; exactly one
// of the three; at most MaxChatQueue wait.
func TestChatQueueRow(t *testing.T) {
	p, ok := Lookup(OpChatQueue)
	if !ok || p.Role != RoleController || p.LaptopCard || p.Audit != AuditYes || p.Phase != PhaseV2 {
		t.Fatalf("chat.queue row %+v", p)
	}
	if MaxChatQueue != 5 {
		t.Fatalf("MaxChatQueue %d", MaxChatQueue)
	}
	big := `{"text":"` + strings.Repeat("a", MaxChatText+1) + `"}`
	for _, b := range []string{`{}`, `{"text":"   "}`, `{"text":"x","remove":"q1"}`, `{"remove":"q1","now":"q2"}`, big, `{"text":"a\u0000b"}`, `{"remove":"bad id!"}`, `{"now":"` + strings.Repeat("q", 40) + `"}`} {
		if _, err := DecodeArgs(OpChatQueue, json.RawMessage(b)); err == nil {
			t.Errorf("%.40s accepted", b)
		}
	}
	for _, b := range []string{`{"text":"then update the docs"}`, `{"remove":"q1"}`, `{"now":"q-12"}`} {
		if _, err := DecodeArgs(OpChatQueue, json.RawMessage(b)); err != nil {
			t.Errorf("%s: %v", b, err)
		}
	}
}
