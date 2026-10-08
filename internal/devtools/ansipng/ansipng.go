// Package ansipng rasterises a frame of ANSI-styled terminal text (what a
// Bubble Tea View returns) into a PNG, cell by cell, with a real monospace
// font. It is a developer tool used by internal/devtools/shots to make the
// README screenshots and to check the look of the UI without a terminal.
//
// Only the escape sequences termocode emits are understood: SGR colours
// (16 / 256 / truecolor), bold, faint, italic, underline and reverse. Other
// sequences (OSC, APC, DCS, cursor moves) are skipped. Block elements
// (U+2580–U+259F) are drawn as exact rectangles so half-block images and
// bars line up without font gaps.
package ansipng

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Cell is one terminal cell after parsing.
type Cell struct {
	Glyph     string
	FG, BG    color.RGBA
	HasFG     bool
	HasBG     bool
	Bold      bool
	Faint     bool
	Italic    bool
	Underline bool
	Wide      bool // first half of a 2-cell glyph
	Cont      bool // second half of a 2-cell glyph (draw nothing)
}

// Options controls rasterisation.
type Options struct {
	FontRegular string   // path to a TTF/OTF (a Nerd Font for icons)
	FontBold    string   // optional bold face
	Size        float64  // font size in px (default 15)
	Fallbacks   []string // fonts tried for glyphs the main font lacks
	DefaultFG   color.RGBA
	DefaultBG   color.RGBA
	Padding     int // outer padding in px, painted with DefaultBG
}

// Renderer holds the loaded faces.
type Renderer struct {
	opt          Options
	regular      font.Face
	bold         font.Face
	fallbacks    []font.Face
	cellW, cellH int
	ascent       int
}

// New loads the fonts and computes the cell grid.
func New(opt Options) (*Renderer, error) {
	if opt.Size == 0 {
		opt.Size = 15
	}
	if opt.DefaultFG == (color.RGBA{}) {
		opt.DefaultFG = color.RGBA{0xd0, 0xd0, 0xd0, 0xff}
	}
	if opt.DefaultBG == (color.RGBA{}) {
		opt.DefaultBG = color.RGBA{0x1c, 0x1c, 0x1c, 0xff}
	}
	reg, err := loadFace(opt.FontRegular, opt.Size)
	if err != nil {
		return nil, err
	}
	bold := reg
	if opt.FontBold != "" {
		if b, err := loadFace(opt.FontBold, opt.Size); err == nil {
			bold = b
		}
	}
	m := reg.Metrics()
	adv, _ := reg.GlyphAdvance('M')
	r := &Renderer{opt: opt, regular: reg, bold: bold}
	for _, fp := range opt.Fallbacks {
		if f, err := loadFace(fp, opt.Size); err == nil {
			r.fallbacks = append(r.fallbacks, f)
		}
	}
	r.cellW = int(math.Round(float64(adv) / 64))
	r.ascent = int(math.Ceil(float64(m.Ascent) / 64))
	r.cellH = r.ascent + int(math.Ceil(float64(m.Descent)/64))
	return r, nil
}

func loadFace(path string, size float64) (font.Face, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("font: %w", err)
	}
	f, err := opentype.Parse(b)
	if err != nil {
		return nil, fmt.Errorf("font %s: %w", path, err)
	}
	return opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
}

// CellSize reports the pixel size of one cell.
func (r *Renderer) CellSize() (int, int) { return r.cellW, r.cellH }

// Render rasterises a frame. cols is the frame width in cells (rows are
// padded / clipped to it).
func (r *Renderer) Render(frame string, cols int) *image.RGBA {
	lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	grid := make([][]Cell, len(lines))
	for i, l := range lines {
		grid[i] = Parse(l, cols)
	}
	p := r.opt.Padding
	img := image.NewRGBA(image.Rect(0, 0, cols*r.cellW+2*p, len(lines)*r.cellH+2*p))
	draw.Draw(img, img.Bounds(), &image.Uniform{r.opt.DefaultBG}, image.Point{}, draw.Src)
	for y, row := range grid {
		for x, c := range row {
			r.drawCell(img, p+x*r.cellW, p+y*r.cellH, c)
		}
	}
	return img
}

