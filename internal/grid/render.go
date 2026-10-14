package grid

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// sgrReset ends every styled span lipgloss (termenv) writes.
const sgrReset = "\x1b[0m"

// maxStyles bounds the style cache. A screen uses a few dozen styles; a
// cache this big only fills when highlights churn, and then it starts over.
const maxStyles = 4096

// styleKey is everything a run's escape codes depend on, after the hl id
// is resolved (defaults, terminal palette, reverse, cursor). Keying on the
// resolved values, not the hl id, means the cache never goes stale: a new
// hl_attr_define, default_colors_set or terminal palette just gives
// different keys. The colour profile is handled by dropping the cache when
// it changes (see Render).
type styleKey struct {
	fg, bg int
	flags  uint8
}

const (
	flagBold uint8 = 1 << iota
	flagItalic
	flagUnderline
	flagStrikethrough
)

// runStyle is what lipgloss would write around text in one style, worked
// out once by rendering probe strings.
type runStyle struct {
	style lipgloss.Style
	// pre / spacePre open the style for text / for whitespace runes; ""
	// means the text is written bare (no escape codes, no reset).
	pre, spacePre string
	// perRune: lipgloss styles each rune on its own (underline and
	// strikethrough), whitespace with spacePre.
	perRune bool
	// slow: the probes did not have the expected shape; always call
	// lipgloss.
	slow bool
}

// Render returns the grid as a styled string suitable for use in a
// Bubble Tea View. The cursor cell (CursorRow, CursorCol) is drawn in
// reverse video.
//
// The output is byte-for-byte what rendering each run with its own
// lipgloss style gives, but the escape codes of each style are worked out
// once and cached, so a frame costs a few allocations instead of several
// per run.
//
// When nothing changed since the last call (no Apply that draws, same
// cursor, same colour profile) the last frame is returned as is.
func (g *Grid) Render() string {
	if g.Width == 0 || g.Height == 0 {
		return ""
	}
	p := lipgloss.ColorProfile()
	f := &g.frame
	if f.ok && f.gen == g.gen && f.profile == p && f.curR == g.CursorRow && f.curC == g.CursorCol {
		return f.out
	}
	if g.styles == nil || p != g.stylesProfile || len(g.styles) > maxStyles {
		g.styles = make(map[styleKey]*runStyle)
		g.stylesProfile = p
	}
	out := g.render()
	*f = frameCache{ok: true, gen: g.gen, profile: p, curR: g.CursorRow, curC: g.CursorCol, out: out}
	return out
}

func (g *Grid) render() string {
	var sb strings.Builder
	sb.Grow(g.lastRenderLen)
	for r := 0; r < g.Height; r++ {
		row := g.Cells[r]
		c := 0
		for c < g.Width {
			if r == g.CursorRow && c == g.CursorCol {
				g.writeRun(&sb, row[c:c+1], true)
				c++
				continue
			}
			j := c + 1
			for j < g.Width &&
				row[j].HlID == row[c].HlID &&
				!(r == g.CursorRow && j == g.CursorCol) {
				j++
			}
			g.writeRun(&sb, row[c:j], false)
			c = j
		}
		if r < g.Height-1 {
			sb.WriteByte('\n')
		}
	}
	g.lastRenderLen = sb.Len()
	return sb.String()
}

// writeRun writes cells (all of one hl id) in their style.
func (g *Grid) writeRun(sb *strings.Builder, cells []Cell, cursor bool) {
	if cursor && cells[0].Cont {
		return // right half of a wide glyph: already drawn
	}
	st := g.runStyle(cells[0].HlID, cursor)
	if st.slow || needsLipgloss(cells) {
		var text strings.Builder
		for _, cell := range cells {
			if !cell.Cont {
				text.WriteRune(cellRune(cell))
			}
		}
		sb.WriteString(st.style.Render(text.String()))
		return
	}
	if st.perRune {
		for _, cell := range cells {
			if cell.Cont {
				continue
			}
			rn := cellRune(cell)
			pre := st.pre
			if unicode.IsSpace(rn) {
				pre = st.spacePre
			}
			writeStyled(sb, pre, rn)
		}
		return
	}
	sb.WriteString(st.pre)
	for _, cell := range cells {
		if !cell.Cont {
			sb.WriteRune(cellRune(cell))
		}
	}
	if st.pre != "" {
		sb.WriteString(sgrReset)
	}
}

func writeStyled(sb *strings.Builder, pre string, rn rune) {
	sb.WriteString(pre)
	sb.WriteRune(rn)
	if pre != "" {
		sb.WriteString(sgrReset)
	}
}

// cellRune is the rune a cell draws. Invalid runes become U+FFFD here,
// as strings.Builder.WriteRune would make them.
func cellRune(cell Cell) rune {
	rn := cell.Rune
	if rn == 0 {
		return ' '
	}
	if !utf8.ValidRune(rn) {
		return utf8.RuneError
	}
	return rn
}

