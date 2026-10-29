package app

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// PERF-5: sgrPaint draws exactly what lipgloss Render draws for a
// foreground / background style, in every color profile, for any text
// (tabs and line breaks go through lipgloss itself).
func TestSgrPaintMatchesLipgloss(t *testing.T) {
	old := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(old)
	texts := []string{"", " ", "nginx", "日本語 · ok", "a\tb", "two\nlines", "cr\r\nlf", "   pad   ", "\x1b[1mraw\x1b[0m"}
	colors := []lipgloss.Color{"", "#1e1e2e", "#ff0000", "203", "7", "color12"}
	for _, p := range []termenv.Profile{termenv.TrueColor, termenv.ANSI256, termenv.ANSI, termenv.Ascii} {
		lipgloss.SetColorProfile(p)
		for _, fg := range colors {
			for _, bg := range colors {
				st := lipgloss.NewStyle()
				if fg != "" {
					st = st.Foreground(fg)
				}
				if bg != "" {
					st = st.Background(bg)
				}
				for _, s := range texts {
					if got, want := sgrPaint(fg, bg, s), st.Render(s); got != want {
						t.Fatalf("profile %v fg %q bg %q text %q:\n got %q\nwant %q", p, fg, bg, s, got, want)
					}
				}
			}
		}
	}
}

func BenchmarkSgrPaint(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = sgrPaint("#ff0000", "#1e1e2e", "registry.example.com/team/app")
	}
}

func BenchmarkLipglossRender(b *testing.B) {
	st := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Background(lipgloss.Color("#1e1e2e"))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = st.Render("registry.example.com/team/app")
	}
}