// WritePNG renders and writes a PNG file.
func (r *Renderer) WritePNG(path, frame string, cols int) error {
	img := r.Render(frame, cols)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func (r *Renderer) drawCell(img *image.RGBA, px, py int, c Cell) {
	if c.Cont {
		return
	}
	w := r.cellW
	if c.Wide {
		w *= 2
	}
	bg := r.opt.DefaultBG
	if c.HasBG {
		bg = c.BG
	}
	fg := r.opt.DefaultFG
	if c.HasFG {
		fg = c.FG
	}
	if c.Faint {
		fg = mix(fg, bg, 0.5)
	}
	fill(img, image.Rect(px, py, px+w, py+r.cellH), bg)
	if c.Glyph == "" || c.Glyph == " " {
		if c.Underline {
			fill(img, image.Rect(px, py+r.cellH-2, px+w, py+r.cellH-1), fg)
		}
		return
	}
	rn := []rune(c.Glyph)[0]
	if r.drawBlock(img, px, py, rn, fg, bg) {
		return
	}
	face := r.regular
	if c.Bold {
		face = r.bold
	}
	if _, ok := face.GlyphAdvance(rn); !ok {
		for _, f := range r.fallbacks {
			if _, ok := f.GlyphAdvance(rn); ok {
				face = f
				break
			}
		}
	}
	d := font.Drawer{Dst: img, Src: &image.Uniform{fg}, Face: face,
		Dot: fixed.P(px, py+r.ascent)}
	// Nerd Font icons are often wider than one advance; centre them in the
	// cell(s) the terminal gives them so they don't spill.
	adv := d.MeasureString(c.Glyph)
	if room := fixed.I(w); adv < room {
		d.Dot.X += (room - adv) / 2
	}
	d.DrawString(c.Glyph)
	if c.Underline {
		fill(img, image.Rect(px, py+r.cellH-2, px+w, py+r.cellH-1), fg)
	}
}

// drawBlock paints U+2580–U+259F block elements as exact rectangles.
func (r *Renderer) drawBlock(img *image.RGBA, px, py int, rn rune, fg, bg color.RGBA) bool {
	w, h := r.cellW, r.cellH
	rect := func(x0, y0, x1, y1 float64, c color.RGBA) {
		fill(img, image.Rect(px+int(math.Round(x0*float64(w))), py+int(math.Round(y0*float64(h))),
			px+int(math.Round(x1*float64(w))), py+int(math.Round(y1*float64(h)))), c)
	}
	switch {
	case rn == '▀':
		rect(0, 0, 1, 0.5, fg)
	case rn == '▄':
		rect(0, 0.5, 1, 1, fg)
	case rn == '█':
		rect(0, 0, 1, 1, fg)
	case rn == '▌':
		rect(0, 0, 0.5, 1, fg)
	case rn == '▐':
		rect(0.5, 0, 1, 1, fg)
	case rn >= '▁' && rn <= '▇': // lower n/8 blocks
		n := float64(rn-'▁'+1) / 8
		rect(0, 1-n, 1, 1, fg)
	case rn >= '▉' && rn <= '▏': // left n/8 blocks (▉ = 7/8 … ▏ = 1/8)
		n := float64(8-(rn-'▉')-1) / 8
		rect(0, 0, n, 1, fg)
	case rn == '▔':
		rect(0, 0, 1, 0.125, fg)
	case rn == '▕':
		rect(0.875, 0, 1, 1, fg)
	case rn == '░' || rn == '▒' || rn == '▓':
		a := map[rune]float64{'░': 0.25, '▒': 0.5, '▓': 0.75}[rn]
		rect(0, 0, 1, 1, mix(fg, bg, a))
	default:
		q, ok := quadrants[rn]
		if !ok {
			return false
		}
		if q&1 != 0 {
			rect(0, 0, 0.5, 0.5, fg)
		}
		if q&2 != 0 {
			rect(0.5, 0, 1, 0.5, fg)
		}
		if q&4 != 0 {
			rect(0, 0.5, 0.5, 1, fg)
		}
		if q&8 != 0 {
			rect(0.5, 0.5, 1, 1, fg)
		}
	}
	return true
}

// quadrant bitmasks: 1=UL 2=UR 4=LL 8=LR.
var quadrants = map[rune]int{
	'▖': 4, '▗': 8, '▘': 1, '▝': 2, '▙': 1 | 4 | 8, '▚': 1 | 8, '▛': 1 | 2 | 4,
	'▜': 1 | 2 | 8, '▞': 2 | 4, '▟': 2 | 4 | 8,
}

func fill(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	draw.Draw(img, r, &image.Uniform{c}, image.Point{}, draw.Src)
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	l := func(x, y uint8) uint8 { return uint8(math.Round(float64(x)*(1-t) + float64(y)*t)) }
	return color.RGBA{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B), 0xff}
}

