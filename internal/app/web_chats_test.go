package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestStoreChat writes a stored chat with a fixed time.
func webTestStoreChat(t *testing.T, store ai.ChatStore, id, title string, at int64) {
	t.Helper()
	c := ai.Conversation{ID: id, Title: title, UpdatedAtMs: at, Messages: []ai.StoredMessage{{Role: ai.StoredUser, Text: title}}}
	b, _ := json.Marshal(c)
	if err := os.MkdirAll(store.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, id+".json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// webTestRun runs a Cmd and feeds its message to the publishers, like
// Update would.
func webTestRun(m *Model, cmd tea.Cmd) {
	for cmd != nil {
		cmd = m.webAfterUpdate(cmd())
	}
}

// Spec (WEB-13): the chats topic lists the stored chats newest first with
// the laptop's open chat live; the store is read off the UI thread when a
// browser first listens and again after the open chat was saved; a
// deleted chat is a remove. The deltas replay to the newest snapshot.
func TestChatsListFromStore(t *testing.T) {
	m := webTestChatModel(t, ai.StoredMessage{Role: ai.StoredUser, Text: "open one"})
	m.ai.conv.UpdatedAtMs = 3000
	store := m.ai.store
	webTestStoreChat(t, store, "c-old", "Old chat", 1000)
	webTestStoreChat(t, store, "c-mid", "Mid chat", 2000)
	read := 0
	old := webChatsList
	webChatsList = func(s ai.ChatStore) []webproto.ChatItem { read++; return old(s) }
	t.Cleanup(func() { webChatsList = old })
	h := webTestHub(t, &m)
	c := webReplayRecord(t, &m, h, "chats", func() {
		webTestRun(&m, m.webAfterUpdate(nil)) // the first read
		m.ai.streaming = true
		m.webAfterUpdate(nil)
		if err := os.Remove(filepath.Join(store.Dir, "c-old.json")); err != nil {
			t.Fatal(err)
		}
		m.ai.streaming = false
		m.ai.conv.UpdatedAtMs = 4000 // saved: read again
		webTestRun(&m, m.webAfterUpdate(nil))
	})
	if read != 2 {
		t.Fatalf("store read %d times, want 2", read)
	}
	var s0, sn webproto.ChatsSnap
	_ = json.Unmarshal(c.Snap0, &s0)
	_ = json.Unmarshal(c.SnapN, &sn)
	if len(s0.Chats) != 1 || !s0.Chats[0].Open {
		t.Fatalf("before the read: %+v", s0)
	}
	if len(sn.Chats) != 2 || sn.Chats[0].ID != "c-1" || sn.Chats[0].Updated != 4000 || sn.Chats[1].ID != "c-mid" {
		t.Fatalf("after: %+v", sn)
	}
	webReplayCheck(t, h.topics["chats"], c)
	webReplayGolden(t, webReplayPath("chats", "read_live_delete"), c)
}
