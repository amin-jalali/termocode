package theme

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// RGB is a plain 8-bit colour used for blending and gradients. Views never
// write RGB literals; they derive them from palette tokens (ToRGB) so every
// colour on screen still comes from the active theme.
type RGB struct{ R, G, B uint8 }

// ParseHex reads "#rrggbb" (or "rrggbb"). ok=false on anything else.
func ParseHex(s string) (RGB, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return RGB{}, false
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b); err != nil {
		return RGB{}, false
	}
	return RGB{r, g, b}, true
}

// Hex formats c as "#rrggbb".
func (c RGB) Hex() string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// LG converts c to a lipgloss colour (downgraded by lipgloss on output).
func (c RGB) LG() lipgloss.Color { return lipgloss.Color(c.Hex()) }

// ToRGB resolves a palette token to RGB. Tokens without a truecolor entry
// fall back to the xterm-256 cube value of their index.
func ToRGB(c Color256) RGB {
	if v, ok := ParseHex(Hex(c)); ok {
		return v
	}
	return xterm256(c)
}

// Lerp blends a→b by t (0 = a, 1 = b).
func Lerp(a, b RGB, t float64) RGB {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	l := func(x, y uint8) uint8 { return uint8(math.Round(float64(x)*(1-t) + float64(y)*t)) }
	return RGB{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B)}
}

// Mix blends two palette tokens and returns a lipgloss colour. Use it for
// derived surfaces (hover tints, soft selection bars) instead of hex.
func Mix(a, b Color256, t float64) lipgloss.Color {
	return Lerp(ToRGB(a), ToRGB(b), t).LG()
}

// Luma is the perceived brightness 0..1.
func (c RGB) Luma() float64 {
	return (0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)) / 255
}

// IsLight reports whether the active theme has a light editor background.
func IsLight() bool { return ToRGB(BgEditor).Luma() > 0.5 }

// Gradient returns n colours evenly spread across the stops.
func Gradient(n int, stops ...RGB) []RGB {
	out := make([]RGB, n)
	if n == 0 || len(stops) == 0 {
		return out
	}
	if len(stops) == 1 || n == 1 {
		for i := range out {
			out[i] = stops[0]
		}
		return out
	}
	for i := range out {
		pos := float64(i) / float64(n-1) * float64(len(stops)-1)
		k := int(pos)
		if k >= len(stops)-1 {
			k = len(stops) - 2
		}
		out[i] = Lerp(stops[k], stops[k+1], pos-float64(k))
	}
	return out
}

// GradientText paints s cell by cell along the stops (foreground only, on
// bg). When the terminal can't show truecolor the whole run collapses to
// the middle stop: a 256/16-colour approximation of a gradient bands
// badly, a flat colour reads cleaner.
func GradientText(s string, bg Color256, bold bool, stops ...RGB) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return ""
	}
	base := lipgloss.NewStyle().Background(LG(bg)).Bold(bold)
	if !TrueColor() || len(stops) < 2 {
		mid := stops[len(stops)/2]
		return base.Foreground(mid.LG()).Render(s)
	}
	cols := Gradient(len(runes), stops...)
	var b strings.Builder
	for i, r := range runes {
		if r == ' ' {
			b.WriteString(base.Render(" "))
			continue
		}
		b.WriteString(base.Foreground(cols[i].LG()).Render(string(r)))
	}
	return b.String()
}

// GradientBar renders width cells of glyph g whose foreground runs along
// the stops (used for the active-tab underline and dashboard rules).
func GradientBar(width int, g string, bg Color256, stops ...RGB) string {
	if width <= 0 {
		return ""
	}
	return GradientText(strings.Repeat(g, width), bg, false, stops...)
}

// Profile is the colour profile lipgloss renders with.
func Profile() termenv.Profile { return lipgloss.ColorProfile() }

// TrueColor reports whether 24-bit colour is being emitted.
func TrueColor() bool { return Profile() == termenv.TrueColor }

// SGR returns the escape sequence that sets fg and/or bg to the given
// colours in the active profile ("" when the profile has no colour).
// Hand-built cell renderers (modal overlay, image half-blocks) use it so
// a 16-colour terminal never receives a 24-bit sequence.
func SGR(fg, bg *RGB) string {
	p := Profile()
	if p == termenv.Ascii {
		return ""
	}
	var parts []string
	if fg != nil {
		if s := p.Color(fg.Hex()); s != nil {
			parts = append(parts, s.Sequence(false))
		}
	}
	if bg != nil {
		if s := p.Color(bg.Hex()); s != nil {
			parts = append(parts, s.Sequence(true))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(parts, ";") + "m"
}

// FgSeq / BgSeq are SGR sequences that set a token as foreground /
// background in the active profile ("" on a no-colour terminal). For
// hand-built ANSI strings (diff colouring, test output).
func FgSeq(c Color256) string { v := ToRGB(c); return SGR(&v, nil) }

// BgSeq: see FgSeq.
func BgSeq(c Color256) string { v := ToRGB(c); return SGR(nil, &v) }

// BgSeqMix is BgSeq of a blend of two tokens.
func BgSeqMix(a, b Color256, t float64) string { v := Lerp(ToRGB(a), ToRGB(b), t); return SGR(nil, &v) }

func xterm256(n int) RGB {
	if n < 0 || n > 255 {
		return RGB{}
	}
	if n < 16 {
		base := [16][3]uint8{
			{0, 0, 0}, {128, 0, 0}, {0, 128, 0}, {128, 128, 0},
			{0, 0, 128}, {128, 0, 128}, {0, 128, 128}, {192, 192, 192},
			{128, 128, 128}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
			{0, 0, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
		}
		return RGB{base[n][0], base[n][1], base[n][2]}
	}
	if n < 232 {
		k := n - 16
		lv := [6]uint8{0, 95, 135, 175, 215, 255}
		return RGB{lv[k/36%6], lv[k/6%6], lv[k%6]}
	}
	v := uint8(8 + (n-232)*10)
	return RGB{v, v, v}
}
