package theme

import (
	"strconv"
	"testing"
)

// The integrated terminal's ANSI colours must be readable on every theme's
// editor background (the old nvim defaults put pure blue on near-black).
func TestTerminalColorsReadable(t *testing.T) {
	t.Cleanup(func() { ApplyTheme("vscode-dark-plus") })
	contrast := func(a, b string) float64 {
		x, ok1 := ParseHex(a)
		y, ok2 := ParseHex(b)
		if !ok1 || !ok2 {
			t.Fatalf("bad hex %q / %q", a, b)
		}
		return contrastRatio(x, y)
	}
	for id := range allThemes {
		ApplyTheme(id)
		c := NvimColors(id)
		for i := 0; i < 16; i++ {
			k := "term_" + strconv.Itoa(i)
			if c[k] == "" {
				t.Fatalf("%s: missing %s", id, k)
			}
			// 0 is "black" (a background colour on dark themes) and 8 is
			// the dim grey; the rest are used as text.
			if i == 0 || i == 8 {
				continue
			}
			if r := contrast(c[k], c["bg"]); r < 3 {
				t.Errorf("%s: %s %s on bg %s contrast %.2f < 3", id, k, c[k], c["bg"], r)
			}
		}
	}
}
