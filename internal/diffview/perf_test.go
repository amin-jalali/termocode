package diffview

import (
	"strings"
	"testing"
)

// bigGo is a generated Go file of n lines; every 7th line differs
// between the old (v=0) and new (v=1) side.
func bigGo(n, v int) string {
	var b strings.Builder
	b.WriteString("package big\n\n")
	for i := 2; i < n; i++ {
		switch {
		case i%7 == 0:
			b.WriteString("\tx := compute(" + itoa(i+v) + ", \"s" + itoa(v) + "\") // note\n")
		case i%5 == 0:
			b.WriteString("func f" + itoa(i) + "(a int) int {\n")
		default:
			b.WriteString("\treturn a + " + itoa(i) + "\n")
		}
	}
	return b.String()
}

func bigCard(syntax bool) Model {
	old, nw := bigGo(5000, 0), bigGo(5000, 1)
	m := NewCommit(commitHeader(), []File{{Name: "big.go", Status: 'M', Old: old, New: nw}}, commitButtons())
	m.SetSize(150, 42)
	if syntax {
		m.SetFileSyntax(0, Tokenize("big.go", old), Tokenize("big.go", nw))
	}
	return m
}

// Budget (design §4.6): tokens for both sides of a 5 000-line file.
func BenchmarkTokenize5000Lines(b *testing.B) {
	old, nw := bigGo(5000, 0), bigGo(5000, 1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Tokenize("big.go", old)
		Tokenize("big.go", nw)
	}
}

// Budget (design §4.6): first diff of a 5 000-line file ≤ 100 ms (the
// diff and the first frame; the tokens come later, off the loop).
func BenchmarkFirstDiff5000(b *testing.B) {
	old, nw := bigGo(5000, 0), bigGo(5000, 1)
	m := NewCommit(commitHeader(), []File{{Name: "big.go", Status: 'M', Pending: true}}, commitButtons())
	m.SetSize(150, 42)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.SetFileText(0, old, nw)
		_ = m.Box()
	}
}

// Budget (design §4.6): card render ≤ 5 ms per frame at 150x42.
func BenchmarkCardRender150x42(b *testing.B) {
	for _, c := range []struct {
		name   string
		syntax bool
	}{{"plain", false}, {"syntax", true}} {
		b.Run(c.name, func(b *testing.B) {
			m := bigCard(c.syntax)
			m.Scroll(300)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = m.Box()
			}
		})
	}
}
