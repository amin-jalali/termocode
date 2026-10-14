package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/rtl"
)

// withRTL runs f with the rtl mode set, then restores it.
func withRTL(t *testing.T, mo rtl.Mode) {
	t.Helper()
	old := rtlMode
	rtlMode = mo
	t.Cleanup(func() { rtlMode = old })
}

func plainRows(lines []aiLine, width int) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(renderAILine(l, width))
	}
	return out
}

// Spec: a Persian answer shows shaped letters in reading order, right
// aligned, and every row is exactly the panel width.
func TestAIRTLAnswerShapedRightAligned(t *testing.T) {
	withRTL(t, rtl.Termocode)
	const w = 40
	lines := renderChatMarkdown("سلام دنیا", w, aiIndent, 0, false)
	rows := plainRows(lines, w)
	want := rtl.Visual("سلام دنیا", rtl.RTL)
	found := false
	for i, r := range rows {
		if ansi.StringWidth(renderAILine(lines[i], w)) != w {
			t.Fatalf("row %d is not %d wide: %q", i, w, r)
		}
		if strings.Contains(r, want) {
			found = true
			// Right aligned: the text ends one cell before the edge.
			if end := strings.TrimRight(r, " "); ansi.StringWidth(end) != w-1 {
				t.Errorf("not right aligned: %q", r)
			}
		}
	}
	if !found {
		t.Fatalf("no shaped visual text in %q", rows)
	}
	if strings.Contains(strings.Join(rows, "\n"), "سلام") {
		t.Error("logical (unshaped) text still on screen")
	}
}

// Spec: English answers are drawn exactly as before.
func TestAIRTLLeavesLTRAlone(t *testing.T) {
	text := "# Title\n\nHello **world**, `code` here.\n\n- one\n- two\n\n> quote"
	withRTL(t, rtl.Off)
	before := renderChatMarkdown(text, 40, aiIndent, 0, false)
	rtlMode = rtl.Termocode
	after := renderChatMarkdown(text, 40, aiIndent, 0, false)
	if len(before) != len(after) {
		t.Fatalf("rows %d vs %d", len(before), len(after))
	}
	for i := range before {
		if before[i].styled != after[i].styled || before[i].sel.text != after[i].sel.text || before[i].sel.x != after[i].sel.x {
			t.Errorf("row %d changed:\n%q\n%q", i, before[i].styled, after[i].styled)
		}
	}
}

// Spec: mode "off" / "terminal" sends the logical text unchanged.
func TestAIRTLOffKeepsLogical(t *testing.T) {
	for _, mo := range []rtl.Mode{rtl.Off, rtl.Terminal} {
		withRTL(t, mo)
		rows := plainRows(renderChatMarkdown("سلام دنیا", 40, aiIndent, 0, false), 40)
		if !strings.Contains(strings.Join(rows, "\n"), "سلام دنیا") {
			t.Errorf("%v: logical text missing: %q", mo, rows)
		}
	}
}

// Spec: a long Persian paragraph is wrapped in logical order (the first
// words on the first row), then each row is reordered and right aligned;
// copying everything gives the original text back.
func TestAIRTLWrapReorderAndCopy(t *testing.T) {
	withRTL(t, rtl.Termocode)
	text := "این یک متن فارسی بلند است که باید در چند خط شکسته شود و هر خط از راست خوانده شود"
	const w = 30
	lines := renderChatMarkdown(text, w, aiIndent, 0, false)
	if len(lines) < 3 {
		t.Fatalf("expected wrapping, got %d rows", len(lines))
	}
	rows := plainRows(lines, w)
	if first := rtl.Visual("این یک", rtl.RTL); !strings.HasSuffix(strings.TrimRight(rows[0], " "), first) {
		t.Errorf("first row does not end (on the right) with the first words: %q", rows[0])
	}
	for i, r := range rows {
		if ansi.StringWidth(strings.TrimRight(r, " ")) != w-1 {
			t.Errorf("row %d not right aligned: %q", i, r)
		}
	}
	a, b := selAll(lines)
	if got := aiSelectionText(lines, a, b); got != text {
		t.Errorf("copy:\n got %q\nwant %q", got, text)
	}
	// The leftmost visual cell of the first row is the last logical
	// letter of that row: a one-cell selection copies that letter.
	r0 := lines[0].sel
	start, end := aiSelPos{0, r0.x}, aiSelPos{0, r0.x + 1}
	lr := []rune(strings.TrimSpace(rtl.Pick(lines[0].sel.bidi.rs, lines[0].sel.bidi.gs)))
	if got := aiSelectionText(lines, start, end); got != string(lr[len(lr)-1]) {
		t.Errorf("one visual cell copies %q, want the logical letter", got)
	}
}

