package app

import (
	"fmt"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/dbclient"
)

// Dev actions for the perf guards (internal/testkit/perf_ui_test.go):
// big canned states that would take minutes to make by hand.

// aiLongChatDemo fills the chat with n messages (questions and Markdown
// answers with a code block) and a last question whose answer streams
// (aiTokenDemo).
func (m *Model) aiLongChatDemo(n int) {
	s := m.aiS()
	s.conv.Messages = s.conv.Messages[:0]
	for i := 0; i < n/2; i++ {
		s.conv.Messages = append(s.conv.Messages,
			ai.StoredMessage{Role: ai.StoredUser, Text: fmt.Sprintf("Question %d: why does the list jump when I scroll it fast?", i)},
			ai.StoredMessage{Role: ai.StoredAssistant, Text: fmt.Sprintf("## Answer %d\n\nThe **list** redraws on every `MouseWheel` event, so *each* row is styled again.\n\n", i) +
				"1. first point with `code`\n2. second point that is long enough to wrap twice in a narrow panel like this one\n\n" +
				"```go\nfunc (m *Model) scroll(d int) {\n\tm.top = max(0, m.top+d)\n\tm.dirty = true\n}\n```\n\n> tip: cache the rows"})
	}
	s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredUser, Text: "And how do I fix it?"})
	s.scroll, s.turnMsg, s.streaming = 0, len(s.conv.Messages)-1, true
}

// aiTokenDemo streams one token of the answer, as a model's text event.
func (m *Model) aiTokenDemo(text string) {
	s := m.aiS()
	// The returned command waits on the turn's channel; there is none.
	_ = m.onAIChatEvent(aiChatEventMsg{gen: s.chatGen, ev: ai.Event{Kind: ai.EventText, Text: text}})
}

// databaseBigResultDemo shows a result of n rows in the grid.
func (m *Model) databaseBigResultDemo(arg string) tea.Cmd {
	n, err := strconv.Atoi(arg)
	if err != nil || n <= 0 {
		return nil
	}
	m.databasesDemo("")
	plans := []string{"free", "pro", "team"}
	res := dbclient.Result{
		Columns: []dbclient.Column{{Name: "id"}, {Name: "email"}, {Name: "plan"}, {Name: "created_at"}},
		Elapsed: 118 * time.Millisecond,
		Rows:    make([][]dbclient.Cell, 0, n),
	}
	for i := 0; i < n; i++ {
		res.Rows = append(res.Rows, []dbclient.Cell{
			{Text: strconv.Itoa(n - i), Kind: "number"}, {Text: fmt.Sprintf("user%d@example.com", i)}, {Text: plans[i%3]},
			{Text: fmt.Sprintf("2026-10-18 09:%02d:%02d+00", (i/60)%60, i%60)},
		})
	}
	r := &dbRun{conn: "local-pg", sql: "SELECT id, email, plan, created_at\nFROM users\nORDER BY id DESC;", by: "user", line: 3, result: res}
	s := m.dbConsole()
	s.tabs, s.tab = nil, 0
	s.placeRun(r)
	s.cache, s.size, s.page, s.sortCol, s.filter = nil, 100, 0, -1, ""
	s.view = dbViewGrid
	s.trans, s.trow = false, 0
	return m.databaseShowPage()
}
