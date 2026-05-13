package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Pseudo-transparent floating modal overlay (pickers, prompts).
//
// Recipe (illusion only — terminal can't do real alpha):
//   1. The modal RECTANGLE is identified at render time.
//   2. The base cells under the rectangle are dimmed + blue-tinted and
//      become the modal BODY background — so the editor's surface
//      visibly shows through.
//   3. Borders / title bar / input bar / selection bars are rendered
//      ON TOP of that dimmed body, with soft tints (no bright cyan).
//   4. Cells outside the rectangle are dimmed so the modal stands out
//      while the editor stays visible at reduced intensity.
//
// No noise, no solid blue panel, no real transparency, no drop shadow.
var (
	modalTint        = rgbColor{0x00, 0x01, 0x03} // essentially black, faintest blue hint
	bodyBlendAlpha   = 0.99                       // 99% tint, 1% underlying — matches menu/toast glass
	bodyFgDimFactor  = 0.18                       // dim editor text to 18% intensity
	glassBorderLight = rgbColor{0x3a, 0x3a, 0x3a} // uniform thin dark-grey rim
	glassBorderDark  = rgbColor{0x15, 0x19, 0x22} // bottom + right rim shadow (unused)
	glassShadow      = rgbColor{0x0b, 0x0d, 0x10} // drop shadow (unused)

	// Soft re-tints for the picker's strong VSCode-blue bars so the
	// modal feels cohesive instead of bright cyan over dim body.
	softTitleBg    = rgbColor{0x1f, 0x4f, 0x63}
	softSelectedBg = rgbColor{0x1f, 0x4f, 0x63}
	softInputBg    = rgbColor{0x1d, 0x26, 0x32}

	pickerPanelBg = rgbColor{0x26, 0x26, 0x26}
	pickerTitleBg = rgbColor{0x0e, 0x63, 0x9c}
	pickerSelBg   = rgbColor{0x09, 0x47, 0x71}
	pickerInputBg = rgbColor{0x1f, 0x34, 0x47}

	editorBgFallback    = rgbColor{0x1c, 0x1c, 0x1c}
	textPrimaryFallback = rgbColor{0xd0, 0xd0, 0xd0}

	defaultOutsideDim = 0.4 // outside cells: 40% intensity (= 60% transparent feel)
)

// ModalOpts lets callers tweak the modal recipe per-call. Pass nil to use
// the package defaults (the values tuned for the Settings modal: blue rim,
// 95% tint, 18% fg dim, 0.4 outside dim).
type ModalOpts struct {
	OutsideDim     float64
	BodyBlendAlpha float64
	BodyFgDim      float64
	BodyTint       rgbColor
	Rim            rgbColor
	SoftAccent     rgbColor
}

func modalOverlay(base, box string, screenW, screenH int, opts *ModalOpts) string {
	outsideDim := defaultOutsideDim
	blendAlpha := bodyBlendAlpha
	fgDim := bodyFgDimFactor
	tint := modalTint
	rim := glassBorderLight
	accent := softTitleBg
	if opts != nil {
		if opts.OutsideDim != 0 {
			outsideDim = opts.OutsideDim
		}
		if opts.BodyBlendAlpha != 0 {
			blendAlpha = opts.BodyBlendAlpha
		}
		if opts.BodyFgDim != 0 {
			fgDim = opts.BodyFgDim
		}
		if opts.BodyTint != (rgbColor{}) {
			tint = opts.BodyTint
		}
		if opts.Rim != (rgbColor{}) {
			rim = opts.Rim
		}
		if opts.SoftAccent != (rgbColor{}) {
			accent = opts.SoftAccent
		}
	}

	baseLines := strings.Split(base, "\n")
	boxLines := strings.Split(strings.TrimRight(box, "\n"), "\n")
	if len(boxLines) == 0 {
		return base
	}
	boxH := len(boxLines)
	boxW := lipgloss.Width(boxLines[0])
	if boxW == 0 {
		return base
	}
	leftCol := (screenW - boxW) / 2
	topRow := (screenH - boxH) / 2
	if leftCol < 0 {
		leftCol = 0
	}
	if topRow < 0 {
		topRow = 0
	}

	dimOutsideRect(baseLines, leftCol, topRow, boxW, boxH, outsideDim)

	for i := range boxLines {
		baseRowIdx := topRow + i
		var baseCells []cellData
		if baseRowIdx >= 0 && baseRowIdx < len(baseLines) {
			baseCells = parseANSIRow(baseLines[baseRowIdx])
		}
		boxLines[i] = styleGlassRow(boxLines[i], baseCells, leftCol, i, len(boxLines), blendAlpha, fgDim, tint, rim, accent)
	}

	for i, line := range boxLines {
		row := topRow + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		baseLines[row] = spliceAt(baseLines[row], line, leftCol)
	}
	return strings.Join(baseLines, "\n")
}

