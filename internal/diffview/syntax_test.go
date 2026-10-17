package diffview

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/textdiff"
	"github.com/amin-jalali/termocode/internal/theme"
)

func trueColor(t *testing.T) {
	t.Helper()
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
}

// Spec (commit view §3.4): tokens are per line, cover every byte of the
// line and name the syntax class (keyword, comment, …); unknown files
// give no tokens (the card stays plain).
func TestTokenizeLines(t *testing.T) {
	src := "package main\n\n// hi\nfunc f() int { return 42 }\n"
	toks := Tokenize("a.go", src)
	lines := textdiff.SplitLines(src)
	if len(toks) != len(lines) {
		t.Fatalf("%d token lines for %d lines", len(toks), len(lines))
	}
	for i, l := range lines {
		n := 0
		for _, tk := range toks[i] {
			n += tk.N
		}
		if n != len(l) {
			t.Errorf("line %d: tokens cover %d bytes of %d", i, n, len(l))
		}
	}
	if len(toks[0]) == 0 || toks[0][0].C != SynControl && toks[0][0].C != SynKeyword {
		t.Errorf("package is not a keyword: %+v", toks[0])
	}
	if len(toks[2]) == 0 || toks[2][0].C != SynComment {
		t.Errorf("comment line: %+v", toks[2])
	}
	if Tokenize("notes.unknownext", "a b c\n") != nil {
		t.Error("an unknown file type has tokens")
	}
	if Tokenize("a.go", "") != nil {
		t.Error("empty text has tokens")
	}
}

// Spec (commit view §3.4): syntax colours paint under the row wash, and
// a changed-word span keeps its bright highlight on top.
func TestSyntaxPaintKeepsWordSpans(t *testing.T) {
	trueColor(t)
	p := colors()
	row := textdiff.Row{Kind: textdiff.Added, Text: "x := 2", NewLine: 1, Paired: true,
		Spans: []textdiff.Span{{Text: "x := "}, {Text: "2", Changed: true}}}
	toks := Tokenize("a.go", "x := 2\n")
	out := renderCell(row, 2, 3, 40, 0, p, false, toks[0])
	op := lipgloss.NewStyle().Background(p.addBg).Foreground(p.syn[synAdd][SynOperator]).Render(":=")
	if !strings.Contains(out, op) {
		t.Errorf("operator not in its syntax colour on the add wash:\n%q\nwant %q", out, op)
	}
	hi := lipgloss.NewStyle().Background(p.addWord).Foreground(p.white).Bold(true).Render("2")
	if !strings.Contains(out, hi) {
		t.Errorf("changed span lost its highlight:\n%q\nwant %q", out, hi)
	}
	// Without tokens the cell is plain.
	plainOut := renderCell(row, 2, 3, 40, 0, p, false, nil)
	if strings.Contains(plainOut, op) {
		t.Error("tokens painted without tokens")
	}
	// A context row is coloured too (on the card background).
	ctx := textdiff.Row{Kind: textdiff.Context, Text: "x := 2", OldLine: 1, NewLine: 1}
	want := lipgloss.NewStyle().Background(p.bg).Foreground(p.syn[synCtx][SynOperator]).Render(":=")
	if out := renderCell(ctx, 2, 3, 40, 0, p, false, toks[0]); !strings.Contains(out, want) {
		t.Errorf("context row not coloured: %q", out)
	}
}

// Spec (commit view §3.4): the card paints plain first; once the tokens
// arrive the same file is coloured.
func TestSetFileSyntaxColoursLater(t *testing.T) {
	trueColor(t)
	old := "package main\n\nfunc a() {}\n"
	nw := "package main\n\nfunc b() {}\n"
	m := NewCommit(commitHeader(), []File{{Name: "a.go", Status: 'M', Old: old, New: nw}}, commitButtons())
	m.SetSize(150, 42)
	p := colors()
	fgSeq := func(c lipgloss.Color) string {
		r, _ := theme.ParseHex(string(c))
		return "38;2;" + itoa(int(r.R)) + ";" + itoa(int(r.G)) + ";" + itoa(int(r.B))
	}
	ctrl := fgSeq(p.syn[synCtx][SynControl])
	if strings.Contains(m.Box(), ctrl) && ctrl != fgSeq(p.text) && ctrl != fgSeq(p.muted) {
		t.Fatal("plain card already uses the keyword colour")
	}
	m.SetFileSyntax(0, Tokenize("a.go", old), Tokenize("a.go", nw))
	if !strings.Contains(m.Box(), ctrl) {
		t.Error("tokens did not colour the card")
	}
	// A stale index is ignored.
	m.SetFileSyntax(5, nil, nil)
}

// Spec (commit view §3.4): every syntax colour stays readable (≥ 3:1) on
// the card, on the added wash and on the removed wash, in every theme.
func TestSyntaxColoursContrast(t *testing.T) {
	defer theme.ApplyTheme("vscode-dark-plus")
	for _, nt := range theme.AvailableThemes() {
		theme.ApplyTheme(nt.ID)
		p := colors()
		for side, bg := range map[int]lipgloss.Color{synCtx: p.bg, synAdd: p.addBg, synDel: p.delBg} {
			for c := SynClass(1); c < synClasses; c++ {
				if r := contrast(string(p.syn[side][c]), string(bg)); r < 3 {
					t.Errorf("%s: class %d on side %d: %.2f < 3", nt.ID, c, side, r)
				}
			}
		}
	}
}

func contrast(a, b string) float64 {
	x, _ := theme.ParseHex(a)
	y, _ := theme.ParseHex(b)
	lum := func(c theme.RGB) float64 {
		ch := func(v uint8) float64 {
			f := float64(v) / 255
			if f <= 0.03928 {
				return f / 12.92
			}
			return math.Pow((f+0.055)/1.055, 2.4)
		}
		return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
	}
	la, lb := lum(x), lum(y)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
