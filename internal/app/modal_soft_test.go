package app

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Spec (UI review #2): a card over the page must not turn the page black.
// The dimmed backdrop stays readable (text ≥ 3:1 on its dimmed
// background), and the drop shadow is a soft step, not a black bar (the
// shadow cell keeps at least 55% of the brightness next to it).
func TestModalBackdropReadableAndShadowSoft(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	defer theme.ApplyTheme("vscode-dark-plus")
	const w, h = 40, 12
	for _, nt := range theme.AvailableThemes() {
		theme.ApplyTheme(nt.ID)
		row := theme.FgBg(theme.TextPrimary, theme.BgEditor).Render(strings.Repeat("x", w))
		base := strings.TrimSuffix(strings.Repeat(row+"\n", h), "\n")
		card := strings.TrimSuffix(strings.Repeat(theme.Bg(theme.BgFloat).Render(strings.Repeat(" ", 10))+"\n", 4), "\n")
		out := strings.Split(modalOverlay(base, card, w, h, nil), "\n")
		left, top := (w-10)/2, (h-4)/2

		back := parseSGR(parseANSIRow(out[0])[0].sgr)
		if r := lumContrast(back.fg, back.bg); r < 3.0 {
			t.Errorf("%s: backdrop text contrast %.2f < 3.0", nt.ID, r)
		}
		sh := parseSGR(parseANSIRow(out[top+1])[left+10].sgr)
		if k := keptBrightness(sh.bg, back.bg); k < 0.55 {
			t.Errorf("%s: shadow keeps %.2f of the backdrop < 0.55 (a hard bar)", nt.ID, k)
		}
		// Toasts draw without a backdrop: the shadow sits on the page.
		out = strings.Split(modalOverlay(base, card, w, h, &ModalOpts{NoDim: true}), "\n")
		page := parseSGR(parseANSIRow(out[0])[0].sgr)
		sh = parseSGR(parseANSIRow(out[top+1])[left+10].sgr)
		if k := keptBrightness(sh.bg, page.bg); k < 0.55 {
			t.Errorf("%s: toast shadow keeps %.2f of the page < 0.55 (a hard bar)", nt.ID, k)
		}
	}
}

func lumContrast(a, b theme.RGB) float64 {
	lum := func(c theme.RGB) float64 {
		ch := func(v uint8) float64 {
			x := float64(v) / 255
			if x <= 0.03928 {
				return x / 12.92
			}
			return math.Pow((x+0.055)/1.055, 2.4)
		}
		return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
	}
	la, lb := lum(a), lum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// keptBrightness is the channel sum of shadow over the channel sum of
// the colour it darkens (1 = no shadow, 0 = black).
func keptBrightness(shadow, under theme.RGB) float64 {
	sum := func(c theme.RGB) float64 { return float64(c.R) + float64(c.G) + float64(c.B) }
	if sum(under) == 0 {
		return 1
	}
	return sum(shadow) / sum(under)
}
