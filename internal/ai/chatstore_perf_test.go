package ai

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/perfbudget"
)

// perfChat is a chat of n messages: user questions, long answers and
// tool lines, like a long agent session.
func perfChat(n int) Conversation {
	c := NewConversation()
	answer := strings.Repeat("Here is the change, with a code block:\n```go\nfunc f() int { return 42 }\n```\n", 8)
	for i := 0; i < n; i++ {
		switch i % 4 {
		case 0:
			c.Messages = append(c.Messages, StoredMessage{Role: StoredUser, Text: fmt.Sprintf("Question %d: please fix the tests", i)})
		case 1, 2:
			c.Messages = append(c.Messages, StoredMessage{Role: StoredTool, Text: fmt.Sprintf("⚙ Read internal/pkg/file%d.go → 120 lines", i)})
		default:
			c.Messages = append(c.Messages, StoredMessage{Role: StoredAssistant, Text: answer, Model: "fake-1"})
		}
	}
	return c
}

// perfStore is a store with 49 other chats of 200 messages, and the
// 2 000-message chat c (saved once).
func perfStore(tb testing.TB) (ChatStore, Conversation) {
	tb.Helper()
	s := ChatStore{Dir: tb.TempDir()}
	for i := 0; i < maxChats-1; i++ {
		o := perfChat(200)
		if err := s.Save(&o); err != nil {
			tb.Fatal(err)
		}
	}
	c := perfChat(2000)
	if err := s.Save(&c); err != nil {
		tb.Fatal(err)
	}
	return s, c
}

func medianDur(ds []time.Duration) time.Duration {
	s := slices.Clone(ds)
	slices.Sort(s)
	return s[len(s)/2]
}

// Perf guard (PERF-7): saving (at the end of each turn) and loading a
// 2 000-message chat in a store with 50 chats.
func TestPerfChatSaveLoad2k(t *testing.T) {
	perfbudget.Require(t)
	s, c := perfStore(t)
	var saves, loads []time.Duration
	for i := 0; i < 9; i++ {
		c.Messages = append(c.Messages, StoredMessage{Role: StoredUser, Text: "one more"})
		t0 := time.Now()
		if err := s.Save(&c); err != nil {
			t.Fatal(err)
		}
		saves = append(saves, time.Since(t0))
		t1 := time.Now()
		got, err := s.Load(c.ID)
		if err != nil || len(got.Messages) != len(c.Messages) {
			t.Fatalf("load: %d %v", len(got.Messages), err)
		}
		loads = append(loads, time.Since(t1))
	}
	ms := func(d time.Duration) float64 { return float64(d.Microseconds()/100) / 10 }
	t.Logf("save %v, load %v", medianDur(saves), medianDur(loads))
	perfbudget.Check(t, "chat_save_2k_messages", ms(medianDur(saves)))
	perfbudget.Check(t, "chat_load_2k_messages", ms(medianDur(loads)))
}

func BenchmarkChatStoreSave2k(b *testing.B) {
	s, c := perfStore(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.Save(&c); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkChatStoreLoad2k(b *testing.B) {
	s, c := perfStore(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Load(c.ID); err != nil {
			b.Fatal(err)
		}
	}
}

// The history list / Runs: every chat of a full store (50 chats).
func BenchmarkChatStoreList50(b *testing.B) {
	s, _ := perfStore(b)
	b.ReportAllocs()
	for b.Loop() {
		if n := len(s.List()); n != maxChats {
			b.Fatalf("%d chats", n)
		}
	}
}
