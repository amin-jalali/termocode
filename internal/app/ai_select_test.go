package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/theme"
)

// selAll is a range over every row of lines.
func selAll(lines []aiLine) (aiSelPos, aiSelPos) {
	return aiSelPos{0, 0}, aiSelPos{len(lines) - 1, 1 << 20}
}

// rowOf finds the first row whose selectable text contains s.
func rowOf(t *testing.T, lines []aiLine, s string) (int, aiLine) {
	t.Helper()
	for i, l := range lines {
		if l.sel.ok && strings.Contains(l.sel.text, s) {
			return i, l
		}
	}
	t.Fatalf("no row with %q", s)
	return 0, aiLine{}
}

// colOf is the panel column where s starts on row l.
func colOf(l aiLine, s string) int {
	i := strings.Index(l.sel.text, s)
	return l.sel.x + runewidth.StringWidth(l.sel.text[:i])
}

// Spec: a word-wrapped paragraph copies back as one line (wrap points
// become the spaces they replaced), with no indent or padding.
func TestAISelWrappedParagraph(t *testing.T) {
	text := "Hello wonderful world of terminals, where every cell counts and words wrap."
	lines := renderChatMarkdown(text, 30, aiIndent, 0, false)
	if len(lines) < 3 {
		t.Fatalf("expected the paragraph to wrap, got %d rows", len(lines))
	}
	a, b := selAll(lines)
	if got := aiSelectionText(lines, a, b); got != text {
		t.Errorf("whole paragraph:\n got %q\nwant %q", got, text)
	}
	// Part: from "wonderful" to the end of "terminals" on a later row.
	r0, l0 := rowOf(t, lines, "wonderful")
	r1, l1 := rowOf(t, lines, "terminals")
	start := aiSelPos{r0, colOf(l0, "wonderful")}
	end := aiSelPos{r1, colOf(l1, "terminals") + len("terminals")}
	if got, want := aiSelectionText(lines, start, end), "wonderful world of terminals"; got != want {
		t.Errorf("part: got %q, want %q", got, want)
	}
}

// Spec: code blocks copy as their source: a fully selected wrapped code
// line gives the real line (tabs kept, no ↪ marker); a part of a row
// gives the text as shown.
func TestAISelCodeBlock(t *testing.T) {
	long := "\tresult := compute(alpha, beta, gamma, delta, epsilon, zeta)"
	text := "Intro.\n\n```go\nfunc x() {\n" + long + "\n}\n```\n\nAfter."
	lines := renderChatMarkdown(text, 40, aiIndent, 3, false)
	a, b := selAll(lines)
	want := "Intro.\n\nfunc x() {\n" + long + "\n}\n\nAfter."
	if got := aiSelectionText(lines, a, b); got != want {
		t.Errorf("whole answer:\n got %q\nwant %q", got, want)
	}
	for _, l := range lines {
		if l.sel.ok && strings.Contains(l.sel.text, "↪") {
			t.Errorf("continuation marker is selectable: %q", l.sel.text)
		}
	}
	r, l := rowOf(t, lines, "compute")
	start := aiSelPos{r, colOf(l, "compute")}
	end := aiSelPos{r, colOf(l, "compute") + len("compute")}
	if got := aiSelectionText(lines, start, end); got != "compute" {
		t.Errorf("part of a code row: %q", got)
	}
	// Rows know their message and code block (right-click menu).
	if l.msg != 4 || l.block != 1 {
		t.Errorf("code row msg/block = %d/%d, want 4/1", l.msg, l.block)
	}
}

// Spec: wide (CJK) characters are selected whole: a range that starts on
// the second cell of a wide character still includes it.
func TestAISelWideChars(t *testing.T) {
	text := "Wide glyphs: 日本語テキスト here"
	lines := renderChatMarkdown(text, 60, aiIndent, 0, false)
	r, l := rowOf(t, lines, "日本語")
	c := colOf(l, "日本語")
	start := aiSelPos{r, c + 1} // second cell of 日
	end := aiSelPos{r, c + 6}   // through 語
	if got := aiSelectionText(lines, start, end); got != "日本語" {
		t.Errorf("got %q, want 日本語", got)
	}
	c0, c1, ok := aiSelRowSpan(l, r, start, end)
	if !ok || c0 != c || c1 != c+6 {
		t.Errorf("highlight span = %d..%d (%v), want %d..%d", c0, c1, ok, c, c+6)
	}
}

