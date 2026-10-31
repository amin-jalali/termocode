package app

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/rtl"
	"github.com/amin-jalali/termocode/internal/theme"
)

// A long answer is drawn in parts so a frame costs what changed, not
// the whole answer: while it streams, each new chunk used to redraw
// every row from the top (a 1 MB answer: about a second a frame). The
// text up to the last blank line outside a code block is drawn once and
// kept (aiMDStream); only the rest is drawn again. A Markdown block
// never spans a blank line outside a code block, and a part that starts
// after one draws as it would after the rows before it, so the parts
// put together are the rows renderChatMarkdown gives for the whole
// text.

// aiMDStream is the kept, drawn start of answer i.
type aiMDStream struct {
	ok     bool
	i      int
	width  int
	theme  theme.RenderKey
	rtl    rtl.Mode
	text   string // latest immutable source; replaced on every append
	n      int
	lines  []aiLine
	blocks int // code blocks in text
}

// renderAnswerParts draws answer i like renderChatMarkdown, reusing the
// kept start when text goes on from it.
func (s *aiState) renderAnswerParts(i int, text string, width int, streaming bool) []aiLine {
	k := theme.CurrentRenderKey()
	st := &s.mdStream
	if !st.ok || st.i != i || st.width != width || st.theme != k || st.rtl != rtlMode || !st.startsWith(text) {
		*st = aiMDStream{ok: true, i: i, width: width, theme: k, rtl: rtlMode}
	}
	if p := mdSafeSplit(text, st.n); p > st.n {
		part := strings.Clone(text[st.n:p])
		lines, blocks := renderChatMarkdownFrom(part, width, aiIndent, i, st.blocks, streaming)
		st.lines = append(st.lines, lines...)
		st.n, st.blocks = p, blocks
	}
	tail, _ := renderChatMarkdownFrom(text[st.n:], width, aiIndent, i, st.blocks, streaming)
	out := trimBlankAILines(append(st.lines, tail...))
	st.text = text
	if !streaming {
		*st = aiMDStream{} // the answer is done: its rows are kept once, by the caller
	}
	return out
}

// startsWith reports whether text begins with the drawn start.
func (st *aiMDStream) startsWith(text string) bool {
	return appendedText(text, st.text)
}

// mdSafeSplit returns the end of the last blank line of text after from
// (a line start outside a code block) that is outside a code block, or
// from when there is none.
func mdSafeSplit(text string, from int) int {
	split, inCode := from, false
	for i := from; i < len(text); {
		j := strings.IndexByte(text[i:], '\n')
		if j < 0 {
			break // the last line is not finished
		}
		line := strings.TrimSpace(strings.ReplaceAll(text[i:i+j], "\r", ""))
		i += j + 1
		switch {
		case strings.HasPrefix(line, "```"):
			inCode = !inCode
		case line == "" && !inCode:
			split = i
		}
	}
	return split
}
