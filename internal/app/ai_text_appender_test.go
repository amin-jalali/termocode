package app

import (
	"strings"
	"testing"
)

// Streamed tokens join to the full text; texts returned earlier never
// change; a text changed elsewhere (not the appender's last result) is
// taken as the new start.
func TestAITextAppender(t *testing.T) {
	var a aiTextAppender
	cur := ""
	var seen []string
	for i := 0; i < 500; i++ {
		cur = a.add(cur, "ab")
		seen = append(seen, cur)
	}
	if cur != strings.Repeat("ab", 500) {
		t.Fatalf("text %q", cur[:20])
	}
	for i, s := range seen {
		if s != strings.Repeat("ab", i+1) {
			t.Fatalf("earlier text %d changed", i)
		}
	}
	other := "edited" + cur[6:] // same length, other bytes
	if got := a.add(other, "!"); got != other+"!" {
		t.Errorf("after an edit: %q", got[:10])
	}
	if got := a.add("x", "y"); got != "xy" {
		t.Errorf("new text: %q", got)
	}
	if cur != strings.Repeat("ab", 500) {
		t.Error("an old text changed")
	}
}
