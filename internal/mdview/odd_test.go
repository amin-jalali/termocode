package mdview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

func joined(rows []string) string { return strings.Join(plain(rows), "\n") }

func TestOddInlineAndBlockMarkdown(t *testing.T) {
	withMode(t, theme.IconModeUnicode)
	src := "line one  \nline two\n\n" + // hard line break
		"<https://example.com/x> and <b>raw</b>\n\n" +
		"![](pic.png)\n\n" +
		"<!-- a comment\nover lines -->\n\n" +
		"- \n- item\n\n" +
		"<div>\nhtml block\n</div>\n"
	rows, marks := RenderDoc(src, Options{Width: 50, Links: true})
	text := joined(rows)
	for _, want := range []string{"line one", "line two", "https://example.com/x", "<b>raw</b>", "[image: pic.png]", "a comment", "over lines -->", "html block"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q missing in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "line one line two") {
		t.Error("hard line break joined the lines")
	}
	if len(marks.Links) != 1 || marks.Links[0].Dest != "https://example.com/x" || marks.Links[0].ID != 1 {
		t.Fatalf("autolink not recorded: %+v", marks.Links)
	}
	for i, r := range rows {
		if w := ansi.StringWidth(r); w != 50 {
			t.Fatalf("row %d width %d", i, w)
		}
	}
}

func TestImageHookDecliningShowsPlaceholder(t *testing.T) {
	withMode(t, theme.IconModeUnicode)
	rows := Render("![chart](c.png) ![two](d.png)\n", Options{Width: 40, Image: func(string, string, int) []string { return nil }})
	text := joined(rows)
	if !strings.Contains(text, "[image: chart]") || !strings.Contains(text, "[image: two]") {
		t.Fatalf("placeholders missing:\n%s", text)
	}
}

func TestClickableDropsControlBytes(t *testing.T) {
	if clickable("https://x/\x1b]8;;evil\x07\u009b") != "https://x/]8;;evil" {
		t.Fatalf("got %q", clickable("https://x/\x1b]8;;evil\x07\u009b"))
	}
	if clickable("file:///etc/passwd") != "" || clickable("guides/a.md") != "" {
		t.Fatal("non-web links are not clickable")
	}
	if clickable("MAILTO:a@b.c") == "" {
		t.Fatal("mail links are clickable")
	}
}

// Raw control bytes in the text never reach the terminal: a lone ESC
// ate the cells after it, and a sequence could move the cursor.
func TestRenderControlBytesShowAsReplacement(t *testing.T) {
	rows := Render("a\x1b[2Jb\x07c\u009bd\x7fe", Options{Width: 30})
	all := strings.Join(rows, "\n")
	if strings.ContainsAny(all, "\x07\u009b\x7f") || strings.Contains(all, "\x1b[2J") {
		t.Fatalf("control bytes reached the output: %q", all)
	}
	if !strings.Contains(ansi.Strip(all), "a�[2Jb�c�d�e") {
		t.Fatalf("text lost: %q", ansi.Strip(all))
	}
}

// A lone CR is a line end, as in CommonMark, never a byte in a row.
func TestRenderLoneCRIsALineEnd(t *testing.T) {
	got := Render("a\rb\r\nc", Options{Width: 20})
	for _, r := range got {
		if strings.Contains(r, "\r") {
			t.Fatalf("row holds a CR: %q", r)
		}
	}
	if j := joined(got); !strings.Contains(j, "a b c") && !strings.Contains(j, "a\nb\nc") {
		t.Fatalf("text lost: %q", j)
	}
}

// A Prepend mark (U+0601) joins the next character into one cell
// group; drawn before the row padding it made the row one cell short.
func TestRenderPrependMarkKeepsRowWidth(t *testing.T) {
	for _, rtlOn := range []bool{false, true} {
		for _, r := range Render(" 0؁و", Options{Width: 81, RTL: rtlOn}) {
			if w := ansi.StringWidth(r); w != 81 {
				t.Fatalf("rtl=%v: row is %d cells, want 81: %q", rtlOn, w, r)
			}
		}
	}
}