// needsLipgloss reports runes lipgloss rewrites (tabs, line breaks); such
// a run goes through lipgloss itself.
func needsLipgloss(cells []Cell) bool {
	for _, cell := range cells {
		if !cell.Cont && (cell.Rune == '\t' || cell.Rune == '\r' || cell.Rune == '\n') {
			return true
		}
	}
	return false
}

// runStyle returns the cached style for hl id hlID (cursor: the cursor
// cell's swapped colours).
func (g *Grid) runStyle(hlID int, cursor bool) *runStyle {
	k := g.resolve(hlID, cursor)
	if st, ok := g.styles[k]; ok {
		return st
	}
	st := newRunStyle(k)
	g.styles[k] = st
	return st
}

// resolve works out the colours and flags hl id hlID draws with.
func (g *Grid) resolve(hlID int, cursor bool) styleKey {
	a, ok := g.HlAttrs[hlID]
	if !ok {
		a = g.HlAttrs[0]
	}
	fg := a.Fg
	if a.TermFg > 0 && g.TermPaletteSet {
		fg = g.TermPalette[a.TermFg-1]
	}
	if fg < 0 {
		fg = g.DefaultFg
	}
	bg := a.Bg
	if a.TermBg > 0 && g.TermPaletteSet {
		bg = g.TermPalette[a.TermBg-1]
	}
	if bg < 0 {
		bg = g.DefaultBg
	}
	if a.Reverse {
		fg, bg = bg, fg
	}
	if cursor {
		fg, bg = bg, fg
	}
	// Only "set or not" and the low 24 bits reach the output.
	if fg < 0 {
		fg = -1
	} else {
		fg &= 0xffffff
	}
	if bg < 0 {
		bg = -1
	} else {
		bg &= 0xffffff
	}
	k := styleKey{fg: fg, bg: bg}
	if a.Bold {
		k.flags |= flagBold
	}
	if a.Italic {
		k.flags |= flagItalic
	}
	if a.Underline {
		k.flags |= flagUnderline
	}
	if a.Strikethrough {
		k.flags |= flagStrikethrough
	}
	return k
}

func (k styleKey) style() lipgloss.Style {
	s := lipgloss.NewStyle()
	if k.fg >= 0 {
		s = s.Foreground(rgbColor(k.fg))
	}
	if k.bg >= 0 {
		s = s.Background(rgbColor(k.bg))
	}
	if k.flags&flagBold != 0 {
		s = s.Bold(true)
	}
	if k.flags&flagItalic != 0 {
		s = s.Italic(true)
	}
	if k.flags&flagUnderline != 0 {
		s = s.Underline(true)
	}
	if k.flags&flagStrikethrough != 0 {
		s = s.Strikethrough(true)
	}
	return s
}

// newRunStyle renders probe strings with the lipgloss style for k and
// keeps what lipgloss wrote around them.
func newRunStyle(k styleKey) *runStyle {
	s := k.style()
	st := &runStyle{
		style:   s,
		perRune: k.flags&(flagUnderline|flagStrikethrough) != 0,
	}
	var ok1, ok2 bool
	st.pre, ok1 = probePrefix(s, "x")
	st.spacePre, ok2 = probePrefix(s, " ")
	if !st.perRune && st.spacePre != st.pre {
		ok2 = false // whole-run styling must not treat spaces apart
	}
	st.slow = !ok1 || !ok2
	return st
}

// probePrefix renders probe and returns what lipgloss put before it,
// checking the output is exactly prefix + probe (+ reset if prefixed).
func probePrefix(s lipgloss.Style, probe string) (string, bool) {
	out := s.Render(probe)
	if out == probe {
		return "", true
	}
	body := strings.TrimSuffix(out, probe+sgrReset)
	if body == out || body == "" {
		return "", false
	}
	return body, true
}

func rgbColor(rgb int) lipgloss.Color {
	return lipgloss.Color(fmt.Sprintf("#%06x", rgb&0xffffff))
}

// styleCache is the per-grid state behind Render.
type styleCache struct {
	styles        map[styleKey]*runStyle
	stylesProfile termenv.Profile
	lastRenderLen int

	// gen counts changes to what the grid draws (see Invalidate). The last
	// frame is reused while gen, the cursor and the colour profile match.
	gen   uint64
	frame frameCache
}

// frameCache is the last frame Render built and what it was built from.
type frameCache struct {
	ok         bool
	gen        uint64
	profile    termenv.Profile
	curR, curC int
	out        string
}

// Invalidate drops the cached frame, so the next Render draws again.
// Apply and SetTermPalette call it; code that writes Cells, HlAttrs or
// the default colours directly must call it too.
func (g *Grid) Invalidate() { g.gen++ }

// SetTermPalette sets the RGB of ANSI colours 0–15 for terminal cells
// (see Attrs.TermFg).
func (g *Grid) SetTermPalette(p [16]int) {
	g.TermPalette = p
	g.TermPaletteSet = true
	g.Invalidate()
}
