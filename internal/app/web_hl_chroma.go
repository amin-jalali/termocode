package app

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Syntax colours without a tree-sitter parser: many nvim installs ship
// parsers for a few languages only (Lua, Vim, Markdown, C), so a Go or
// Python file would be plain on the page. When nvim says "none", the hub
// colours the rows itself with the lexers the TUI uses for diffs and
// Markdown (chroma), from its own mirror of the buffer, and names each
// run like a tree-sitter capture, so the page paints both the same way.

// webHlCtxRows is how far above the asked rows the lexer starts on a big
// file: enough to know a block comment or a long string is open.
const webHlCtxRows = 2000

// webHlMaxBytes caps the text one fallback answer lexes.
const webHlMaxBytes = 2 << 20

// webHlControl are keywords drawn in the control colour (the TUI's
// @keyword.conditional / repeat / return / exception / import).
var webHlControl = map[string]bool{
	"if": true, "else": true, "elif": true, "for": true, "while": true, "do": true, "loop": true,
	"switch": true, "case": true, "default": true, "match": true, "select": true, "break": true,
	"continue": true, "goto": true, "fallthrough": true, "return": true, "yield": true,
	"try": true, "catch": true, "finally": true, "throw": true, "raise": true, "except": true,
	"defer": true, "go": true, "await": true, "import": true, "from": true, "export": true,
	"package": true, "use": true, "require": true, "include": true, "then": true, "fi": true,
	"done": true, "esac": true, "elseif": true, "until": true, "in": true,
}

// webHlLexer is chroma's lexer for a file: by its name first, then by
// nvim's filetype. nil: no lexer (the file stays plain).
func webHlLexer(path, ft string) chroma.Lexer {
	lx := lexers.Match(filepath.Base(path))
	if lx == nil && ft != "" {
		lx = lexers.Get(ft)
	}
	if lx == nil || lx == lexers.Fallback || lx.Config().Name == "plaintext" {
		return nil
	}
	return lx
}

// webHlGroupOf names a chroma token like a tree-sitter capture ("" plain).
func webHlGroupOf(tt chroma.TokenType, v string) string {
	switch {
	case tt == chroma.CommentPreproc || tt == chroma.CommentPreprocFile:
		return "keyword.directive"
	case tt.InCategory(chroma.Comment):
		return "comment"
	case tt == chroma.KeywordType:
		return "type.builtin"
	case tt == chroma.KeywordConstant:
		return "constant.builtin"
	case tt == chroma.KeywordNamespace:
		return "keyword.import"
	case tt.InCategory(chroma.Keyword):
		if webHlControl[strings.TrimSpace(v)] {
			return "keyword.conditional"
		}
		return "keyword"
	case tt == chroma.LiteralStringEscape || tt == chroma.LiteralStringInterpol:
		return "string.escape"
	case tt == chroma.LiteralStringRegex:
		return "string.regexp"
	case tt == chroma.LiteralStringSymbol:
		return "string.special"
	case tt.InSubCategory(chroma.LiteralString):
		return "string"
	case tt.InSubCategory(chroma.LiteralNumber):
		return "number"
	case tt == chroma.NameFunction || tt == chroma.NameFunctionMagic:
		return "function"
	case tt == chroma.NameBuiltin:
		return "function.builtin"
	case tt == chroma.NameClass || tt == chroma.NameException:
		return "type"
	case tt == chroma.NameNamespace:
		return "module"
	case tt == chroma.NameBuiltinPseudo:
		return "variable.builtin"
	case tt == chroma.NameConstant || tt == chroma.NameEntity:
		return "constant"
	case tt == chroma.NameDecorator:
		return "attribute"
	case tt == chroma.NameTag:
		return "tag"
	case tt == chroma.NameAttribute:
		return "tag.attribute"
	case tt == chroma.NameLabel:
		return "label"
	case tt == chroma.NameProperty:
		return "property"
	case tt.InCategory(chroma.NameVariable):
		return "variable"
	case tt == chroma.OperatorWord:
		return "keyword.operator"
	case tt.InCategory(chroma.Operator):
		return "operator"
	case tt == chroma.Punctuation:
		return "punctuation"
	case tt == chroma.GenericHeading || tt == chroma.GenericSubheading:
		return "markup.heading"
	case tt == chroma.GenericStrong:
		return "markup.strong"
	case tt == chroma.GenericEmph:
		return "markup.italic"
	case tt == chroma.GenericInserted:
		return "diff.plus"
	case tt == chroma.GenericDeleted:
		return "diff.minus"
	case tt == chroma.GenericError || tt == chroma.Error:
		return ""
	}
	return ""
}

// webHlChroma colours rows [first, last) of rows (the buffer at tick)
// with chroma. ok is false when no lexer knows the file: the answer is
// then "none" as before.
func webHlChroma(path, ft string, rows []string, tick, first, last, maxSpans int) (res webproto.HlRes, ok bool) {
	lx := webHlLexer(path, ft)
	if lx == nil {
		return webproto.HlRes{}, false
	}
	last = min(last, len(rows))
	first = min(first, last)
	res = webproto.HlRes{Tick: tick, First: first, Last: last, Groups: []string{}, Spans: [][]int{}}
	if first == last {
		return res, true
	}
	// Start at the top of the file (or webHlCtxRows above), so a comment
	// or string opened above the window is known; cap the bytes.
	start := max(0, first-webHlCtxRows)
	size := 0
	for i := last - 1; i >= start; i-- {
		size += len(rows[i]) + 1
		if size > webHlMaxBytes {
			start = min(i+1, first)
			break
		}
	}
	it, err := chroma.Coalesce(lx).Tokenise(nil, strings.Join(rows[start:last], "\n")+"\n")
	if err != nil {
		return res, true
	}
	gi := map[string]int{}
	row, col := start, 0 // the row and the UTF-16 column in it
	add := func(text, g string) {
		if g == "" || row < first || row >= last || text == "" {
			col += webU16Len(text)
			return
		}
		c0 := col
		col += webU16Len(text)
		k, seen := gi[g]
		if !seen {
			k = len(res.Groups)
			gi[g] = k
			res.Groups = append(res.Groups, g)
		}
		if n := len(res.Spans); n > 0 {
			if p := res.Spans[n-1]; p[0] == row && p[2] == c0 && p[3] == k {
				p[2] = col
				return
			}
		}
		if len(res.Spans) >= maxSpans {
			res.Cut = true
			return
		}
		res.Spans = append(res.Spans, []int{row, c0, col, k})
	}
	for tok := it(); tok != chroma.EOF && row < last; tok = it() {
		g := webHlGroupOf(tok.Type, tok.Value)
		v := tok.Value
		for v != "" && row < last {
			i := strings.IndexByte(v, '\n')
			if i < 0 {
				add(v, g)
				break
			}
			add(v[:i], g)
			row, col = row+1, 0
			v = v[i+1:]
		}
	}
	// chroma may drop or add text: a span past its row's end is cut out.
	out := res.Spans[:0]
	for _, s := range res.Spans {
		if n := webU16Len(rows[s[0]]); s[2] <= n {
			out = append(out, s)
		}
	}
	res.Spans = out
	return res, true
}

// webU16Len is the length of s in UTF-16 code units (what the page's
// strings count).
func webU16Len(s string) int {
	n := 0
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		s = s[size:]
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}
