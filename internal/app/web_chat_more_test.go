//go:build unix

package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webChatMoreRig is a controller on a model whose open chat is c-1 and
// whose store holds c-old.
func webChatMoreRig(t *testing.T) *webFilesRig {
	t.Helper()
	r := newWebFilesRig(t)
	r.m.ai.conv = ai.Conversation{ID: "c-1", Title: "Fix the build", Messages: []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "first"}, {Role: ai.StoredAssistant, Text: "answer one"},
		{Role: ai.StoredUser, Text: "second"}, {Role: ai.StoredAssistant, Text: "answer two"}}}
	if err := r.m.ai.store.Save(&r.m.ai.conv); err != nil {
		t.Fatal(err)
	}
	webTestStoreChat(t, r.m.ai.store, "c-old", "Old chat", 1000)
	return r
}

// Spec (web parity: ai-history-rename): chat.rename renames a stored chat
// and the open one (its title follows at once); a missing chat is
// not_found.
func TestWebChatRename(t *testing.T) {
	r := webChatMoreRig(t)
	if f := r.call(webproto.OpChatRename, &webproto.ChatRenameArgs{ID: "c-old", Title: "Better name"}); webErrCode(f) != "" {
		t.Fatalf("rename: %v", f)
	}
	if c, err := r.m.ai.store.Load("c-old"); err != nil || c.Title != "Better name" {
		t.Fatalf("stored title %q %v", c.Title, err)
	}
	r.call(webproto.OpChatRename, &webproto.ChatRenameArgs{ID: "c-1", Title: "  Open one  "})
	if r.m.ai.conv.Title != "Open one" {
		t.Fatalf("open chat title %q", r.m.ai.conv.Title)
	}
	if code := webErrCode(r.call(webproto.OpChatRename, &webproto.ChatRenameArgs{ID: "c-none", Title: "x"})); code != "not_found" {
		t.Fatalf("missing: %s", code)
	}
}

// Spec (web parity: ai-fork-chat): chat.fork forks the laptop's open chat
// at message i into a new chat (the old one stays in the history); it is
// busy while the AI answers and not_found past the end.
func TestWebChatFork(t *testing.T) {
	r := webChatMoreRig(t)
	r.m.ai.streaming = true
	if code := webErrCode(r.call(webproto.OpChatFork, &webproto.ChatForkArgs{I: 2})); code != "busy" {
		t.Fatalf("fork while busy: %s", code)
	}
	r.m.ai.streaming = false
	if code := webErrCode(r.call(webproto.OpChatFork, &webproto.ChatForkArgs{I: 9})); code != "not_found" {
		t.Fatalf("past the end: %s", code)
	}
	f := r.call(webproto.OpChatFork, &webproto.ChatForkArgs{I: 2})
	var ref webproto.ChatRef
	webTestData(t, f, &ref)
	if ref.ID == "" || ref.ID == "c-1" || r.m.ai.conv.ID != ref.ID {
		t.Fatalf("fork: %v open=%s", f, r.m.ai.conv.ID)
	}
	if _, err := r.m.ai.store.Load("c-1"); err != nil {
		t.Fatal("the original chat is gone")
	}
}

// Spec (web parity: ai-history-delete, ai-history-clear): delete and clear
// put a laptop card up (Cancel first); nothing is deleted before the
// laptop's yes, and Cancel deletes nothing.
func TestWebChatDeleteAndClearWaitForTheLaptop(t *testing.T) {
	r := webChatMoreRig(t)
	m := r.m
	r.call(webproto.OpChatDelete, &webproto.ChatOpenArgs{ID: "c-old"})
	if !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.SafeID() != webproto.ChoiceCancel {
		t.Fatal("no laptop card")
	}
	if !strings.Contains(m.confirm.View(), "Old chat") {
		t.Errorf("the card does not name the chat:\n%s", m.confirm.View())
	}
	m.confirmOpen = false
	m.answerWebAsk(webproto.ChoiceCancel)
	if _, err := m.ai.store.Load("c-old"); err != nil {
		t.Fatal("cancel deleted")
	}
	r.call(webproto.OpChatDelete, &webproto.ChatOpenArgs{ID: "c-old"})
	m.confirmOpen = false
	m.answerWebAsk(webproto.ChoiceConfirm)
	if _, err := m.ai.store.Load("c-old"); err == nil {
		t.Fatal("not deleted after the yes")
	}
	if code := webErrCode(r.call(webproto.OpChatDelete, &webproto.ChatOpenArgs{ID: "c-old"})); code != "not_found" {
		t.Fatalf("gone: %s", code)
	}

	r.call(webproto.OpChatClear, &webproto.EmptyArgs{})
	if !m.confirmOpen || len(m.ai.store.List()) == 0 {
		t.Fatal("clear ran without a card")
	}
	m.confirmOpen = false
	m.answerWebAsk(webproto.ChoiceConfirm)
	if n := len(m.ai.store.List()); n != 0 || m.ai.conv.ID == "c-1" {
		t.Fatalf("after clear: %d chats, open %s", n, m.ai.conv.ID)
	}
}

// Spec (web parity: ai-export-chat): chat.export answers with a chat as
// markdown and a file name for the browser to save (nothing is written on
// the laptop); a registered secret is hidden.
func TestWebChatExport(t *testing.T) {
	r := webChatMoreRig(t)
	secret := "sk-" + "test-export-0123456789"
	ai.RegisterSecret(secret)
	r.m.ai.conv.Messages[3].Text = "the key is " + secret
	var ex webproto.ChatExport
	webTestData(t, r.call(webproto.OpChatExport, &webproto.ChatOpenArgs{ID: "c-1"}), &ex)
	if !strings.HasPrefix(ex.Text, "# Fix the build") || !strings.Contains(ex.Text, "answer one") || strings.Contains(ex.Text, secret) {
		t.Fatalf("export:\n%s", ex.Text)
	}
	if !strings.HasPrefix(ex.Name, "ai-chat-") || !strings.HasSuffix(ex.Name, ".md") {
		t.Fatalf("name %q", ex.Name)
	}
	stored, _ := webTestCall(t, r.m, r.c, webproto.OpChatExport, `{"id":"c-old"}`) // read off the UI thread
	webTestData(t, stored, &ex)
	if !strings.Contains(ex.Text, "# Old chat") {
		t.Fatalf("stored export:\n%s", ex.Text)
	}
	if missing, _ := webTestCall(t, r.m, r.c, webproto.OpChatExport, `{"id":"c-none"}`); webErrCode(missing) != "not_found" {
		t.Fatalf("missing: %v", missing)
	}
}
