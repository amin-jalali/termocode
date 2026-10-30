package theme

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Fast gives lipgloss's bytes for every style and text it is used with,
// in every colour profile.
func TestFastSameAsLipgloss(t *testing.T) {
	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	styles := []lipgloss.Style{
		lipgloss.NewStyle(),
		lipgloss.NewStyle().Background(lipgloss.Color("#202020")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#c0ffee")).Background(lipgloss.Color("#202020")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#c0ffee")).Background(lipgloss.Color("#202020")).Bold(true),
		lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Italic(true).Underline(true),
		lipgloss.NewStyle().Background(lipgloss.Color("#202020")).Width(10),
		lipgloss.NewStyle().Background(lipgloss.Color("#202020")).Padding(0, 1),
		lipgloss.NewStyle().Strikethrough(true).Faint(true),
	}
	texts := []string{"", "x", " ", "a b", "  lead", "trail  ", "名前…", "⌕ search", "a\tb", "two\nlines", "\x1b[1mbold\x1b[0m", "▌", "\r"}
	r := rand.New(rand.NewSource(1))
	pieces := []string{"a", " ", "名", "…", "─", "\t", "\n", "é", "😀", "x"}
	for i := 0; i < 300; i++ {
		var b strings.Builder
		for n := r.Intn(6); n >= 0; n-- {
			b.WriteString(pieces[r.Intn(len(pieces))])
		}
		texts = append(texts, b.String())
	}
	for _, prof := range []termenv.Profile{termenv.TrueColor, termenv.ANSI256, termenv.ANSI, termenv.Ascii} {
		lipgloss.SetColorProfile(prof)
		for si, st := range styles {
			f := NewFast(st)
			for _, s := range texts {
				if got, want := f.Render(s), st.Render(s); got != want {
					t.Fatalf("profile %v style %d text %q: Fast %q, lipgloss %q", prof, si, s, got, want)
				}
			}
		}
	}
}

// BenchmarkFastRender is one short label in a bold coloured style.
func BenchmarkFastRender(b *testing.B) {
	prev := lipgloss.ColorProfile()
	b.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	lipgloss.SetColorProfile(termenv.TrueColor)
	st := lipgloss.NewStyle().Foreground(lipgloss.Color("#c0ffee")).Background(lipgloss.Color("#202020")).Bold(true)
	f := NewFast(st)
	for i := 0; i < b.N; i++ {
		_ = f.Render("Git: Show Log")
	}
}

// BenchmarkLipglossRender is the same through lipgloss.
func BenchmarkLipglossRender(b *testing.B) {
	prev := lipgloss.ColorProfile()
	b.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	lipgloss.SetColorProfile(termenv.TrueColor)
	st := lipgloss.NewStyle().Foreground(lipgloss.Color("#c0ffee")).Background(lipgloss.Color("#202020")).Bold(true)
	for i := 0; i < b.N; i++ {
		_ = st.Render("Git: Show Log")
	}
}
