package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// webHlAt is the group of the span covering (row, col), or "".
func webHlAt(res webproto.HlRes, row, col int) string {
	g := ""
	for _, s := range res.Spans {
		if s[0] == row && s[1] <= col && col < s[2] {
			g = res.Groups[s[3]]
		}
	}
	return g
}

// Spec: without a tree-sitter parser the hub colours the rows with the
// TUI's lexers, named like captures, for Go, TS/JS, Python, Rust, JSON,
// YAML, Markdown and shell.
func TestHlChromaLanguages(t *testing.T) {
	for _, c := range []struct {
		file, body string
		row, col   int
		want       string
	}{
		{"a.go", "package main\n\nfunc f() { return }\n", 2, 0, "keyword"},
		{"a.go", "package main\n\nfunc f() { return }\n", 2, 11, "keyword.conditional"},
		{"a.go", "// note\n", 0, 3, "comment"},
		{"a.go", "var s = \"hi\"\n", 0, 9, "string"},
		{"a.ts", "const a: number = 1\n", 0, 0, "keyword"},
		{"a.js", "let s = 'x'\n", 0, 9, "string"},
		{"a.py", "def f():\n    pass\n", 0, 0, "keyword"},
		{"a.rs", "fn main() { let x = 1; }\n", 0, 0, "keyword"},
		{"a.json", "{\"a\": 12}\n", 0, 7, "number"},
		{"a.yaml", "key: value # c\n", 0, 12, "comment"},
		{"a.md", "# Title\n\ntext\n", 0, 2, "markup.heading"},
		{"a.sh", "if true; then echo hi; fi\n", 0, 0, "keyword.conditional"},
	} {
		t.Run(c.file+"/"+c.want, func(t *testing.T) {
			rows := strings.Split(strings.TrimSuffix(c.body, "\n"), "\n")
			res, ok := webHlChroma(c.file, "", rows, 7, 0, len(rows), webproto.MaxHlSpans)
			if !ok {
				t.Fatal("no lexer")
			}
			if res.Tick != 7 || res.First != 0 || res.Last != len(rows) || res.None {
				t.Fatalf("head %+v", res)
			}
			if g := webHlAt(res, c.row, c.col); !strings.HasPrefix(g, c.want) {
				t.Errorf("(%d,%d) is %q, want %s (%v %v)", c.row, c.col, g, c.want, res.Spans, res.Groups)
			}
		})
	}
}

// Spec: the fallback answers only the rows asked, in UTF-16 columns, knows
// a comment opened above the window, caps the spans, and says "no lexer"
// for a file type it does not know.
func TestHlChromaWindowColumnsCap(t *testing.T) {
	rows := []string{"/* open", "still comment */", `s := "سلام 😀" // note`}
	res, _ := webHlChroma("x.go", "", rows, 1, 1, 3, webproto.MaxHlSpans)
	for _, s := range res.Spans {
		if s[0] < 1 || s[0] > 2 {
			t.Errorf("row %d outside [1,3)", s[0])
		}
	}
	if g := webHlAt(res, 1, 0); g != "comment" {
		t.Errorf("row 1 is %q, want the comment opened above", g)
	}
	at := u16(rows[2][:strings.Index(rows[2], "//")])
	if g := webHlAt(res, 2, at); g != "comment" {
		t.Errorf("comment at UTF-16 col %d is %q (%v)", at, g, res.Spans)
	}
	many := strings.Split(strings.Repeat("a := 1\n", 30), "\n")
	res, _ = webHlChroma("x.go", "", many, 1, 0, len(many), 3)
	if !res.Cut || len(res.Spans) != 3 {
		t.Errorf("cap: cut %v, %d spans", res.Cut, len(res.Spans))
	}
	if _, ok := webHlChroma("notes.unknownext", "", []string{"x"}, 1, 0, 1, 10); ok {
		t.Error("a file type no lexer knows got colours")
	}
	if _, ok := webHlChroma("Makefile.x", "python", []string{"def f(): pass"}, 1, 0, 1, 10); !ok {
		t.Error("nvim's filetype is not used when the name says nothing")
	}
}

// Spec: when nvim has no parser for the file (an install with no Go
// parser), buf.hl answers the hub's own colours, not "none".
func TestHlFallsBackWhenNvimHasNoParser(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	old := webHlNoParser
	webHlNoParser = true // as an nvim with no parsers at all
	t.Cleanup(func() { webHlNoParser = old })
	r.write("a.go", "package main\n\nfunc main() {}\n")
	b := webBufOpen(t, r, r.c, "a.go")
	var res webproto.HlRes
	webResData(t, webHl(r, b.Buf, 0, 99), &res)
	if res.None || res.Tick != b.Tick || res.Last != 3 {
		t.Fatalf("head %+v", res)
	}
	if g := webHlGroup(res, 0, 0); g != "keyword.import" && g != "keyword" {
		t.Errorf("`package` is %q (%v %v)", g, res.Spans, res.Groups)
	}
	r.write("n.txt", "just words\n")
	b = webBufOpen(t, r, r.c, "n.txt")
	webResData(t, webHl(r, b.Buf, 0, 9), &res)
	if !res.None || len(res.Spans) != 0 {
		t.Errorf("plain text: %+v", res)
	}
}