// dimOutsideRect multiplies fg+bg of every cell outside the modal rectangle
// by `factor` (0..1). factor=0.4 keeps 40% of the original intensity, i.e.
// the user-requested "60% transparent" feel for the surrounding chrome.
func dimOutsideRect(lines []string, leftCol, topRow, boxW, boxH int, factor float64) {
	rectStartRow := topRow
	rectEndRow := topRow + boxH // exclusive
	rectStartCol := leftCol
	rectEndCol := leftCol + boxW
	for i := range lines {
		if i < rectStartRow || i >= rectEndRow {
			lines[i] = dimRowFully(lines[i], factor)
		} else {
			lines[i] = dimRowOutsideCols(lines[i], rectStartCol, rectEndCol, factor)
		}
	}
}

func dimRowFully(row string, factor float64) string {
	cells := parseANSIRow(row)
	for i := range cells {
		dimCellInPlace(&cells[i], factor)
	}
	return renderCells(cells)
}

func dimRowOutsideCols(row string, startCol, endCol int, factor float64) string {
	cells := parseANSIRow(row)
	for i := range cells {
		if i >= startCol && i < endCol {
			continue
		}
		dimCellInPlace(&cells[i], factor)
	}
	return renderCells(cells)
}

func dimCellInPlace(c *cellData, factor float64) {
	if bg, ok := extractBgRGB(c.sgr); ok {
		c.sgr = replaceBgRGB(c.sgr, dimRGB(bg, factor))
	}
	if fg, ok := extractFgRGB(c.sgr); ok {
		c.sgr = replaceFgRGB(c.sgr, dimRGB(fg, factor))
	}
}

// styleGlassRow rebuilds one row of the modal output. Border/title/input/
// selection cells get soft re-tints; everything else is replaced with a
// dimmed+tinted version of the underlying base cell so the editor shows
// through the modal body.
func styleGlassRow(row string, baseCells []cellData, leftCol, rowIdx, totalRows int, blendAlpha, fgDim float64, tint, rim, accent rgbColor) string {
	cells := parseANSIRow(row)
	if len(cells) == 0 {
		return row
	}
	n := len(cells)
	isTopRow := rowIdx == 0
	isBottomRow := rowIdx == totalRows-1

	for i := range cells {
		c := cells[i]
		isLeftCol := i == 0
		isRightCol := i == n-1
		isBorder := isTopRow || isBottomRow || isLeftCol || isRightCol

		var underlying cellData
		bi := leftCol + i
		if bi >= 0 && bi < len(baseCells) {
			underlying = baseCells[bi]
		}
		dimBg, dimFg := dimBaseColors(underlying, blendAlpha, fgDim, tint)

		if isBorder && c.glyph != " " && c.glyph != "" {
			// Picker's RoundedBorder gives the curved corners (╭ ╮ ╰ ╯).
			c.sgr = replaceFgRGB(c.sgr, rim)
			c.sgr = replaceBgRGB(c.sgr, dimBg)
			cells[i] = c
			continue
		}

		// Track whether this cell's bg is the standard panel bg. Only
		// panel-bg (or unset) cells get the dim+tint body replacement
		// below — cells with a deliberately-set bg (e.g. a footer red,
		// a section accent) pass through untouched so callers can paint
		// regions of the modal that opt out of the glass body look.
		isPanelOrUnset := true
		if bg, ok := extractBgRGB(c.sgr); ok {
			switch bg {
			case pickerTitleBg:
				c.sgr = replaceBgRGB(c.sgr, accent)
				cells[i] = c
				continue
			case pickerSelBg:
				c.sgr = replaceBgRGB(c.sgr, accent)
				cells[i] = c
				continue
			case pickerInputBg:
				c.sgr = replaceBgRGB(c.sgr, softInputBg)
				cells[i] = c
				continue
			case pickerPanelBg:
				// fall through into body replacement
			default:
				isPanelOrUnset = false
			}
		}

		if !isPanelOrUnset {
			cells[i] = c
			continue
		}

		if c.glyph == " " || c.glyph == "" {
			// Show the underlying glyph dimmed ONLY if it's printable
			// single-byte ASCII (regular code text). Multi-byte glyphs —
			// emoji, Nerd Font icons, box-drawing — get masked with a
			// space because they don't reliably obey ANSI dim/fg and
			// would otherwise pop bright through the modal body.
			if isPrintableASCII(underlying.glyph) {
				cells[i] = cellData{
					sgr:   makeFgBgSGR(dimFg, dimBg),
					glyph: underlying.glyph,
				}
			} else {
				cells[i] = cellData{
					sgr:   makeFgBgSGR(rgbColor{}, dimBg),
					glyph: " ",
				}
			}
		} else {
			c.sgr = replaceBgRGB(c.sgr, dimBg)
			cells[i] = c
		}
	}
	return renderCells(cells)
}

