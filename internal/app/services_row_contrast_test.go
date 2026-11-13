package app

import (
	"math"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/theme"
)

func wcagLum(c theme.RGB) float64 {
	f := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c.R) + 0.7152*f(c.G) + 0.0722*f(c.B)
}

func wcagRatio(a, b theme.RGB) float64 {
	la, lb := wcagLum(a), wcagLum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// SCN-34: on the selected row of the Services / Containers list the uptime
// ("2h 14m") reads in a light theme too (it was muted text on the purple
// selection wash, 2.4:1).
func TestServiceRowUptimeReadsOnSelectedRow(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	defer theme.ApplyTheme("vscode-dark-plus")
	st := svcTestState()
	pg, _ := st.findService(svcSecCompose, "postgres")
	for _, id := range []string{"catppuccin-latte", "rose-pine-dawn", "vscode-dark-plus"} {
		theme.ApplyTheme(id)
		for _, focused := range []bool{true, false} {
			row := renderServiceRow(pg, st.statusOf(svcSecCompose, "postgres"), true, focused, 40, svcTestNow)
			found := false
			for _, c := range parseANSIRow(row) {
				if c.glyph == "h" {
					s := parseSGR(c.sgr)
					found = true
					if r := wcagRatio(s.fg, s.bg); r < 3.0 {
						t.Errorf("%s focused=%v: uptime contrast %.2f:1 < 3:1", id, focused, r)
					}
					break
				}
			}
			if !found {
				t.Fatalf("%s: no uptime in the row", id)
			}
		}
	}
}

// SCN-34: the attachment chip of a sent message ("+ model.go:120-160") reads
// on the light theme (muted text on its wash was 2.7:1).
func TestAIRightChipReadsOnLightTheme(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	defer theme.ApplyTheme("vscode-dark-plus")
	for _, id := range []string{"catppuccin-latte", "rose-pine-dawn"} {
		theme.ApplyTheme(id)
		for _, c := range parseANSIRow(aiRightChip("+ model.go:120-160", 60).styled) {
			if c.glyph != "m" {
				continue
			}
			s := parseSGR(c.sgr)
			if r := wcagRatio(s.fg, s.bg); r < 3.0 {
				t.Errorf("%s: chip contrast %.2f:1 < 3:1", id, r)
			}
			break
		}
	}
}
