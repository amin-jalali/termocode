package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// transcriptDump is every transcript line as drawn, with its buttons,
// message and selection data.
func transcriptDump(m Model, width int) string {
	var b strings.Builder
	for _, l := range m.buildAITranscriptAt(width, aiActivityView{}) {
		fmt.Fprintf(&b, "%q %v %d %d %+v\n", renderAILine(l, width), l.buttons, l.msg, l.block, l.sel)
	}
	return b.String()
}

// The per-message transcript cache (segCache) gives the same lines as
// a cold build after each kind of change: a message's text, its model,
// a new message after it (the "run by" line), the width, the theme and
// the model label.
func TestAITranscriptSegmentCacheInvalidates(t *testing.T) {
	m := chatPerfModel(t, 6)
	s := m.aiS()
	cold := func(width int) string {
		saved := s.segCache
		s.segCache = nil
		defer func() { s.segCache = saved }()
		return transcriptDump(m, width)
	}
	check := func(step string, width int) {
		t.Helper()
		got := transcriptDump(m, width)
		if want := cold(width); got != want {
			t.Fatalf("%s: cached transcript differs from a cold build:\ngot:\n%s\nwant:\n%s", step, got, want)
		}
		if len(s.segCache) == 0 {
			t.Fatalf("%s: nothing cached", step)
		}
	}
	check("first", 60)
	check("again", 60)
	s.conv.Messages[1].Text = "## Other\n\nA **new** answer."
	check("text", 60)
	s.conv.Messages[3].Model = "slot-model"
	check("model", 60)
	s.conv.Messages[0].Rules, s.conv.Messages[0].Lessons = "AGENTS.md (1 KB)", 2
	check("rules", 60)
	s.conv.Messages[5].Memory = 3
	check("memory", 60)
	s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredUser, Text: "start a run"})
	check("new question", 60)
	s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredAssistant, Text: "run",
		Run: &ai.StoredRun{By: &ai.RunBy{ChatTitle: "fix the parser"}}})
	check("run after", 60)
	check("width", 44)
	s.cfg.Model = "another-model"
	check("label", 44)
	theme.ApplyTheme("catppuccin-latte")
	check("theme", 44)
	s.conv.Messages = s.conv.Messages[:2]
	check("shorter", 44)
}
