package app

import (
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func goldenLines(t testing.TB) []string {
	files, _ := filepath.Glob("../devtools/shotkit/testdata/golden/*.ans")
	if len(files) == 0 {
		t.Fatal("no golden frames")
	}
	var out []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, strings.Split(string(b), "\n")...)
	}
	return out
}

var widthSamples = []string{
	"", "abc", "\x1b[1;38;2;1;2;3mx\x1b[0m", "é", "é", "👍", "👍🏽", "👨‍👩‍👧", "🇩🇪", "☀", "☀️",
	"中文", "한국어", "가", "a\tb", "\x1b]8;;http://x\x07link\x1b]8;;\x07", "\x1b_Gi=1;OK\x1b\\",
	"\x1b[", "\x1bP1;2q#0\x1b\\", "\u0085x", "\x7f", " ▎▕─│╭", " main", string([]byte{0xff, 'a'}),
}

// TestCellWidthMatchesAnsi: cellWidth is a fast path, never a different
// answer: it must equal ansi.StringWidth on every row of every golden
// screen, on tricky samples and on random mixes of them.
func TestCellWidthMatchesAnsi(t *testing.T) {
	check := func(s string) {
		if got, want := cellWidth(s), ansi.StringWidth(s); got != want {
			t.Errorf("cellWidth(%q) = %d, ansi.StringWidth = %d", s, got, want)
		}
	}
	for _, l := range goldenLines(t) {
		check(l)
	}
	for _, s := range widthSamples {
		check(s)
	}
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 5000; i++ {
		var b strings.Builder
		for k := r.Intn(6); k >= 0; k-- {
			b.WriteString(widthSamples[r.Intn(len(widthSamples))])
		}
		check(b.String())
	}
}

// TestJoinsMatchLipgloss: joinColumns / joinRows give the same bytes as
// lipgloss.JoinHorizontal(Top) / JoinVertical(Left).
func TestJoinsMatchLipgloss(t *testing.T) {
	lines := goldenLines(t)
	r := rand.New(rand.NewSource(2))
	block := func() string {
		n := 1 + r.Intn(6)
		var rows []string
		for i := 0; i < n; i++ {
			l := lines[r.Intn(len(lines))]
			if cut := r.Intn(3); cut == 0 {
				l = ansi.Truncate(l, r.Intn(40), "")
			}
			rows = append(rows, l)
		}
		return strings.Join(rows, "\n")
	}
	for i := 0; i < 300; i++ {
		cols := []string{block(), block(), block()}[:1+r.Intn(3)]
		if got, want := joinColumns(cols...), lipgloss.JoinHorizontal(lipgloss.Top, cols...); got != want {
			t.Fatalf("joinColumns differs from lipgloss for %q", cols)
		}
		if got, want := joinRows(cols...), lipgloss.JoinVertical(lipgloss.Left, cols...); got != want {
			t.Fatalf("joinRows differs from lipgloss for %q", cols)
		}
	}
}

// TestTextWidthMatchesLipgloss: textWidth equals lipgloss.Width.
func TestTextWidthMatchesLipgloss(t *testing.T) {
	lines := goldenLines(t)
	for i := 0; i+3 < len(lines); i += 3 {
		s := strings.Join(lines[i:i+3], "\n")
		if got, want := textWidth(s), lipgloss.Width(s); got != want {
			t.Fatalf("textWidth = %d, lipgloss.Width = %d for %q", got, want, s)
		}
	}
	for _, s := range widthSamples {
		if got, want := textWidth(s+"\n"+s), lipgloss.Width(s+"\n"+s); got != want {
			t.Errorf("textWidth(%q) = %d, lipgloss.Width = %d", s, got, want)
		}
	}
}
