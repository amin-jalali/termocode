package webproto

import (
	"strings"
	"testing"
)

// Spec (CMP-88): chat.comment is a viewer op (a person the chat was
// shared with leaves a comment), with an audit line and no laptop card.
// The text is not empty, at most MaxChatComment bytes, and may have new
// lines and tabs but no other control character; the chat id is a name.
func TestChatCommentOp(t *testing.T) {
	p, ok := Lookup(OpChatComment)
	if !ok || p.Role != RoleViewer || p.LaptopCard || p.Audit != AuditYes {
		t.Fatalf("chat.comment: %+v %v", p, ok)
	}
	if _, err := DecodeArgs(OpChatComment, []byte(`{"id":"c-1","text":"Looks good.\nBut check\tthe test."}`)); err != nil {
		t.Errorf("good args: %v", err)
	}
	for _, args := range []string{
		`{"id":"c-1","text":""}`,
		`{"id":"c-1","text":"   "}`,
		`{"id":"","text":"x"}`,
		`{"id":"c-1","text":"a\u0007b"}`,
		`{"id":"c-1","text":"` + strings.Repeat("t", MaxChatComment+1) + `"}`,
	} {
		if _, err := DecodeArgs(OpChatComment, []byte(args)); err == nil {
			t.Errorf("accepted %s", args)
		}
	}
}
