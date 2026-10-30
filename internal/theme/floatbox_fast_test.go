package theme

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// floatBoxLipgloss is FloatBox without its fast path.
func floatBoxLipgloss(content string, width int) string {
	c := Float()
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(c.Border).
		BorderBackground(c.Bg).Background(c.Bg).Width(width - 2).Render(content)
}

// FloatBox's fast path writes lipgloss's bytes: rows that fit, shorter
// rows, empty rows, styled rows, wide glyphs, in every colour profile;
// rows that do not fit take the lipgloss path.
func TestFloatBoxFastSameAsLipgloss(t *testing.T) {
	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	sty := []lipgloss.Style{
		lipgloss.NewStyle(),
		lipgloss.NewStyle().Background(lipgloss.Color("#112233")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Bold(true),
	}
	pieces := []string{"a", " ", "名", "…", "─", "x", "  ", "é", "▌"}
	r := rand.New(rand.NewSource(7))
	row := func() string {
		var b strings.Builder
		for n := r.Intn(5); n > 0; n-- {
			var p strings.Builder
			for k := r.Intn(4) + 1; k > 0; k-- {
				p.WriteString(pieces[r.Intn(len(pieces))])
			}
			b.WriteString(sty[r.Intn(len(sty))].Render(p.String()))
		}
		return b.String()
	}
	for _, prof := range []termenv.Profile{termenv.TrueColor, termenv.ANSI256, termenv.ANSI, termenv.Ascii} {
		lipgloss.SetColorProfile(prof)
		for i := 0; i < 400; i++ {
			rows := make([]string, r.Intn(6)+1)
			for k := range rows {
				rows[k] = row()
			}
			content := strings.Join(rows, "\n")
			for _, w := range []int{6, 12, 30} {
				if got, want := FloatBox(content, w), floatBoxLipgloss(content, w); got != want {
					t.Fatalf("profile %v width %d content %q:\nfast     %q\nlipgloss %q", prof, w, content, got, want)
				}
			}
		}
	}
}

// BenchmarkFloatBox is a 24-row picker card; BenchmarkFloatBoxLipgloss
// the same without the fast path.
func BenchmarkFloatBox(b *testing.B) { benchFloatBox(b, FloatBox) }

func BenchmarkFloatBoxLipgloss(b *testing.B) { benchFloatBox(b, floatBoxLipgloss) }

func benchFloatBox(b *testing.B, box func(string, int) string) {
	prev := lipgloss.ColorProfile()
	b.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	lipgloss.SetColorProfile(termenv.TrueColor)
	st := lipgloss.NewStyle().Background(lipgloss.Color("#303030")).Foreground(lipgloss.Color("#c0c0c0"))
	rows := make([]string, 24)
	for i := range rows {
		rows[i] = st.Render(strings.Repeat("row text ", 6))
	}
	content := strings.Join(rows, "\n")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = box(content, 56)
	}
}