// Parse turns one ANSI line into exactly cols cells.
func Parse(line string, cols int) []Cell {
	out := make([]Cell, 0, cols)
	var cur Cell
	reverse := false
	i := 0
	for i < len(line) {
		ch := line[i]
		if ch == 0x1b && i+1 < len(line) {
			switch line[i+1] {
			case '[':
				j := i + 2
				for j < len(line) && (line[j] < 0x40 || line[j] > 0x7e) {
					j++
				}
				if j < len(line) && line[j] == 'm' {
					applySGR(&cur, &reverse, line[i+2:j])
				}
				i = j + 1
				continue
			case ']', '_', 'P', '^', 'X':
				// String sequences end at BEL or ST.
				j := i + 2
				for j < len(line) {
					if line[j] == 0x07 {
						j++
						break
					}
					if line[j] == 0x1b && j+1 < len(line) && line[j+1] == '\\' {
						j += 2
						break
					}
					j++
				}
				i = j
				continue
			default:
				i += 2
				continue
			}
		}
		// Decode one grapheme-ish rune.
		rn, size := decodeRune(line[i:])
		g := line[i : i+size]
		i += size
		wd := runewidth.RuneWidth(rn)
		if wd == 0 {
			if len(out) > 0 {
				out[len(out)-1].Glyph += g
			}
			continue
		}
		c := cur
		if reverse {
			c.FG, c.BG = c.BG, c.FG
			c.HasFG, c.HasBG = c.HasBG, c.HasFG
			if !c.HasFG {
				c.FG, c.HasFG = color.RGBA{0x1c, 0x1c, 0x1c, 0xff}, true
			}
		}
		c.Glyph = g
		if wd == 2 {
			c.Wide = true
			out = append(out, c)
			cont := c
			cont.Glyph, cont.Wide, cont.Cont = "", false, true
			out = append(out, cont)
		} else {
			out = append(out, c)
		}
	}
	for len(out) < cols {
		out = append(out, Cell{Glyph: " "})
	}
	return out[:cols]
}

func decodeRune(s string) (rune, int) {
	for i, r := range s {
		if i == 0 {
			n := len(string(r))
			if r == 0xFFFD && len(s) > 0 && s[0] < 0x80 {
				n = 1
			}
			return r, n
		}
	}
	return 0, 1
}

func applySGR(c *Cell, reverse *bool, body string) {
	if body == "" {
		*c = Cell{}
		*reverse = false
		return
	}
	parts := strings.Split(body, ";")
	for i := 0; i < len(parts); i++ {
		n, _ := strconv.Atoi(parts[i])
		switch {
		case n == 0:
			*c = Cell{}
			*reverse = false
		case n == 1:
			c.Bold = true
		case n == 2:
			c.Faint = true
		case n == 3:
			c.Italic = true
		case n == 4:
			c.Underline = true
		case n == 7:
			*reverse = true
		case n == 22:
			c.Bold, c.Faint = false, false
		case n == 23:
			c.Italic = false
		case n == 24:
			c.Underline = false
		case n == 27:
			*reverse = false
		case n >= 30 && n <= 37:
			c.FG, c.HasFG = ansi16(n-30), true
		case n == 39:
			c.HasFG = false
		case n >= 40 && n <= 47:
			c.BG, c.HasBG = ansi16(n-40), true
		case n == 49:
			c.HasBG = false
		case n >= 90 && n <= 97:
			c.FG, c.HasFG = ansi16(n-90+8), true
		case n >= 100 && n <= 107:
			c.BG, c.HasBG = ansi16(n-100+8), true
		case n == 38 || n == 48 || n == 58:
			var col color.RGBA
			ok := false
			if i+1 < len(parts) && parts[i+1] == "5" && i+2 < len(parts) {
				k, _ := strconv.Atoi(parts[i+2])
				col, ok = xterm256(k), true
				i += 2
			} else if i+1 < len(parts) && parts[i+1] == "2" && i+4 < len(parts) {
				rr, _ := strconv.Atoi(parts[i+2])
				gg, _ := strconv.Atoi(parts[i+3])
				bb, _ := strconv.Atoi(parts[i+4])
				col, ok = color.RGBA{uint8(rr), uint8(gg), uint8(bb), 0xff}, true
				i += 4
			}
			if ok && n == 38 {
				c.FG, c.HasFG = col, true
			} else if ok && n == 48 {
				c.BG, c.HasBG = col, true
			}
		}
	}
}

var base16 = [16][3]uint8{
	{0x1d, 0x1f, 0x21}, {0xcc, 0x66, 0x66}, {0xb5, 0xbd, 0x68}, {0xf0, 0xc6, 0x74},
	{0x81, 0xa2, 0xbe}, {0xb2, 0x94, 0xbb}, {0x8a, 0xbe, 0xb7}, {0xc5, 0xc8, 0xc6},
	{0x66, 0x66, 0x66}, {0xd5, 0x4e, 0x53}, {0xb9, 0xca, 0x4a}, {0xe7, 0xc5, 0x47},
	{0x7a, 0xa6, 0xda}, {0xc3, 0x97, 0xd8}, {0x70, 0xc0, 0xb1}, {0xea, 0xea, 0xea},
}

func ansi16(n int) color.RGBA {
	b := base16[n&15]
	return color.RGBA{b[0], b[1], b[2], 0xff}
}

func xterm256(n int) color.RGBA {
	if n < 16 {
		return ansi16(n)
	}
	if n < 232 {
		k := n - 16
		lv := [6]uint8{0, 95, 135, 175, 215, 255}
		return color.RGBA{lv[k/36%6], lv[k/6%6], lv[k%6], 0xff}
	}
	v := uint8(8 + (n-232)*10)
	return color.RGBA{v, v, v, 0xff}
}
