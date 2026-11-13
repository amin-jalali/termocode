package toast

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/charmbracelet/x/ansi"
)

// A path error whose reason alone fills the row shows only the reason,
// cut to the width.
func TestErrorDetailTextReasonOnlyWhenNoRoomForPath(t *testing.T) {
	got := errorDetailText("open /a/b.txt: a very long reason that fills the row", 12)
	if ansi.StringWidth(got) > 12 || !strings.HasPrefix(got, "a very long") || strings.Contains(got, "b.txt") {
		t.Fatalf("got %q", got)
	}
}

// When even the file name is too wide, the path keeps some root context
// and the end of the name, and always fits the width.
func TestMiddlePathLongFileNameKeepsBothEnds(t *testing.T) {
	got := middlePath("/root/dir/"+strings.Repeat("n", 40)+".txt", 20)
	if ansi.StringWidth(got) > 20 || !strings.Contains(got, "…") || !strings.HasPrefix(got, "/root") || !strings.HasSuffix(got, ".txt") {
		t.Fatalf("got %q (%d)", got, ansi.StringWidth(got))
	}
}

// TitleWidth is the widest of the toasts on screen plus the card chrome.
func TestTitleWidth(t *testing.T) {
	m := New()
	if m.TitleWidth() != 0 {
		t.Fatal("empty stack has no title width")
	}
	m, _ = m.Push(Info, "abc")
	m, _ = m.Push(Info, "abcdefgh")
	if got := m.TitleWidth(); got != 8+8 {
		t.Fatalf("TitleWidth = %d, want 16", got)
	}
}

// Each icon mode gives its own glyph for each severity.
func TestSeverityIconPerMode(t *testing.T) {
	old := theme.CurrentIconMode
	t.Cleanup(func() { theme.CurrentIconMode = old })
	want := map[theme.IconMode][3]string{
		theme.IconModeNerdFont: {"", "", ""},
		theme.IconModeUnicode:  {"●", "▲", "✕"},
		theme.IconModeASCII:    {"i", "!", "x"},
	}
	for mode, w := range want {
		theme.CurrentIconMode = mode
		for i, sev := range []Severity{Info, Warn, Errr} {
			if got, _ := severityIcon(sev); got != w[i] {
				t.Fatalf("mode %v severity %v: %q, want %q", mode, sev, got, w[i])
			}
		}
	}
}

// OnPush gets the title and the detail together.
func TestPushHookGetsTitleAndDetail(t *testing.T) {
	old := OnPush
	t.Cleanup(func() { OnPush = old })
	var got string
	OnPush = func(_ Severity, msg string) { got = msg }
	New().PushDetail(Warn, "Title", "more")
	if got != "Title — more" {
		t.Fatalf("hook got %q", got)
	}
}

// The compact card writes an error's detail as reason plus path, and a
// very narrow card still has room for the title.
func TestCompactErrorDetailAndNarrowWidth(t *testing.T) {
	m, _ := New().PushDetail(Errr, "Save", "open /tmp/some/dir/file.txt: permission denied")
	text := ansi.Strip(strings.Join(m.CompactCards(70, 1)[0].Lines, "\n"))
	if !strings.Contains(text, "permission denied · open") {
		t.Fatalf("compact detail: %s", text)
	}
	for _, line := range m.CompactCards(5, 1)[0].Lines {
		if ansi.StringWidth(line) != 16 {
			t.Fatalf("narrow card width %d", ansi.StringWidth(line))
		}
	}
}
