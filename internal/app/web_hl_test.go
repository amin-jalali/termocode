package app

import (
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/amin-jalali/termocode/internal/webproto"
)

func webHl(r *webFilesRig, buf, first, last int) map[string]any {
	r.t.Helper()
	return r.callOn(r.c, webproto.OpBufHl, &webproto.HlArgs{Buf: buf, First: first, Last: last})
}

// webHlGroup is the group of the span that starts at (row, c0), or "".
func webHlGroup(res webproto.HlRes, row, c0 int) string {
	g := ""
	for _, s := range res.Spans {
		if s[0] == row && s[1] == c0 {
			g = res.Groups[s[3]] // the last one wins, as on the page
		}
	}
	return g
}

func u16(s string) int { return len(utf16.Encode([]rune(s))) }

// Spec (WEB-38): buf.hl answers nvim's tree-sitter captures of the rows
// asked, with columns in UTF-16 units of each row (a Persian word and an
// emoji before a comment move it by their UTF-16 length, not bytes), the
// tick they belong to, and only real highlight groups (no @spell).
func TestHlSpansFromTreeSitter(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	line1 := `local s = "سلام 😀" -- note`
	r.write("a.lua", "local x = 1\n"+line1+"\n")
	b := webBufOpen(t, r, r.c, "a.lua")
	var res webproto.HlRes
	webResData(t, webHl(r, b.Buf, 0, 99), &res)
	if res.Tick != b.Tick || res.First != 0 || res.Last != 2 || res.None {
		t.Fatalf("hl head %+v", res)
	}
	if g := webHlGroup(res, 0, 0); g != "keyword" {
		t.Errorf("`local` is %q, want keyword", g)
	}
	at := u16(line1[:strings.Index(line1, "--")])
	if g := webHlGroup(res, 1, at); g != "comment" {
		t.Errorf("the comment at UTF-16 column %d is %q (spans %v %v)", at, g, res.Spans, res.Groups)
	}
	for _, s := range res.Spans {
		if s[1] >= s[2] || s[0] < 0 || s[0] > 1 {
			t.Errorf("bad span %v", s)
		}
		if g := res.Groups[s[3]]; g == "spell" || strings.HasPrefix(g, "_") {
			t.Errorf("group %q went out", g)
		}
	}
	// Only the rows asked.
	webResData(t, webHl(r, b.Buf, 1, 2), &res)
	for _, s := range res.Spans {
		if s[0] != 1 {
			t.Errorf("row %d outside [1,2)", s[0])
		}
	}
}

// Spec (WEB-38): a file with no parser is plain (none); a range past the
// end is cut to the buffer; a buffer the page did not open is not_found;
// the span cap cuts and says so.
func TestHlPlainRangeAndCap(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	r.write("n.txt", "just words\n")
	b := webBufOpen(t, r, r.c, "n.txt")
	var res webproto.HlRes
	webResData(t, webHl(r, b.Buf, 0, 50), &res)
	if !res.None || len(res.Spans) != 0 || res.Last != 1 {
		t.Errorf("plain text: %+v", res)
	}
	if f := webHl(r, 999, 0, 1); webErrCode(f) != "not_found" {
		t.Errorf("unopened buffer: %v", f)
	}
	r.write("m.lua", strings.Repeat("local a = 1\n", 40))
	b = webBufOpen(t, r, r.c, "m.lua")
	old := webHlMaxSpans
	webHlMaxSpans = 10
	t.Cleanup(func() { webHlMaxSpans = old })
	webResData(t, webHl(r, b.Buf, 0, 40), &res)
	if !res.Cut || len(res.Spans) != 10 {
		t.Errorf("cap: cut %v, %d spans", res.Cut, len(res.Spans))
	}
}

// Spec (WEB-38): fixtures per language. Lua always has a parser in nvim;
// Markdown, Go, TypeScript and Python run where their parser is
// installed (skipped otherwise: the page then shows plain text).
func TestHlFixtures(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	for _, c := range []struct{ file, body, lang, want string }{
		{"f.lua", "return function() end\n", "lua", "keyword"},
		{"f.md", "# Title\n\ntext\n", "markdown", "markup.heading"},
		{"f.go", "package main\n", "go", "keyword"},
		{"f.ts", "const a: number = 1\n", "typescript", "keyword"},
		{"f.py", "def f():\n    pass\n", "python", "keyword"},
	} {
		t.Run(c.lang, func(t *testing.T) {
			var ok bool
			r.lua(`return (pcall(vim.treesitter.language.add, ...)) and vim.treesitter.language.add(...) or false`, &ok, c.lang)
			if !ok {
				t.Skipf("no %s parser in this nvim", c.lang)
			}
			r.write(c.file, c.body)
			b := webBufOpen(t, r, r.c, c.file)
			var res webproto.HlRes
			webResData(t, webHl(r, b.Buf, 0, 10), &res)
			if g := webHlGroup(res, 0, 0); !strings.HasPrefix(g, c.want) {
				t.Errorf("row 0 col 0 is %q, want %s… (%v %v)", g, c.want, res.Spans, res.Groups)
			}
		})
	}
}

// Spec (WEB-38): what the chunk answers is checked before it goes out:
// odd group names and spans outside the range or pointing nowhere drop.
func TestHlCleanDropsOddSpans(t *testing.T) {
	got := webHlClean(webLuaHlRes{Tick: 3, First: 2, Last: 4, Groups: []string{"keyword", "<b>", "string"},
		Spans: [][]int{{2, 0, 5, 0}, {3, 1, 2, 1}, {9, 0, 1, 0}, {2, 4, 4, 2}, {3, 0, 3, 7}, {3, 0, 3}, {3, 6, 9, 2}}})
	want := "keyword,string|2:0-5:0 3:6-9:1"
	var b strings.Builder
	for _, s := range got.Spans {
		b.WriteString(" " + itoa(s[0]) + ":" + itoa(s[1]) + "-" + itoa(s[2]) + ":" + itoa(s[3]))
	}
	if s := strings.Join(got.Groups, ",") + "|" + strings.TrimSpace(b.String()); s != want {
		t.Errorf("clean: %s, want %s", s, want)
	}
}