// Spec: double-click selects the word under the pointer; triple-click
// the whole (wrapped) paragraph.
func TestAISelWordAndLine(t *testing.T) {
	text := "First paragraph that is long enough to wrap around.\n\nSecond one."
	lines := renderChatMarkdown(text, 30, aiIndent, 0, false)
	r, l := rowOf(t, lines, "enough")
	a, b, ok := aiWordAt(lines, aiSelPos{r, colOf(l, "enough") + 2})
	if !ok || aiSelectionText(lines, a, b) != "enough" {
		t.Errorf("word: %q (%v)", aiSelectionText(lines, a, b), ok)
	}
	a, b, ok = aiLineAt(lines, aiSelPos{r, colOf(l, "enough")})
	if got := aiSelectionText(lines, a, b); !ok || got != "First paragraph that is long enough to wrap around." {
		t.Errorf("line: %q (%v)", got, ok)
	}
}

// Spec: list items keep their marker; a wrapped item joins back into one
// line; user bubbles copy without the bubble padding.
func TestAISelListAndBubble(t *testing.T) {
	old := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	t.Cleanup(func() { theme.CurrentIconMode = old })
	text := "- first item that is long enough to wrap\n- second"
	lines := renderChatMarkdown(text, 30, aiIndent, 0, false)
	a, b := selAll(lines)
	if got, want := aiSelectionText(lines, a, b), "• first item that is long enough to wrap\n• second"; got != want {
		t.Errorf("list:\n got %q\nwant %q", got, want)
	}
	msg := "please explain this function to me in detail\nand the tests"
	bub := aiUserBubble(msg, 40, 0)
	a, b = selAll(bub)
	if got := aiSelectionText(bub, a, b); got != msg {
		t.Errorf("bubble:\n got %q\nwant %q", got, msg)
	}
}

// Spec: with no clipboard at all (no display, no tool, a test run), a
// copy says so with a fix instead of claiming "Copied".
func TestCopyWithoutClipboardWarns(t *testing.T) {
	perfColour(t)
	m := perfModel(t, true)
	m.copyWithToast("some text", "Copied 9 chars")
	view := stripANSI(m.View())
	if strings.Contains(view, "Copied 9 chars") || !strings.Contains(view, "clipboard") {
		t.Errorf("want a copy-failed toast, got:\n%s", view)
	}
	var got string
	restore := SetClipboardSink(func(s string) { got = s })
	defer restore()
	m.copyWithToast("some text", "Copied 9 chars")
	if got != "some text" || !strings.Contains(stripANSI(m.View()), "Copied 9 chars") {
		t.Errorf("sink copy: got %q", got)
	}
}

// Spec: dragging above the transcript scrolls it up and extends the
// selection to the top row; the drawn transcript rows stay cached.
func TestAISelDragAutoScroll(t *testing.T) {
	perfColour(t)
	m := perfModel(t, true)
	width := m.actionsColumnWidth()
	g := m.aiPanelGeom(width, m.h)
	x := m.w - width + 5
	_ = m.View()
	cached := m.ai.transcript.rows
	m.handleAIPanelMouse(tea.MouseMsg{X: x, Y: g.transTop + 3, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m.handleAIPanelMouse(tea.MouseMsg{X: x, Y: 0, Type: tea.MouseMotion, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	if m.ai.scroll != 1 {
		t.Errorf("scroll = %d after dragging above, want 1", m.ai.scroll)
	}
	lines := m.aiTranscript(width)
	top, _ := aiVisibleRange(len(lines), g.transH, m.ai.scroll)
	if m.ai.sel.head.row != top {
		t.Errorf("head row = %d, want the top visible row %d", m.ai.sel.head.row, top)
	}
	_ = m.View()
	if &m.ai.transcript.rows[0] != &cached[0] {
		t.Errorf("selection rebuilt the transcript cache")
	}
	m.handleAIPanelMouse(tea.MouseMsg{X: x, Y: 0, Type: tea.MouseRelease, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if m.ai.sel.dragging {
		t.Errorf("still dragging after release")
	}
}
