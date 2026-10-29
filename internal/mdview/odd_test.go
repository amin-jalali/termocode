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