// dimBaseColors returns the bg/fg the modal should paint behind a given
// underlying cell: bg pulled toward `tint` at `blendAlpha`, fg reduced to
// `fgDim` of original intensity. Falls back to editor defaults when the
// underlying cell has no styling.
func dimBaseColors(c cellData, blendAlpha, fgDim float64, tint rgbColor) (bg, fg rgbColor) {
	baseBg, bgOK := extractBgRGB(c.sgr)
	baseFg, fgOK := extractFgRGB(c.sgr)
	if !bgOK {
		baseBg = editorBgFallback
	}
	if !fgOK {
		baseFg = textPrimaryFallback
	}
	bg = blendRGB(baseBg, tint, blendAlpha)
	fg = dimRGB(baseFg, fgDim)
	return
}

func blendRGB(a, b rgbColor, alpha float64) rgbColor {
	return rgbColor{
		r: int(float64(a.r)*(1-alpha) + float64(b.r)*alpha),
		g: int(float64(a.g)*(1-alpha) + float64(b.g)*alpha),
		b: int(float64(a.b)*(1-alpha) + float64(b.b)*alpha),
	}
}

func dimRGB(c rgbColor, factor float64) rgbColor {
	return rgbColor{
		r: int(float64(c.r) * factor),
		g: int(float64(c.g) * factor),
		b: int(float64(c.b) * factor),
	}
}

func makeFgBgSGR(fg, bg rgbColor) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm",
		fg.r, fg.g, fg.b, bg.r, bg.g, bg.b)
}

// isPrintableASCII reports whether g is a single printable ASCII byte
// (space through ~). Used to decide which underlying glyphs are safe to
// show dimmed inside the modal body — multi-byte / wide / emoji glyphs
// don't reliably honour ANSI dim and would pop bright through the body.
func isPrintableASCII(g string) bool {
	if len(g) != 1 {
		return false
	}
	b := g[0]
	return b >= 0x20 && b <= 0x7e
}

// addDropShadow paints a one-cell-offset L-shape of dark cells just below
// and right of the modal. Currently unused — kept for callers that want to
// re-introduce a floating-panel look.
func addDropShadow(lines []string, leftCol, topRow, boxW, boxH int) {
	if r := topRow + boxH; r >= 0 && r < len(lines) {
		lines[r] = paintCellsBg(lines[r], leftCol+1, leftCol+boxW+1, glassShadow)
	}
	col := leftCol + boxW
	for r := topRow + 1; r <= topRow+boxH && r >= 0 && r < len(lines); r++ {
		lines[r] = paintCellsBg(lines[r], col, col+1, glassShadow)
	}
}

func paintCellsBg(row string, startCol, endCol int, bg rgbColor) string {
	cells := parseANSIRow(row)
	for len(cells) < endCol {
		cells = append(cells, cellData{glyph: " "})
	}
	for i := startCol; i < endCol && i < len(cells); i++ {
		if i < 0 {
			continue
		}
		cells[i].sgr = replaceBgRGB(cells[i].sgr, bg)
		if cells[i].glyph == "" {
			cells[i].glyph = " "
		}
	}
	return renderCells(cells)
}