// Spec: mixed text keeps LTR runs (English, numbers, inline code) in
// order; parentheses are mirrored.
func TestAIRTLMixedRuns(t *testing.T) {
	withRTL(t, rtl.Termocode)
	lines := renderChatMarkdown("نسخه `go 1.26` را (حتما) نصب کن 123", 60, aiIndent, 0, false)
	row := plainRows(lines, 60)[0]
	for _, s := range []string{" go 1.26 ", "123"} {
		if !strings.Contains(row, s) {
			t.Errorf("LTR run %q broken: %q", s, row)
		}
	}
	if !strings.Contains(row, "("+rtl.Visual("حتما", rtl.RTL)+")") {
		t.Errorf("brackets not mirrored: %q", row)
	}
	a, b := selAll(lines)
	if got := aiSelectionText(lines, a, b); got != "نسخه  go 1.26  را (حتما) نصب کن 123" {
		t.Errorf("copy: %q", got)
	}
}

// Spec: code blocks stay LTR (no reorder) even with Persian comments.
func TestAIRTLCodeBlockStaysLTR(t *testing.T) {
	withRTL(t, rtl.Termocode)
	lines := renderChatMarkdown("```go\nx := 1 // سلام دنیا\n```", 50, aiIndent, 0, false)
	joined := strings.Join(plainRows(lines, 50), "\n")
	// The code reads left to right; only the Persian comment is turned.
	if want := "x := 1 // " + rtl.Visual("سلام دنیا", rtl.RTL); !strings.Contains(joined, want) {
		t.Errorf("code row = %q, want %q", joined, want)
	}
	a, b := selAll(lines)
	if got := aiSelectionText(lines, a, b); got != "x := 1 // سلام دنیا" {
		t.Errorf("copy: %q", got)
	}
	// A part of the row copies the logical text too.
	r, l := rowOf(t, lines, "x := 1")
	if got := aiSelectionText(lines, aiSelPos{r, l.sel.x}, aiSelPos{r, l.sel.x + 6}); got != "x := 1" {
		t.Errorf("part: %q", got)
	}
}

// Spec: a Persian user message shows shaped, right aligned in its bubble,
// and copies as typed.
func TestAIRTLUserBubble(t *testing.T) {
	withRTL(t, rtl.Termocode)
	const w = 40
	lines := aiUserBubble("سلام، حالت چطوره؟", w, 0)
	rows := plainRows(lines, w)
	if !strings.Contains(rows[0], rtl.Visual("سلام، حالت چطوره؟", rtl.RTL)) {
		t.Errorf("bubble: %q", rows[0])
	}
	for i, l := range lines {
		if got := ansi.StringWidth(renderAILine(l, w)); got != w {
			t.Errorf("row %d width %d", i, got)
		}
	}
	a, b := selAll(lines)
	if got := aiSelectionText(lines, a, b); got != "سلام، حالت چطوره؟" {
		t.Errorf("copy: %q", got)
	}
}

