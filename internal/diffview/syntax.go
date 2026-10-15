package diffview

import (
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/textdiff"
	"github.com/amin-jalali/termocode/internal/theme"
)

// SynClass is a syntax colour class; 0 is plain text.
type SynClass uint8

// The syntax classes (theme Syntax* colours).
const (
	SynPlain SynClass = iota
	SynKeyword
	SynControl
	SynFunction
	SynVariable
	SynType
	SynString
	SynNumber
	SynComment
	SynConstant
	SynOperator
	synClasses
)

// The three backgrounds a token is painted on (pal.syn).
const (
	synCtx = iota // the card (context rows)
	synAdd        // the added wash
	synDel        // the removed wash
)

// Tok is one syntax run of a line: N bytes of class C.
type Tok struct {
	N int
	C SynClass
}

// Tokenize splits text into per-line syntax runs (lines as
// textdiff.SplitLines cuts them). It returns nil when chroma does not
// know the file type: the card then stays plain. It is slow next to a
// frame, so it runs off the UI loop.
func Tokenize(name, text string) [][]Tok {
	if text == "" {
		return nil
	}
	lx := lexers.Match(name)
	if lx == nil {
		return nil
	}
	lines := textdiff.SplitLines(text)
	it, err := chroma.Coalesce(lx).Tokenise(nil, text)
	if err != nil {
		return nil
	}
	out := make([][]Tok, len(lines))
	row := 0
	add := func(n int, c SynClass) {
		if n == 0 || row >= len(out) {
			return
		}
		l := out[row]
		if k := len(l); k > 0 && l[k-1].C == c {
			l[k-1].N += n
			return
		}
		out[row] = append(l, Tok{N: n, C: c})
	}
	for tok := it(); tok != chroma.EOF; tok = it() {
		c := synClass(tok.Type)
		v := tok.Value
		for {
			i := strings.IndexByte(v, '\n')
			if i < 0 {
				add(len(v), c)
				break
			}
			add(i, c)
			row++
			v = v[i+1:]
		}
	}
	// chroma may drop or add bytes (a final newline): trust the line
	// lengths, so a run never cuts the wrong text.
	for i, l := range lines {
		n := 0
		for _, t := range out[i] {
			n += t.N
		}
		if n != len(l) {
			out[i] = []Tok{{N: len(l)}}
		}
	}
	return out
}

func synClass(tt chroma.TokenType) SynClass {
	switch {
	case tt.InCategory(chroma.Comment):
		return SynComment
	case tt == chroma.KeywordType:
		return SynType
	case tt == chroma.KeywordNamespace || tt == chroma.KeywordDeclaration:
		return SynControl
	case tt.InCategory(chroma.Keyword):
		return SynKeyword
	case tt.InSubCategory(chroma.LiteralString):
		return SynString
	case tt.InSubCategory(chroma.LiteralNumber):
		return SynNumber
	case tt == chroma.NameFunction || tt == chroma.NameFunctionMagic:
		return SynFunction
	case tt == chroma.NameBuiltin || tt == chroma.NameClass || tt == chroma.NameNamespace:
		return SynType
	case tt == chroma.NameConstant || tt == chroma.NameBuiltinPseudo:
		return SynConstant
	case tt.InCategory(chroma.Operator) || tt == chroma.Punctuation:
		return SynOperator
	case tt == chroma.NameTag:
		return SynKeyword
	case tt.InCategory(chroma.Name):
		return SynVariable
	}
	return SynPlain
}

// HasLexer reports whether Tokenize knows the file type of name.
func HasLexer(name string) bool { return lexers.Match(name) != nil }

// HasSyntax reports whether file i has its syntax tokens.
func (m Model) HasSyntax(i int) bool {
	return i >= 0 && i < len(m.files) && (m.files[i].synOld != nil || m.files[i].synNew != nil)
}

// SetFileSyntax colours file i with tokens for its old and new text
// (from Tokenize). Until it is called the file is plain.
func (m *Model) SetFileSyntax(i int, old, new [][]Tok) {
	if i < 0 || i >= len(m.files) {
		return
	}
	m.files[i].synOld, m.files[i].synNew = old, new
}

// rowToks is the token line of diff row r on the given pane side
// (0 unified, 1 old, 2 new).
func rowToks(f *fileState, r textdiff.Row, side int) []Tok {
	line, src := r.NewLine, f.synNew
	if r.Kind == textdiff.Removed || side == 1 && r.Kind == textdiff.Context {
		line, src = r.OldLine, f.synOld
	}
	if line <= 0 || line > len(src) {
		return nil
	}
	return src[line-1]
}

// synPalette is the syntax colours on the three backgrounds, each moved
// toward the text colour until it reads at 3:1 (cached per palette).
type synPalette [3][synClasses]lipgloss.Color

var synCache struct {
	sync.Mutex
	key string
	p   synPalette
}

var synTheme = [synClasses]*theme.Color256{
	SynPlain: &theme.TextPrimary, SynKeyword: &theme.SyntaxKeyword, SynControl: &theme.SyntaxControl,
	SynFunction: &theme.SyntaxFunction, SynVariable: &theme.SyntaxVariable, SynType: &theme.SyntaxType,
	SynString: &theme.SyntaxString, SynNumber: &theme.SyntaxNumber, SynComment: &theme.SyntaxComment,
	SynConstant: &theme.SyntaxConstant, SynOperator: &theme.SyntaxOperator,
}

func synColours(bg, add, del lipgloss.Color) synPalette {
	key := string(bg) + string(add) + string(del)
	for c := SynClass(0); c < synClasses; c++ {
		key += theme.Hex(*synTheme[c])
	}
	synCache.Lock()
	defer synCache.Unlock()
	if synCache.key == key {
		return synCache.p
	}
	var p synPalette
	for side, b := range [3]lipgloss.Color{bg, add, del} {
		for c := SynClass(0); c < synClasses; c++ {
			p[side][c] = theme.ReadableOn(*synTheme[c], b, 3)
		}
	}
	synCache.key, synCache.p = key, p
	return p
}
