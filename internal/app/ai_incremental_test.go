package app

import (
	"reflect"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

func TestIncrementalCleanMatchesWhole(t *testing.T) {
	var c aiStreamCleaner
	for _, source := range []string{"hello\t世界\n\x1b[31mred\x1b[0m\n", "\x1b]52;c;payload\nmore\x1b\\tail\u202e\r\n", "replacement\x00answer", "\xff\xc2\xa0"} {
		for end := 0; end <= len(source); end++ {
			if got, want := c.clean(source[:end]), aiCleanText(source[:end]); got != want {
				t.Fatalf("prefix %q: got %q, want %q", source[:end], got, want)
			}
		}
	}
}

func TestGrowingTranscriptMatchesFresh(t *testing.T) {
	s := &aiState{streaming: true, conv: ai.Conversation{Messages: []ai.StoredMessage{{Role: ai.StoredUser, Text: "question"}, {Role: ai.StoredAssistant}}}}
	m := Model{ai: s}
	source := strings.Repeat("A paragraph with **bold** text.\n\n", 100) + "```go\n\treturn 1\n```\n\nfinal\x1b[31m text"
	for end := 1; end <= len(source); end = min(end+137, len(source)) {
		s.conv.Messages[1].Text = source[:end]
		got := m.buildAITranscriptAt(80, aiActivityView{})
		fresh := Model{ai: &aiState{streaming: true, conv: s.conv}}
		want := fresh.buildAITranscriptAt(80, aiActivityView{})
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("transcript differs at %d", end)
		}
		if end == len(source) {
			break
		}
	}
	if s.streamTranscript.reused == 0 {
		t.Fatal("growing answer did not reuse transcript rows")
	}
	// Edits and width changes must invalidate every dependent row.
	s.conv.Messages[0].Text = "different question"
	s.conv.Messages[1].Text = "replacement\n\nanswer"
	got := m.buildAITranscriptAt(50, aiActivityView{})
	fresh := Model{ai: &aiState{streaming: true, conv: s.conv}}
	if !reflect.DeepEqual(got, fresh.buildAITranscriptAt(50, aiActivityView{})) {
		t.Fatal("stale rows after edit/resize")
	}
}