// ── helpers: cell parser, RGB extract/replace ─────────────────────────────

type rgbColor struct{ r, g, b int }

type cellData struct {
	sgr   string
	glyph string
}

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
			if sgr == "\x1b[0m" || sgr == "\x1b[m" {
				current = ""
			} else {
				current = sgr
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

func extractBgRGB(sgr string) (rgbColor, bool) { return extractColor(sgr, "48") }
func extractFgRGB(sgr string) (rgbColor, bool) { return extractColor(sgr, "38") }

func extractColor(sgr, kind string) (rgbColor, bool) {
	if !strings.HasPrefix(sgr, "\x1b[") || !strings.HasSuffix(sgr, "m") {
		return rgbColor{}, false
	}
	body := sgr[2 : len(sgr)-1]
	if body == "" {
		return rgbColor{}, false
	}
	parts := strings.Split(body, ";")
	for i := 0; i < len(parts); i++ {
		if parts[i] == kind && i+1 < len(parts) {
			switch parts[i+1] {
			case "2":
				if i+4 < len(parts) {
					r, e1 := strconv.Atoi(parts[i+2])
					g, e2 := strconv.Atoi(parts[i+3])
					b, e3 := strconv.Atoi(parts[i+4])
					if e1 == nil && e2 == nil && e3 == nil {
						return rgbColor{r, g, b}, true
					}
				}
			case "5":
				if i+2 < len(parts) {
					n, err := strconv.Atoi(parts[i+2])
					if err == nil {
						return xterm256ToRGB(n), true
					}
				}
			}
		}
	}
	return rgbColor{}, false
}

func replaceBgRGB(sgr string, c rgbColor) string { return replaceColor(sgr, "48", c) }
func replaceFgRGB(sgr string, c rgbColor) string { return replaceColor(sgr, "38", c) }

func replaceColor(sgr, kind string, c rgbColor) string {
	if sgr == "" {
		return fmt.Sprintf("\x1b[%s;2;%d;%d;%dm", kind, c.r, c.g, c.b)
	}
	if !strings.HasPrefix(sgr, "\x1b[") || !strings.HasSuffix(sgr, "m") {
		return sgr
	}
	body := sgr[2 : len(sgr)-1]
	parts := strings.Split(body, ";")
	out := make([]string, 0, len(parts)+2)
	i := 0
	replaced := false
	for i < len(parts) {
		if parts[i] == kind && i+1 < len(parts) {
			if parts[i+1] == "2" && i+4 < len(parts) {
				out = append(out, kind, "2",
					strconv.Itoa(c.r), strconv.Itoa(c.g), strconv.Itoa(c.b))
				i += 5
				replaced = true
				continue
			}
			if parts[i+1] == "5" && i+2 < len(parts) {
				out = append(out, kind, "2",
					strconv.Itoa(c.r), strconv.Itoa(c.g), strconv.Itoa(c.b))
				i += 3
				replaced = true
				continue
			}
		}
		out = append(out, parts[i])
		i++
	}
	if !replaced {
		out = append(out, kind, "2",
			strconv.Itoa(c.r), strconv.Itoa(c.g), strconv.Itoa(c.b))
	}
	return "\x1b[" + strings.Join(out, ";") + "m"
}

func xterm256ToRGB(n int) rgbColor {
	if n < 0 || n > 255 {
		return rgbColor{}
	}
	if n < 16 {
		base := [16][3]int{
			{0, 0, 0}, {128, 0, 0}, {0, 128, 0}, {128, 128, 0},
			{0, 0, 128}, {128, 0, 128}, {0, 128, 128}, {192, 192, 192},
			{128, 128, 128}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
			{0, 0, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
		}
		return rgbColor{base[n][0], base[n][1], base[n][2]}
	}
	if n < 232 {
		k := n - 16
		levels := [6]int{0, 95, 135, 175, 215, 255}
		return rgbColor{levels[k/36%6], levels[k/6%6], levels[k%6]}
	}
	v := 8 + (n-232)*10
	return rgbColor{v, v, v}
}
