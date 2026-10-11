package app

import (
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Floating overlay compositor (modals, pickers, prompts, confirms, menus,
// toasts).
//
// The terminal has no alpha channel, so a "floating" card is an
// illusion built from cells:
//
//  1. The frame behind the card is dimmed (backdrop), so the card stands
//     out while the editor stays readable around it.
//  2. The card itself is opaque: it paints its own BgFloat surface.
//  3. A soft drop shadow — the cells one column right and one row below
//     the card, blended toward the theme's Shadow token — lifts the card
//     off the page.
//  4. While an open animation runs, the card slides up a couple of rows
//     and fades in from the backdrop (Progress < 1).
//
// Colour math only happens on truecolor / 256-colour terminals. On a
// 16-colour terminal the backdrop uses the SGR "faint" attribute and the
// shadow a plain dark background, so no 24-bit sequence is ever written.

// ModalOpts tweaks one overlay. nil = defaults (dim backdrop, shadow, no
// animation).
type ModalOpts struct {
	// OutsideDim is the backdrop intensity (0..1, default 0.45).
	OutsideDim float64
	// NoDim keeps the backdrop at full intensity (menus, toasts).
	NoDim bool
	// NoShadow skips the drop shadow.
	NoShadow bool
	// Animating + Progress drive the open animation (0 → 1).
	Animating bool
	Progress  float64
	// At, when set, places the card at (X, Y) instead of centring it.
	At     bool
	X, Y   int
	Slide  int // rows the card travels while animating (default 2)
	SlideX int // columns the card travels while animating (toasts)
}

const defaultOutsideDim = 0.62

// modalOverlay centres box over base and composites it (see above).
func modalOverlay(base, box string, screenW, screenH int, opts *ModalOpts) string {
	boxLines := strings.Split(strings.TrimRight(box, "\n"), "\n")
	if len(boxLines) == 0 {
		return base
	}
	boxH := len(boxLines)
	boxW := lipgloss.Width(boxLines[0])
	if boxW == 0 {
		return base
	}
	o := ModalOpts{}
	if opts != nil {
		o = *opts
	}
	p := 1.0
	if o.Animating {
		p = clamp01(o.Progress)
	}
	leftCol := (screenW - boxW) / 2
	topRow := (screenH - boxH) / 2
	if o.At {
		leftCol, topRow = o.X, o.Y
	}
	slide := o.Slide
	if slide == 0 && !o.At {
		slide = 2
	}
	// Slide in from above for top-anchored cards (pickers), from below
	// for centred ones.
	if o.At && slide > 0 {
		slide = -slide
	}
	topRow += int(math.Round((1 - p) * float64(slide)))
	leftCol += int(math.Round((1 - p) * float64(o.SlideX)))
	if leftCol < 0 {
		leftCol = 0
	}
	if topRow < 0 {
		topRow = 0
	}

	var lines []string
	dim := 1.0
	if o.NoDim {
		lines = strings.Split(base, "\n")
	} else {
		dim = o.OutsideDim
		if dim == 0 {
			dim = defaultOutsideDim
		}
		// The backdrop fades in with the card.
		dim = 1 - (1-dim)*p
		cached := dimmedBaseCached(base, leftCol, topRow, boxW, boxH, dim)
		lines = make([]string, len(cached))
		copy(lines, cached)
	}

	for i, bl := range boxLines {
		row := topRow + i
		if row < 0 || row >= len(lines) {
			continue
		}
		under := parseANSIRow(lines[row])
		bl = edgeOnBackdrop(bl, under, leftCol, i == 0 || i == boxH-1, dim)
		if p < 1 {
			bl = fadeRow(bl, under, leftCol, p)
		}
		// A card sliding in from the right is partly off screen: clip it,
		// or the row gets wider than the screen and the terminal wraps it.
		if room := screenW - leftCol; room <= 0 {
			continue
		} else if lipgloss.Width(bl) > room {
			bl = ansi.Truncate(bl, room, "")
		}
		lines[row] = spliceAt(lines[row], bl, leftCol)
	}
	if !o.NoShadow {
		paintShadow(lines, leftCol, topRow, boxW, boxH, p)
	}
	return strings.Join(lines, "\n")
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// edgeOnBackdrop gives the card's border glyphs the backdrop's
// background, so the rounded corners and edges sit on the page instead of
// inside a rectangle of card colour.
func edgeOnBackdrop(row string, under []cellData, leftCol int, edgeRow bool, dim float64) string {
	cells := parseANSIRow(row)
	n := len(cells)
	changed := false
	for i := range cells {
		if !edgeRow && i != 0 && i != n-1 {
			continue
		}
		if !isBorderGlyph(cells[i].glyph) {
			continue
		}
		ui := leftCol + i
		if ui < 0 || ui >= len(under) {
			continue
		}
		u := parseSGR(under[ui].sgr)
		s := parseSGR(cells[i].sgr)
		if !u.hasBg {
			u.bg = theme.ToRGB(theme.BgEditor)
		}
		// The cells under the card were not dimmed with the backdrop;
		// dim them the same way so the edge blends in.
		if dim < 1 && colorMath() {
			u.bg = dimToward(u.bg, dim)
		}
		s.bg, s.hasBg = u.bg, true
		cells[i].sgr = s.render()
		changed = true
	}
	if !changed {
		return row
	}
	return renderCells(cells)
}

func isBorderGlyph(g string) bool {
	switch g {
	case "╭", "╮", "╰", "╯", "─", "│", "┌", "┐", "└", "┘":
		return true
	}
	return false
}

// fadeRow blends a card row toward the cells underneath it by (1-p).
func fadeRow(row string, under []cellData, leftCol int, p float64) string {
	cells := parseANSIRow(row)
	for i := range cells {
		ui := leftCol + i
		ubg := theme.ToRGB(theme.BgEditor)
		if ui >= 0 && ui < len(under) {
			if s := parseSGR(under[ui].sgr); s.hasBg {
				ubg = s.bg
			}
		}
		s := parseSGR(cells[i].sgr)
		if s.hasBg {
			s.bg = theme.Lerp(ubg, s.bg, p)
		} else {
			s.bg, s.hasBg = ubg, true
		}
		if s.hasFg {
			s.fg = theme.Lerp(ubg, s.fg, p)
		}
		cells[i].sgr = s.render()
	}
	return renderCells(cells)
}

// paintShadow darkens the L-shaped strip right of and below the card.
func paintShadow(lines []string, leftCol, topRow, boxW, boxH int, p float64) {
	strength := 0.7 * p
	if theme.IsLight() {
		strength = 0.35 * p
	}
	shadow := theme.ToRGB(theme.Shadow)
	shade := func(row, from, to int) {
		if row < 0 || row >= len(lines) || from >= to {
			return
		}
		cells := parseANSIRow(lines[row])
		changed := false
		for i := from; i < to && i < len(cells); i++ {
			if i < 0 {
				continue
			}
			s := parseSGR(cells[i].sgr)
			if !colorMath() {
				s.bg, s.hasBg = shadow, true
				s.faint = true
			} else {
				bg := theme.ToRGB(theme.BgEditor)
				if s.hasBg {
					bg = s.bg
				}
				s.bg, s.hasBg = theme.Lerp(bg, shadow, strength), true
				if s.hasFg {
					s.fg = theme.Lerp(s.fg, shadow, strength)
				}
			}
			cells[i].sgr = s.render()
			changed = true
		}
		if changed {
			lines[row] = renderCells(cells)
		}
	}
	// Right edge: rows 1..boxH (offset one row down), 2 columns wide
	// (cells are tall, a 1-column shadow reads as a hairline).
	for r := topRow + 1; r <= topRow+boxH; r++ {
		shade(r, leftCol+boxW, leftCol+boxW+2)
	}
	// Bottom edge: one row below, offset two columns right.
	shade(topRow+boxH, leftCol+2, leftCol+boxW)
}

// colorMath reports whether blended colours can be shown faithfully.
func colorMath() bool {
	p := theme.Profile()
	return p == termenv.TrueColor || p == termenv.ANSI256
}

// modalDimCache memoises the dimmed backdrop. Single-entry: only one modal
// is open at a time, and View() runs on the Bubble Tea goroutine.
type modalDimCache struct {
	base                        string
	leftCol, topRow, boxW, boxH int
	outsideDim                  float64
	profile                     termenv.Profile
	lines                       []string
}

var modalDimCacheV *modalDimCache

// dimmedBaseCached returns base split into lines with every cell outside
// the card rectangle dimmed; reused while base and rect are unchanged (the
// hover fast path: the card changes on every mouse move, the base doesn't).
func dimmedBaseCached(base string, leftCol, topRow, boxW, boxH int, outsideDim float64) []string {
	prof := theme.Profile()
	if c := modalDimCacheV; c != nil && c.base == base &&
		c.leftCol == leftCol && c.topRow == topRow &&
		c.boxW == boxW && c.boxH == boxH && c.outsideDim == outsideDim && c.profile == prof {
		return c.lines
	}
	lines := strings.Split(base, "\n")
	if outsideDim < 1 {
		dimOutsideRect(lines, leftCol, topRow, boxW, boxH, outsideDim)
	}
	modalDimCacheV = &modalDimCache{
		base: base, leftCol: leftCol, topRow: topRow,
		boxW: boxW, boxH: boxH, outsideDim: outsideDim, profile: prof, lines: lines,
	}
	return lines
}

// dimOutsideRect dims every cell outside the rectangle by factor (0..1).
func dimOutsideRect(lines []string, leftCol, topRow, boxW, boxH int, factor float64) {
	for i := range lines {
		if i < topRow || i >= topRow+boxH {
			lines[i] = dimRow(lines[i], -1, -1, factor)
		} else {
			lines[i] = dimRow(lines[i], leftCol, leftCol+boxW, factor)
		}
	}
}

// dimRow dims the cells of row outside [skipFrom, skipTo).
func dimRow(row string, skipFrom, skipTo int, factor float64) string {
	cells := parseANSIRow(row)
	cm := colorMath()
	bgDef := theme.ToRGB(theme.BgEditor)
	for i := range cells {
		if i >= skipFrom && i < skipTo {
			continue
		}
		s := parseSGR(cells[i].sgr)
		if !cm {
			s.faint = true
		} else {
			if !s.hasBg {
				s.bg, s.hasBg = bgDef, true
			}
			// Dim toward black on dark themes; on light themes dim toward
			// a grey so the backdrop reads as "shaded", not inverted.
			s.bg = dimToward(s.bg, factor)
			if s.hasFg {
				s.fg = dimToward(s.fg, factor)
			}
		}
		cells[i].sgr = s.render()
	}
	return renderCells(cells)
}

func dimToward(c theme.RGB, factor float64) theme.RGB {
	target := theme.RGB{}
	if theme.IsLight() {
		target = theme.Lerp(theme.ToRGB(theme.Shadow), theme.RGB{}, 0.3)
	}
	return theme.Lerp(target, c, factor)
}

// ── cell parser ───────────────────────────────────────────────────────────

type rgbColor = theme.RGB

type cellData struct {
	sgr   string
	glyph string
}

// parseANSIRow splits one rendered row into cells, each carrying the full
// SGR in effect. `[` (0x5B) is also an SGR final-byte candidate, so the
// CSI introducer `\x1b[` is consumed as a unit.
func parseANSIRow(s string) []cellData {
	var cells []cellData
	current := ""
	i := 0
	for i < len(s) {
		c := s[i]
		if c == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				j++
			}
			sgr := s[i:j]
			switch {
			case sgr == "\x1b[0m" || sgr == "\x1b[m":
				current = ""
			case strings.HasSuffix(sgr, "m"):
				// lipgloss emits one SGR per span; merge consecutive ones
				// (e.g. a bold run inside a coloured run) into one state.
				current = mergeSGR(current, sgr)
			}
			i = j
			continue
		}
		glyphStart := i
		i++
		for i < len(s) && (s[i]&0xC0) == 0x80 {
			i++
		}
		cells = append(cells, cellData{sgr: current, glyph: s[glyphStart:i]})
	}
	return cells
}

// mergeSGR folds next into cur (both full "\x1b[…m" sequences).
func mergeSGR(cur, next string) string {
	if cur == "" {
		return next
	}
	a := parseSGR(cur)
	b := parseSGRInto(a, next)
	return b.render()
}

func renderCells(cells []cellData) string {
	var b strings.Builder
	last := ""
	for _, c := range cells {
		if c.sgr != last {
			if last != "" {
				b.WriteString("\x1b[0m")
			}
			if c.sgr != "" {
				b.WriteString(c.sgr)
			}
			last = c.sgr
		}
		b.WriteString(c.glyph)
	}
	if last != "" {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

// sgrState is a decoded SGR: colours as RGB plus the other attributes.
type sgrState struct {
	fg, bg       rgbColor
	hasFg, hasBg bool
	bold, faint  bool
	italic       bool
	underline    bool
	reverse      bool
	strike       bool
}

func parseSGR(sgr string) sgrState { return parseSGRInto(sgrState{}, sgr) }

func parseSGRInto(s sgrState, sgr string) sgrState {
	if !strings.HasPrefix(sgr, "\x1b[") || !strings.HasSuffix(sgr, "m") {
		return s
	}
	body := sgr[2 : len(sgr)-1]
	if body == "" {
		return sgrState{}
	}
	parts := strings.Split(body, ";")
	for i := 0; i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			continue
		}
		switch {
		case n == 0:
			s = sgrState{}
		case n == 1:
			s.bold = true
		case n == 2:
			s.faint = true
		case n == 3:
			s.italic = true
		case n == 4:
			s.underline = true
		case n == 7:
			s.reverse = true
		case n == 9:
			s.strike = true
		case n == 22:
			s.bold, s.faint = false, false
		case n == 23:
			s.italic = false
		case n == 24:
			s.underline = false
		case n == 27:
			s.reverse = false
		case n == 29:
			s.strike = false
		case n >= 30 && n <= 37:
			s.fg, s.hasFg = ansi16RGB(n-30), true
		case n >= 90 && n <= 97:
			s.fg, s.hasFg = ansi16RGB(n-90+8), true
		case n == 39:
			s.hasFg = false
		case n >= 40 && n <= 47:
			s.bg, s.hasBg = ansi16RGB(n-40), true
		case n >= 100 && n <= 107:
			s.bg, s.hasBg = ansi16RGB(n-100+8), true
		case n == 49:
			s.hasBg = false
		case n == 38 || n == 48:
			var c rgbColor
			ok := false
			if i+1 < len(parts) && parts[i+1] == "2" && i+4 < len(parts) {
				r, _ := strconv.Atoi(parts[i+2])
				g, _ := strconv.Atoi(parts[i+3])
				b, _ := strconv.Atoi(parts[i+4])
				c, ok = rgbColor{R: uint8(r), G: uint8(g), B: uint8(b)}, true
				i += 4
			} else if i+1 < len(parts) && parts[i+1] == "5" && i+2 < len(parts) {
				k, _ := strconv.Atoi(parts[i+2])
				c, ok = xterm256ToRGB(k), true
				i += 2
			}
			if ok && n == 38 {
				s.fg, s.hasFg = c, true
			} else if ok {
				s.bg, s.hasBg = c, true
			}
		}
	}
	return s
}

// render encodes the state for the active colour profile.
func (s sgrState) render() string {
	var parts []string
	if s.bold {
		parts = append(parts, "1")
	}
	if s.faint {
		parts = append(parts, "2")
	}
	if s.italic {
		parts = append(parts, "3")
	}
	if s.underline {
		parts = append(parts, "4")
	}
	if s.reverse {
		parts = append(parts, "7")
	}
	if s.strike {
		parts = append(parts, "9")
	}
	prof := theme.Profile()
	if prof != termenv.Ascii {
		if s.hasFg {
			if c := prof.Color(s.fg.Hex()); c != nil {
				parts = append(parts, c.Sequence(false))
			}
		}
		if s.hasBg {
			if c := prof.Color(s.bg.Hex()); c != nil {
				parts = append(parts, c.Sequence(true))
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(parts, ";") + "m"
}

// ── compatibility helpers (used by other overlays) ───────────────────────

func extractBgRGB(sgr string) (rgbColor, bool) { s := parseSGR(sgr); return s.bg, s.hasBg }
func extractFgRGB(sgr string) (rgbColor, bool) { s := parseSGR(sgr); return s.fg, s.hasFg }

func replaceBgRGB(sgr string, c rgbColor) string {
	s := parseSGR(sgr)
	s.bg, s.hasBg = c, true
	return s.render()
}

func replaceFgRGB(sgr string, c rgbColor) string {
	s := parseSGR(sgr)
	s.fg, s.hasFg = c, true
	return s.render()
}

func makeFgBgSGR(fg, bg rgbColor) string {
	return sgrState{fg: fg, bg: bg, hasFg: true, hasBg: true}.render()
}

func blendRGB(a, b rgbColor, alpha float64) rgbColor { return theme.Lerp(a, b, alpha) }

func dimRGB(c rgbColor, factor float64) rgbColor { return theme.Lerp(rgbColor{}, c, factor) }

// isPrintableASCII reports whether g is a single printable ASCII byte.
func isPrintableASCII(g string) bool {
	if len(g) != 1 {
		return false
	}
	b := g[0]
	return b >= 0x20 && b <= 0x7e
}

var ansi16Table = [16]rgbColor{
	{R: 0, G: 0, B: 0}, {R: 128, G: 0, B: 0}, {R: 0, G: 128, B: 0}, {R: 128, G: 128, B: 0},
	{R: 0, G: 0, B: 128}, {R: 128, G: 0, B: 128}, {R: 0, G: 128, B: 128}, {R: 192, G: 192, B: 192},
	{R: 128, G: 128, B: 128}, {R: 255, G: 0, B: 0}, {R: 0, G: 255, B: 0}, {R: 255, G: 255, B: 0},
	{R: 0, G: 0, B: 255}, {R: 255, G: 0, B: 255}, {R: 0, G: 255, B: 255}, {R: 255, G: 255, B: 255},
}

func ansi16RGB(n int) rgbColor { return ansi16Table[n&15] }

func xterm256ToRGB(n int) rgbColor {
	if n < 0 || n > 255 {
		return rgbColor{}
	}
	if n < 16 {
		return ansi16RGB(n)
	}
	if n < 232 {
		k := n - 16
		levels := [6]uint8{0, 95, 135, 175, 215, 255}
		return rgbColor{R: levels[k/36%6], G: levels[k/6%6], B: levels[k%6]}
	}
	v := uint8(8 + (n-232)*10)
	return rgbColor{R: v, G: v, B: v}
}
