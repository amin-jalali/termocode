package ai

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
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
		if err := s.Save(&c); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(s.List()); n != maxChats {
		t.Fatalf("kept %d", n)
	}
	if DeriveTitle(nil) != defaultChatTitle {
		t.Error("default title")
	}
}
