package app

import (
	"strings"
	"unicode/utf8"
	"unsafe"

	"github.com/amin-jalali/termocode/internal/ai"
)

// appendedText uses the immutable buffer shared by streamed strings.
// A buffer growth needs one prefix check; ordinary chunks need none.
func appendedText(text, previous string) bool {
	return len(text) >= len(previous) && (unsafe.StringData(text) == unsafe.StringData(previous) || strings.HasPrefix(text, previous))
}

// aiStreamCleaner keeps the safe prefix. An incomplete escape or UTF-8
// rune stays in the tail, so later bytes produce exactly aiCleanText's
// whole-string result, including split OSC terminators.
type aiStreamCleaner struct {
	raw, cleaned string
	n            int
	buf          aiTextAppender
}

func (c *aiStreamCleaner) clean(text string) string {
	if !appendedText(text, c.raw) {
		*c = aiStreamCleaner{}
	}
	end := c.n
	for end < len(text) {
		if text[end] == 0x1b {
			match := outputANSIRe.FindStringIndex(text[end:])
			if match == nil || match[0] != 0 {
				break
			}
			end += match[1]
			continue
		}
		if !utf8.FullRuneInString(text[end:]) {
			break
		}
		_, n := utf8.DecodeRuneInString(text[end:])
		end += n
	}
	if end > c.n {
		c.cleaned = c.buf.add(c.cleaned, aiCleanText(text[c.n:end]))
		c.n = end
	}
	c.raw = text
	if end == len(text) {
		return c.cleaned
	}
	return c.cleaned + aiCleanText(text[end:])
}

// aiStreamTranscript owns the assembled rows while the last answer
// grows. Only rows after its stable Markdown prefix are overwritten.
// Returned render slices are borrowed until the next build.
type aiStreamTranscript struct {
	keys                []aiSegKey
	fork                string
	runGen, restoreGen  int
	lines               []aiLine
	start, turn, reused int
	active              bool
}

func (c *aiStreamTranscript) prepare(m Model, width int, resizing bool) bool {
	s := m.ai
	last := len(s.conv.Messages) - 1
	if resizing || !s.streaming || last < 0 || s.conv.Messages[last].Role != ai.StoredAssistant || s.conv.Messages[last].Run != nil {
		*c = aiStreamTranscript{}
		return false
	}
	keys := make([]aiSegKey, len(s.conv.Messages))
	for i := range keys {
		keys[i] = m.aiSegKeyFor(i, width)
	}
	keys[last].text = ""
	match := c.active && c.fork == s.conv.ForkNote() && c.runGen == s.runGen && c.restoreGen == s.restoreGen && len(keys) == len(c.keys)
	if match {
		for i := range keys {
			if keys[i] != c.keys[i] {
				match = false
				break
			}
		}
	}
	c.keys, c.fork, c.runGen, c.restoreGen = keys, s.conv.ForkNote(), s.runGen, s.restoreGen
	c.active, c.reused = true, 0
	return match
}
