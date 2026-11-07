package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (CMP-88): a person the chat was shared with leaves a comment
// (chat.comment, a viewer op). The laptop keeps it with the chat (not in
// the chat's messages, so the AI never reads it) and says so; a comment
// on a chat that does not exist is not_found. AI: Chat Comments… lists
// the open chat's comments, newest first; Enter asks first (Cancel first)
// and Add to message puts the comment into the chat input, for the user
// to send. Neither the share nor the comments are AI actions.
func TestAIChatComments(t *testing.T) {
	r := webChatMoreRig(t)
	viewer := r.conn("v-1", webproto.RoleViewer)
	if code := webErrCode(r.callOn(viewer, webproto.OpChatComment, &webproto.ChatCommentArgs{ID: "c-none", Text: "hi"})); code != "not_found" {
		t.Fatalf("missing chat: %s", code)
	}
	if f := r.callOn(viewer, webproto.OpChatComment, &webproto.ChatCommentArgs{ID: "c-1", Text: "  Check the second answer.  "}); webErrCode(f) != "" {
		t.Fatalf("comment: %v", f)
	}
	r.callOn(viewer, webproto.OpChatComment, &webproto.ChatCommentArgs{ID: "c-1", Text: "Also run the tests."})
	got := aiChatCommentsLoad("c-1")
	if len(got) != 2 || got[0].Text != "Check the second answer." || got[1].Text != "Also run the tests." {
		t.Fatalf("stored %+v", got)
	}
	for _, msg := range r.m.ai.conv.Messages {
		if strings.Contains(msg.Text, "second answer.") && msg.Text != "answer two" {
			t.Fatalf("a comment went into the chat messages: %+v", msg)
		}
	}
	m := r.m
	if _, ok := m.dispatchAIPalette(aiChatCommentsID); !ok || !m.pickerOpen || m.pickerKind != pickerKindAIChatComments {
		t.Fatalf("palette: picker %v kind %v", m.pickerOpen, m.pickerKind)
	}
	items := m.aiChatCommentItems()
	if len(items) != 2 || items[0].Title != "Also run the tests." {
		t.Fatalf("rows %+v", items)
	}
	m.pickerOpen = false
	m.handleAIPickerSelect(items[1].ID)
	if !m.confirmOpen || m.confirmKind != confirmKindAIChatComment || m.confirm.Buttons()[0].ID != "cancel" {
		t.Fatalf("card: open %v kind %v", m.confirmOpen, m.confirmKind)
	}
	m.confirmOpen = false
	m.handleAIConfirm("add")
	if in := string(m.ai.input); !strings.Contains(in, "Check the second answer.") {
		t.Fatalf("input %q", in)
	}
	for _, id := range []string{aiChatCommentsID, aiShareChatID} {
		if p, ok := aiActionPolicyFor(id); !ok || p.Gate != aiGateNever {
			t.Errorf("%s policy %+v %v", id, p, ok)
		}
	}
}

// Spec (CMP-88): AI: Share Chat by Link… first says what is shared, on a
// card with Cancel first: Cancel makes no link.
func TestAIShareChatCard(t *testing.T) {
	r := webChatMoreRig(t)
	m := r.m
	m.dispatchAIPalette(aiShareChatID)
	if !m.confirmOpen || m.confirmKind != confirmKindAIShareChat || m.confirm.Buttons()[0].ID != "cancel" {
		t.Fatalf("card: open %v kind %v", m.confirmOpen, m.confirmKind)
	}
	m.confirmOpen = false
	m.handleAIConfirm("cancel")
	if m.confirmOpen || m.webAccess().link != "" {
		t.Fatalf("cancel made a link %q", m.webAccess().link)
	}
}
