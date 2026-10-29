package mdview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// FuzzRender draws any markdown at any width. Every row is exactly the
// view width, and heading and link marks point at rows that exist.
func FuzzRender(f *testing.F) {
	for _, s := range []string{
		"# Title\n\nSome *em* and **strong** and `code`.\n\n- a\n- b\n  1. c\n\n> quote\n\n```go\nfunc x() {}\n```\n",
		"| a | b |\n|---|:-:|\n| 1 | 2 |\n| long cell text here | x |\n",
		"[link](guides/git.md#x) ![img](a.png) <https://x.y>\n\n---\n\n- [ ] task\n- [x] done\n",
		"## سلام دنیا\n\nمتن فارسی با `code` و English.\n",
		"漢字の段落です。とても長い行がここにあります。\n\n😀 emoji ‍👩‍👩‍👧 zwj",
		"\x1b[31mraw escape\x1b[0m\n\t\ttabs\n",
		"<div>html</div>\n\n[^1]: note\n",
	} {
		f.Add(s, uint8(80), true)
		f.Add(s, uint8(12), false)
	}
	f.Fuzz(func(t *testing.T, src string, w uint8, rtl bool) {
		width := int(w%160) + 1
		rows, marks := RenderDoc(src, Options{Width: width, RTL: rtl, Links: true})
		for i, r := range rows {
			if got := ansi.StringWidth(r); got != width {
				t.Fatalf("row %d is %d cells, want %d: %q", i, got, width, r)
			}
			if strings.ContainsAny(ansi.Strip(r), "\n\r") {
				t.Fatalf("row %d holds a line break: %q", i, r)
			}
		}
		for _, h := range marks.Headings {
			if h.Row < 0 || h.Row >= len(rows) {
				t.Fatalf("heading %+v outside %d rows", h, len(rows))
			}
		}
		for _, l := range marks.Links {
			if l.Row < 0 || l.Row >= len(rows) || l.X0 > l.X1 || l.X1 > width {
				t.Fatalf("link %+v outside the view (%d rows, %d wide)", l, len(rows), width)
			}
		}
	})
}
