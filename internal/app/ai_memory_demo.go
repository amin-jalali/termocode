package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/aimemory"
)

// aiMemoryDemo sets up the memory shots (dev action
// "memorydemo:chat|ask|picker|forget"): a sample turn that used tools and
// saved two lessons, the ask-once card after it, AI: Memory… with sample
// lessons (never the workspace's file), and a lesson's Forget card.
func (m *Model) aiMemoryDemo(arg string) tea.Cmd {
	s := m.aiS()
	s.conv.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "Why does the toast slide out twice?", Lessons: 2},
		{Role: ai.StoredTool, Text: "⚙ Search \"leaving\" in internal/toast → 3 matches"},
		{Role: ai.StoredTool, Text: "⚙ Run go test ./internal/toast → FAIL (1 test)"},
		{Role: ai.StoredAssistant, Text: aiDemoAnswer, Memory: 2},
	}
	s.scroll = 0
	s.slotFake = aiDemoSlots(nil, nil) // a quick model slot: the card may ask
	lessons := []aimemory.Lesson{
		{Date: "2026-10-21", Title: "Golden frames need a look first", Text: "Run the shotkit golden test; accept changes with -update only after viewing the frame.", Paths: []string{"internal/devtools/shotkit"}},
		{Date: "2026-10-23", Title: "Run go test with -race", Text: "CI runs every package with -race; a data race fails the build.", Paths: nil},
		{Date: "2026-10-25", Title: "Toast ticks can fire twice", Text: "Tick runs again before the slide ends: check leaving before starting a slide.", Paths: []string{"internal/toast/model.go"}},
		{Date: "2026-10-25", Title: "Toast tests need the frozen clock", Text: "Use clock.Set in toast tests; time.Now makes them flaky.", Paths: []string{"internal/toast/model_test.go"}},
	}
	for i := range lessons {
		lessons[i].ID = aimemory.NewID(lessons[i].Title, lessons[i].Text)
	}
	s.memFake = &lessons
	switch arg {
	case "ask":
		return m.askAIMemory(0)
	case "picker", "forget":
		sess := loadSession()
		sess.AIMemory = aiMemoryOn
		saveSession(sess)
		cmd := m.openAIMemory()
		if arg == "forget" {
			m.pickerOpen = false
			m.onAIMemoryPicked(lessons[2].ID)
			return m.answerAIMemoryCard(confirmKindAIMemoryLesson, "forget")
		}
		return cmd
	}
	return nil
}
