package ai

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestChatStoreSaveLoadList(t *testing.T) {
	s := ChatStore{Dir: t.TempDir()}
	c := NewConversation()
	if err := s.Save(&c); err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 0 {
		t.Fatal("empty chats are not written")
	}
	c.Messages = []StoredMessage{{Role: StoredUser, Text: "  How do I write a table-driven test in Go for this parser?\nmore"}, {Role: StoredAssistant, Text: "Like this"}}
	if err := s.Save(&c); err != nil {
		t.Fatal(err)
	}
	if c.Title != "How do I write a table-driven test in Go" {
		t.Errorf("title %q", c.Title)
	}
	got, err := s.Load(c.ID)
	if err != nil || len(got.Messages) != 2 {
		t.Fatalf("load: %+v %v", got, err)
	}
	st, _ := os.Stat(filepath.Join(s.Dir, c.ID+".json"))
	if st.Mode().Perm() != 0o600 {
		t.Errorf("chat file mode %v", st.Mode().Perm())
	}
	_ = os.WriteFile(filepath.Join(s.Dir, "broken.json"), []byte("{"), 0o600)
	if l := s.List(); len(l) != 1 {
		t.Fatalf("corrupt files must be skipped, got %d", len(l))
	}
	if err := s.Delete(c.ID); err != nil || len(s.List()) != 0 {
		t.Fatal("delete")
	}
}

func TestChatStorePrunes(t *testing.T) {
	s := ChatStore{Dir: t.TempDir()}
	for i := 0; i < maxChats+3; i++ {
		c := NewConversation()
		c.Messages = []StoredMessage{{Role: StoredUser, Text: fmt.Sprint(i)}}
		for ms := time.Now().UnixMilli(); time.Now().UnixMilli() == ms; { // each save its own ms
		}
		if err := s.Save(&c); err != nil {
			t.Fatal(err)
		}
	}
	list := s.List()
	if n := len(list); n != maxChats {
		t.Fatalf("kept %d", n)
	}
	// The newest are kept: the first three saved are gone.
	kept := map[string]bool{}
	for _, c := range list {
		kept[c.Title] = true
	}
	for i := 0; i < maxChats+3; i++ {
		if want := i >= 3; kept[fmt.Sprint(i)] != want {
			t.Errorf("chat %d kept=%v, want %v", i, !want, want)
		}
	}
	if DeriveTitle(nil) != defaultChatTitle {
		t.Error("default title")
	}
}

// Spec (U20 AP-4): a pipeline run keeps its step cards and the user's
// fold choices in the chat file; an old file without them still loads.
func TestChatStoreStepsAndFolds(t *testing.T) {
	s := ChatStore{Dir: t.TempDir()}
	c := NewConversation()
	cost := 0.013
	c.Messages = []StoredMessage{
		{Role: StoredUser, Text: "review"},
		{Role: StoredAssistant, Text: "fixed",
			Run: &StoredRun{ID: "r1", Profile: "review", State: RunStateDone, ElapsedMs: 36000},
			Steps: []StoredStep{{ID: "reviewer", Title: "Reviewer", Slot: "main", Model: "claude-sonnet-4-5", Category: "deep",
				Text: "two problems", Tools: []string{"⚙ Read a.go → 10 lines"}, ElapsedMs: 14000,
				Usage: &Usage{Model: "claude-sonnet-4-5", Input: 4000, Output: 100, Requests: 1, PeakContext: 4000},
				Cost:  &cost, Files: 1, State: "completed"}}},
	}
	c.Folded = map[string]bool{"step:r1:reviewer": false}
	if err := s.Save(&c); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(s.Dir, c.ID+".json"))
	for _, want := range []string{`"steps"`, `"folded"`, `"input":4000`, `"elapsed_ms":14000`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("chat file has no %s:\n%s", want, b)
		}
	}
	got, err := s.Load(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	m := got.Messages[1]
	if m.Run == nil || m.Run.ID != "r1" || len(m.Steps) != 1 || m.Steps[0].Usage.Input != 4000 || *m.Steps[0].Cost != cost ||
		m.Steps[0].Tools[0] != "⚙ Read a.go → 10 lines" || got.Folded["step:r1:reviewer"] != false || len(got.Folded) != 1 {
		t.Errorf("loaded %+v folded %v", m, got.Folded)
	}
	old := `{"id":"x","title":"t","messages":[{"role":"user","text":"hi"}],"updated_at_ms":1}`
	_ = os.WriteFile(filepath.Join(s.Dir, "x.json"), []byte(old), 0o600)
	if o, err := s.Load("x"); err != nil || o.Messages[0].Steps != nil || o.Folded != nil {
		t.Errorf("old file: %+v %v", o, err)
	}
	plain, _ := json.Marshal(StoredMessage{Role: StoredUser, Text: "hi"})
	if strings.Contains(string(plain), "steps") || strings.Contains(string(plain), "run") {
		t.Errorf("a plain message stays as before: %s", plain)
	}
}

// ListCached reads a chat file again only when it changed: a saved chat
// (also one with the same size), a new and a deleted chat show at once;
// an unchanged file is not read again.
func TestChatStoreListCached(t *testing.T) {
	s := ChatStore{Dir: t.TempDir()}
	var lc ChatListCache
	a, b := NewConversation(), NewConversation()
	a.Messages = []StoredMessage{{Role: StoredUser, Text: "alpha"}}
	b.Messages = []StoredMessage{{Role: StoredUser, Text: "bravo"}}
	for _, c := range []*Conversation{&a, &b} {
		if err := s.Save(c); err != nil {
			t.Fatal(err)
		}
	}
	titles := func() string {
		var ts []string
		for _, c := range s.ListCached(&lc) {
			ts = append(ts, c.Messages[0].Text)
		}
		sort.Strings(ts)
		return strings.Join(ts, ",")
	}
	if got := titles(); got != "alpha,bravo" {
		t.Fatalf("first: %s", got)
	}
	// Unchanged: not read again (an unreadable file still lists).
	pb := s.path(b.ID)
	if err := os.Chmod(pb, 0); err == nil && os.Getuid() != 0 {
		if got := titles(); got != "alpha,bravo" {
			t.Errorf("unchanged file read again: %s", got)
		}
		_ = os.Chmod(pb, 0o600)
	}
	b.Messages[0].Text = "BRAVO" // the same size
	if err := s.Save(&b); err != nil {
		t.Fatal(err)
	}
	c := NewConversation()
	c.Messages = []StoredMessage{{Role: StoredUser, Text: "charlie"}}
	if err := s.Save(&c); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if got := titles(); got != "BRAVO,charlie" {
		t.Errorf("after changes: %s", got)
	}
	if got := titles(); got != "BRAVO,charlie" {
		t.Errorf("again: %s", got)
	}
	// A file rewritten in place with the same size and time is the one
	// case it can not see; the store never does that (it renames).
}

func TestConversationIDHasRandomSuffix(t *testing.T) {
	seen := make(map[string]bool)
	for range 1000 {
		c := NewConversation()
		_, suffix, ok := strings.Cut(c.ID, "-")
		if !ok || len(suffix) != 32 || strings.Trim(suffix, "0123456789abcdef") != "" {
			t.Fatalf("chat id lacks a 128-bit random suffix: %q", c.ID)
		}
		if seen[suffix] {
			t.Fatalf("repeated random suffix: %s", suffix)
		}
		seen[suffix] = true
	}
}
