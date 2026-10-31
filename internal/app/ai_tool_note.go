package app

import "strings"

// Tool notes (MCPX-38): a tool shows the user a note the model does not
// get (an MCP result item for the user only). The chat keeps the notes
// of the running tool and adds them under its line when it ends; the
// history for the model leaves them out.

// aiToolNoteMark starts a note line in the transcript.
const aiToolNoteMark = "↳ for you: "

// aiMaxToolNote caps one note.
const aiMaxToolNote = 2000

// aiToolNoteLine reports a note line.
func aiToolNoteLine(text string) bool { return strings.HasPrefix(text, aiToolNoteMark) }

// aiForUserOnly reports a stored tool line that is for the user, not the
// model (a fallback line, a tool note).
func aiForUserOnly(text string) bool { return aiFallbackLine(text) || aiToolNoteLine(text) }

// aiAddToolNote keeps a note of the running tool.
func (s *aiState) aiAddToolNote(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if r := []rune(text); len(r) > aiMaxToolNote {
		text = string(r[:aiMaxToolNote-1]) + "…"
	}
	s.toolNotes = append(s.toolNotes, aiToolNoteMark+text)
}

// aiTakeToolNotes returns the kept notes and forgets them.
func (s *aiState) aiTakeToolNotes() []string {
	n := s.toolNotes
	s.toolNotes = nil
	return n
}
