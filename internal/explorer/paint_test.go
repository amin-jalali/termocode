package explorer

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Spec: paint gives exactly what the lipgloss style gives, for every
// colour profile, colour, bold flag and text (tabs and line breaks too).
func TestPaintMatchesLipgloss(t *testing.T) {
	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	colors := []lipgloss.Color{"", "#ff0000", "#1e1e2e", "12", "208"}
	texts := []string{"", " ", "   ", "abc", "│", "▌", " ", "file name.go", "a\tb", "x\ny", "a\r\nb"}
	for _, p := range []termenv.Profile{termenv.TrueColor, termenv.ANSI256, termenv.ANSI, termenv.Ascii} {
		lipgloss.SetColorProfile(p)
		for _, fg := range colors {
			for _, bg := range colors[1:] {
				for _, bold := range []bool{false, true} {
					for _, s := range texts {
						st := lipgloss.NewStyle()
						if fg != "" {
							st = st.Foreground(fg)
						}
						want := st.Background(bg).Bold(bold).Render(s)
						if got := paint(fg, bg, bold, s); got != want {
							t.Fatalf("profile %v fg %q bg %q bold %v text %q:\n got %q\nwant %q", p, fg, bg, bold, s, got, want)
						}
					}
				}
			}
		}
	}
}
