package termimg

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"github.com/muesli/termenv"
)

// RenderBlocks draws img as cols×rows half-block cells. Each cell shows two
// vertical pixels: "▀" with fg = top pixel and bg = bottom pixel. Pixels are
// alpha-blended onto bg. Colors go through profile, so 256 / 16 color
// terminals get the nearest palette color; the Ascii profile gets shade
// characters and no color at all. Every line is exactly cols cells wide.
// Pure: safe to cache.
func RenderBlocks(img image.Image, cols, rows int, bg color.Color, profile termenv.Profile) []string {
	if cols <= 0 || rows <= 0 || img == nil {
		return nil
	}
	px := resample(img, cols, rows*2)
	back := toRGBA(bg)
	lines := make([]string, rows)
	var sb strings.Builder
	if profile == termenv.Ascii {
		const ramp = " ░▒▓█"
		shades := []rune(ramp)
		for y := 0; y < rows; y++ {
			sb.Reset()
			for x := 0; x < cols; x++ {
				t := over(px[(2*y)*cols+x], back)
				b := over(px[(2*y+1)*cols+x], back)
				l := (luma(t) + luma(b)) / 2
				idx := int(l * float64(len(shades)-1) / 255.0)
				sb.WriteRune(shades[idx])
			}
			lines[y] = sb.String()
		}
		return lines
	}
	for y := 0; y < rows; y++ {
		sb.Reset()
		lastFg, lastBg := "", ""
		for x := 0; x < cols; x++ {
			t := over(px[(2*y)*cols+x], back)
			b := over(px[(2*y+1)*cols+x], back)
			fg := seq(profile, t, false)
			bgs := seq(profile, b, true)
			if fg != lastFg || bgs != lastBg {
				sb.WriteString("\x1b[")
				sb.WriteString(fg)
				if bgs != "" {
					if fg != "" {
						sb.WriteByte(';')
					}
					sb.WriteString(bgs)
				}
				sb.WriteByte('m')
				lastFg, lastBg = fg, bgs
			}
			sb.WriteString("▀")
		}
		sb.WriteString("\x1b[0m")
		lines[y] = sb.String()
	}
	return lines
}

// seq returns the SGR parameters (without ESC[ and m) for c in profile.
func seq(p termenv.Profile, c color.RGBA, bg bool) string {
	hex := fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	tc := p.Color(hex)
	if tc == nil {
		return ""
	}
	return tc.Sequence(bg)
}

func luma(c color.RGBA) float64 {
	return 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
}