// Spec: the chat input shows the current Persian line shaped and in
// reading order, rows stay exactly the panel width, and the caret is
// still drawn.
func TestAIRTLInputBox(t *testing.T) {
	withRTL(t, rtl.Termocode)
	m := aiTestModel(t)
	s := m.ai
	s.input = []rune("سلام دنیا")
	s.caret = len(s.input)
	s.focused = true
	const w = 40
	rows := m.renderAIInput(w, 2)
	joined := ansi.Strip(strings.Join(rows, "\n"))
	if !strings.Contains(joined, rtl.Visual("سلام دنیا", rtl.RTL)) {
		t.Errorf("input not shaped: %q", joined)
	}
	for i, r := range rows {
		if lipgloss.Width(r) != w {
			t.Errorf("row %d width %d", i, lipgloss.Width(r))
		}
	}
}

// Spec: the full panel with Persian messages keeps every row exactly the
// panel width.
func TestAIRTLPanelExactWidth(t *testing.T) {
	withRTL(t, rtl.Termocode)
	m := aiTestModel(t)
	m.actionsOpen, m.actionsPinned = true, true
	s := m.ai
	s.provider = fakeAIProvider{}
	s.conv.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "لطفا این تابع را سریع‌تر کن، متن طولانی است تا بشکند"},
		{Role: ai.StoredAssistant, Text: "## پاسخ\n\nحتما. این کد `for` را عوض کن:\n\n- مورد اول با English words\n- مورد دوم\n\n> نقل قول فارسی\n\n```go\nx := 1\n```"},
		{Role: ai.StoredError, Text: "خطا: اتصال قطع شد"},
	}
	for _, w := range []int{24, 32, 50} {
		rows := strings.Split(m.renderActionsPanel(w, m.h), "\n")
		for i, r := range rows {
			if lipgloss.Width(r) != w {
				t.Fatalf("w=%d row %d width %d: %q", w, i, lipgloss.Width(r), r)
			}
		}
	}
}

// Spec: with rtl = auto termocode asks the terminal for BDSM explicit
// mode while it runs (and back to implicit on exit); with rtl = off, or
// on Konsole, it sends nothing.
func TestRTLTerminalModes(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("KONSOLE_VERSION", "")
	t.Setenv("MLTERM", "")
	if in, out := RTLTerminalModes(); in != "\x1b[8l" || out != "\x1b[8h" {
		t.Errorf("auto: %q %q", in, out)
	}
	t.Setenv("KONSOLE_VERSION", "230805")
	if in, out := RTLTerminalModes(); in != "" || out != "" {
		t.Errorf("Konsole: %q %q", in, out)
	}
	t.Setenv("KONSOLE_VERSION", "")
	if err := saveSettings(settingsConfig{RTL: "off"}); err != nil {
		t.Fatal(err)
	}
	if in, out := RTLTerminalModes(); in != "" || out != "" {
		t.Errorf("off: %q %q", in, out)
	}
	if got := ConfigDisplay().RTL; got != "off" {
		t.Errorf("ConfigDisplay().RTL = %q", got)
	}
}

// Spec: a Persian list item has its marker on the right, the text right
// aligned next to it; a copy gives the item as written.
func TestAIRTLListMarkerRight(t *testing.T) {
	withRTL(t, rtl.Termocode)
	const w = 40
	lines := renderChatMarkdown("- مورد اول\n- مورد دوم", w, aiIndent, 0, false)
	rows := plainRows(lines, w)
	bullet, _, _ := listItem("- x")
	if want := rtl.Visual("مورد اول", rtl.RTL) + " " + bullet; !strings.HasSuffix(strings.TrimRight(rows[0], " "), want) {
		t.Errorf("row 0 = %q, want it to end with %q", rows[0], want)
	}
	for i, l := range lines {
		if got := ansi.StringWidth(renderAILine(l, w)); got != w {
			t.Errorf("row %d width %d", i, got)
		}
	}
	a, b := selAll(lines)
	if got := aiSelectionText(lines, a, b); got != bullet+" مورد اول\n"+bullet+" مورد دوم" {
		t.Errorf("copy: %q", got)
	}
}
