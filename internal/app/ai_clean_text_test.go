package app

import (
	"strings"
	"testing"
)

// Spec: text from the model (or from a tool result the model quotes) can
// carry terminal escapes and bidi overrides. They must never reach the
// screen: an escape can clear the screen or set the clipboard (OSC 52), and
// a bidi override can hide what a file name really is ("txt.exe").
func TestAIAnswerDropsEscapesAndBidiOverrides(t *testing.T) {
	const hostile = "hello \x1b[2J\x1b]52;c;ZXZpbA==\x07world ‮exe.txt‬ and `code\x1b[31m` x⁦y⁩\r\b"
	for _, w := range []int{40, 80} {
		lines := renderChatMarkdown(aiCleanText(hostile), w, aiIndent, 0, false)
		var b strings.Builder
		for _, l := range lines {
			b.WriteString(renderAILine(l, w))
			b.WriteByte('\n')
		}
		out := b.String()
		for _, bad := range []string{"\x1b[2J", "\x1b]52", "\x07", "‮", "‬", "⁦", "⁩", "\x1b[31m`", "\r", "\b"} {
			if strings.Contains(out, bad) {
				t.Errorf("w=%d: %q reached the screen", w, bad)
			}
		}
		if !strings.Contains(stripSGRForTest(out), "hello") || !strings.Contains(stripSGRForTest(out), "world") {
			t.Errorf("w=%d: the normal text is gone: %q", w, out)
		}
	}
}

func TestAICleanText(t *testing.T) {
	cases := map[string]string{
		"plain":                  "plain",
		"a\nb\tc":                "a\nb    c",
		"a\x1b[2Jb":              "ab",
		"a\x1b]8;;http://x\x07b": "ab",
		"a\x1b]52;c;Zg==\x1b\\b": "ab",
		"x‮y‬":                   "xy",
		"x⁦y⁩":                   "xy",
		"‏سلام‎":                 "‏سلام‎", // marks stay: they do not reorder
		"a\r\nb":                 "a\nb",
		"a\x00b\x7fc\u009bd":     "abcd",
		"سلام دنیا":              "سلام دنیا",
	}
	for in, want := range cases {
		if got := aiCleanText(in); got != want {
			t.Errorf("aiCleanText(%q) = %q, want %q", in, got, want)
		}
	}
}

// stripSGRForTest removes the colour codes the renderer itself adds.
func stripSGRForTest(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Spec: the Output panel (AI audit log, task output) and extension panels
// show outside text too; they drop the same escapes, C1 controls and bidi
// overrides.
func TestOutputAndExtTextDropUnsafe(t *testing.T) {
	const in = "a\x1b[2Jb\u009bc‮d⁦e‏f"
	if got, want := sanitizeOutputLine(in), "abcde‏f"; got != want {
		t.Errorf("sanitizeOutputLine = %q, want %q", got, want)
	}
	if got, want := sanitizeExtText(in+"\n"), "abcde‏f\n"; got != want {
		t.Errorf("sanitizeExtText = %q, want %q", got, want)
	}
}
