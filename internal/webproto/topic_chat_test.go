package webproto

import (
	"strings"
	"testing"
)

// Spec (WEB-13): the chat data is typed for the page: chat and chats in
// TopicData, the results of the chat ops in OpResult, and the markdown
// AST as a recursive MdNode with a closed list of kinds.
func TestChatDataGenerated(t *testing.T) {
	b, err := GenerateTS()
	if err != nil {
		t.Fatal(err)
	}
	ts := string(b)
	for _, want := range []string{
		`  "chats": { snap: ChatsSnap; delta: ChatsDelta };`,
		`  "chat": { snap: ChatSnap; delta: ChatDelta };`,
		`export interface OpResult {`,
		`  "chat.open": ChatSnap;`,
		`  "chat.get_message": ChatMsg;`,
		`export interface MdNode {`,
		`  k: "doc" | "p" | "h" | "em" | "strong" | "code" | "codeblock" | "ul" | "ol" | "li" | "blockquote" | "link" | "hr" | "br" | "table" | "th" | "td" | "text";`,
		`  c?: MdNode[];`,
		`  rows?: MdNode[][];`,
		`  op: "append" | "message" | "truncate" | "state" | "switched";`,
		`  i?: number;`,
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("proto.d.ts misses %q", want)
		}
	}
	if strings.Contains(ts, "html") || strings.Contains(ts, "HTML") {
		t.Error("the chat data must not name HTML anywhere")
	}
}

func TestResultRegistry(t *testing.T) {
	if r, ok := ResultOf(OpChatOpen); !ok || r.Name() != "ChatSnap" {
		t.Errorf("chat.open result %v", r)
	}
	defer func() {
		if recover() == nil {
			t.Error("a result for an unknown op must panic")
		}
	}()
	registerResult("nope.op", ChatRef{})
}
