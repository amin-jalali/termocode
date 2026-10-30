package webhl

import (
	"strings"
	"unicode/utf16"

	"github.com/alecthomas/chroma/v2"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The outline without a language server: a guess from declarations. The
// name that follows a declaring keyword (func, def, class, …) on the same
// row is a symbol; an indented one nests one level (a method of a class).
// Go's receiver "(b *Box)" is skipped, and its function is a method.

// LSP SymbolKind numbers the outline uses.
const (
	kindModule    = 2
	kindClass     = 5
	kindMethod    = 6
	kindEnum      = 10
	kindInterface = 11
	kindFunction  = 12
	kindStruct    = 23
)

// symDecl maps a declaring keyword to the kind of what it names.
var symDecl = map[string]int{
	"func": kindFunction, "function": kindFunction, "def": kindFunction, "fn": kindFunction, "fun": kindFunction,
	"sub": kindFunction, "proc": kindFunction, "procedure": kindFunction, "method": kindMethod,
	"class": kindClass, "type": kindClass, "record": kindClass, "object": kindClass, "impl": kindClass,
	"struct": kindStruct, "interface": kindInterface, "trait": kindInterface, "protocol": kindInterface,
	"enum": kindEnum, "module": kindModule, "namespace": kindModule,
}

// Symbols guesses the symbols of rows (at most max; cut: there were
// more). ok is false when no lexer knows the file.
func Symbols(path, ft string, rows []string, max int) (syms []webproto.BufSymbol, cut, ok bool) {
	lx := webHlLexer(path, ft)
	if lx == nil {
		return nil, false, false
	}
	var b strings.Builder
	for _, r := range rows {
		if b.Len()+len(r) > webHlMaxBytes {
			break
		}
		b.WriteString(r)
		b.WriteByte('\n')
	}
	it, err := lx.Tokenise(nil, b.String())
	if err != nil {
		return nil, false, true
	}
	syms = []webproto.BufSymbol{}
	for i, line := range chroma.SplitTokensIntoLines(it.Tokens()) {
		s, found := symOfRow(line)
		if !found {
			continue
		}
		if len(syms) >= max {
			return syms, true, true
		}
		s.Line = i + 1
		syms = append(syms, s)
	}
	return syms, false, true
}

// symOfRow finds the one declaration of a row.
func symOfRow(line []chroma.Token) (webproto.BufSymbol, bool) {
	col, kind, paren, recv := 0, 0, 0, false
	indent := true
	depth := 0
	for _, tk := range line {
		v := tk.Value
		w := len(utf16.Encode([]rune(v)))
		blank := strings.TrimSpace(v) == ""
		switch {
		case blank:
			if indent && col == 0 && v != "\n" {
				depth = 1
			}
		case kind == 0:
			indent = false
			k, decl := symDecl[v]
			if !decl || !tk.Type.InCategory(chroma.Keyword) {
				if !tk.Type.InCategory(chroma.Keyword) {
					return webproto.BufSymbol{}, false // the row does something else first
				}
				break
			}
			kind = k
		case tk.Type.InCategory(chroma.Punctuation) || tk.Type.InCategory(chroma.Operator):
			if kind == kindFunction && strings.Contains(v, "(") && paren == 0 {
				paren++
				recv = true
			} else if paren > 0 && strings.Contains(v, ")") {
				paren--
			} else if paren == 0 {
				return webproto.BufSymbol{}, false
			}
		case paren > 0:
		case tk.Type.InCategory(chroma.Name):
			if recv || (kind == kindFunction && depth > 0) {
				kind = kindMethod
			}
			if recv {
				depth = 0
			}
			return webproto.BufSymbol{Name: v, Kind: kind, Col: col + 1, Depth: depth}, true
		case tk.Type.InCategory(chroma.Keyword):
			if k, decl := symDecl[v]; decl {
				kind = k
			}
		default:
			return webproto.BufSymbol{}, false
		}
		col += w
	}
	return webproto.BufSymbol{}, false
}
