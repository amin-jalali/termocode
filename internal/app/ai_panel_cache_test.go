package app

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// freshActionsPanel renders the chat panel with every render cache
// emptied first (transcript, rendered answers, input box, header). While
// the splitter is dragged the rendered answers are kept: by design they
// are reused at any width until the drag ends (renderAnswer).
func freshActionsPanel(m Model, w int) string {
	s := m.ai
	s.transcript, s.inputCache = aiTranscriptCache{}, aiInputCache{}
	if m.dragKind != dragActionsLeft {
		s.mdCache = nil
	}
	actionsHeaderMemo.Lock()
	actionsHeaderMemo.ok = false
	actionsHeaderMemo.Unlock()
	return m.renderActionsPanel(w, m.h)
}

// TestAIPanelCacheMatchesFreshDraw drives the chat panel through random
// changes (messages added and streamed, streaming on / off, width,
// resizing, input text / caret / focus, scroll, attachments, header
// hover / pin, theme / icons / colour profile) and checks after every
// step that the cached panel equals a draw with all caches emptied.
func TestAIPanelCacheMatchesFreshDraw(t *testing.T) {
	prevProfile, prevIcons := lipgloss.ColorProfile(), theme.CurrentIconMode
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(prevProfile)
		theme.CurrentIconMode = prevIcons
		theme.ApplyTheme("vscode-dark-plus")
	})
	m := aiTestModel(t)
	m.actionsOpen, m.actionsPinned = true, true
	s := m.ai
	s.provider = fakeAIProvider{}
	texts := []string{"", "Hi", "```go\nunfinished(", "Explain **this** file:\n\n```go\nfunc x() {\n\treturn 1\n}\n```\n", "a longer user question that wraps in the panel", "1. one\n2. two\n"}
	r := rand.New(rand.NewSource(5))
	w := 50
	for step := 0; step < 1500; step++ {
		_ = m.renderActionsPanel(w, m.h) // fill the caches
		op := r.Intn(12)
		switch op {
		case 0:
			role := []string{ai.StoredUser, ai.StoredAssistant, ai.StoredTool, ai.StoredError}[r.Intn(4)]
			s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: role, Text: texts[r.Intn(len(texts))]})
		case 1: // the streamed answer grows (same slice, new text)
			if n := len(s.conv.Messages); n > 0 {
				s.conv.Messages[n-1].Text += texts[r.Intn(len(texts))]
			}
		case 2:
			s.streaming = !s.streaming
		case 3:
			w = 24 + r.Intn(60)
		case 4:
			if r.Intn(2) == 0 {
				m.dragKind = dragActionsLeft
			} else {
				m.dragKind = dragNone
			}
		case 5:
			in := []rune(texts[r.Intn(len(texts))])
			s.input = in[:r.Intn(min(10, len(in))+1)]
			s.caret = r.Intn(len(s.input) + 1)
		case 6:
			s.focused = !s.focused
		case 7:
			s.scroll = r.Intn(20)
		case 8:
			s.attachments = nil
			if r.Intn(2) == 0 {
				s.attachments = []aiAttachment{{Label: fmt.Sprintf("f%d.go", r.Intn(3))}}
			}
		case 9:
			m.hoverX, m.hoverY = m.w-w+r.Intn(w), r.Intn(3)
			m.actionsPinned = r.Intn(2) == 0
		case 10:
			switch r.Intn(3) {
			case 0:
				theme.ApplyTheme([]string{"vscode-dark-plus", "github-dark", "one-dark"}[r.Intn(3)])
			case 1:
				theme.CurrentIconMode = []theme.IconMode{theme.IconModeASCII, theme.IconModeUnicode, theme.IconModeNerdFont}[r.Intn(3)]
			case 2:
				lipgloss.SetColorProfile([]termenv.Profile{termenv.TrueColor, termenv.ANSI256}[r.Intn(2)])
			}
		case 11:
			if r.Intn(4) == 0 {
				s.conv.Messages = nil // new chat
			} else { // an answer starts: empty until the first delta
				s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredAssistant})
			}
		}
		got := m.renderActionsPanel(w, m.h)
		if want := freshActionsPanel(m, w); got != want {
			t.Fatalf("step %d (op %d, drag %v): cached chat panel differs from a fresh draw", step, op, m.dragKind == dragActionsLeft)
		}
	}
}
