package confirm

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Style constants for buttons.
const (
	StylePrimary     = "primary"
	StyleDestructive = "destructive"
	StyleDefault     = ""
)

type Button struct {
	ID    string
	Title string
	Style string
}

type SelectMsg struct{ ID string }
type CloseMsg struct{}

// rect is a screen-space rectangle (top-left x,y plus width/height) used
// for hit-testing mouse clicks against rendered UI elements.
type rect struct{ x, y, w, h int }

type Model struct {
	scrollText bool
	wideText   bool // the card grows (up to 110 cells) to fit the longest line
	scroll     int
	title      string
	message    string
	buttons    []Button
	cursor     int
	w, h       int
	code       string // a command shown on one line in a frame (WithCode)
	note       string // text below the code
}

func New(title, message string, buttons []Button) Model {
	return Model{title: title, message: message, buttons: buttons}
}

// Buttons returns a copy of the card's buttons, in order.
func (m Model) Buttons() []Button { return append([]Button(nil), m.buttons...) }

// WithCode shows code (a shell command) below the message on ONE line in
// a frame, cut in the middle with … when the dialog is too narrow, and
// note below it. The dialog grows wider (up to the window) to fit it.
func (m Model) WithCode(code, note string) Model {
	m.code, m.note = code, note
	return m
}

// WithScrollText keeps the entire message available with Up/Down.
func (m Model) WithScrollText() Model { m.scrollText = true; return m }

// WithWideText lets the card grow, up to 110 cells and the window, so
// lines such as SQL statements are not cut in the middle of a word.
func (m Model) WithWideText() Model { m.wideText = true; return m }

func (m Model) fullTextLines() []string {
	width := max(1, min(60, m.w-8)-8)
	if m.wideText {
		width = max(1, m.boxWidth()-6)
	}
	var out []string
	for _, line := range strings.Split(m.message, "\n") {
		part := ""
		cells := 0
		for _, r := range line {
			n := lipgloss.Width(string(r))
			if cells+n > width && part != "" {
				if m.wideText && r == ' ' {
					out = append(out, part) // the space ends the line
					part, cells = "", 0
					continue
				}
				if cut := strings.LastIndexByte(part, ' '); m.wideText && cut > 0 {
					// A wide card breaks at the last space, not inside a word.
					out = append(out, part[:cut])
					part = part[cut+1:]
					cells = lipgloss.Width(part)
				} else {
					out = append(out, part)
					part = ""
					cells = 0
				}
			}
			part += string(r)
			cells += n
		}
		out = append(out, part)
	}
	return out
}

// truncMiddle cuts s to w cells, keeping its start and end around a ….
func truncMiddle(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	r := []rune(s)
	keep := w - 1
	head := (keep + 1) / 2
	// Cut by cells, not runes (wide characters count twice).
	h, hw := 0, 0
	for h < len(r) && hw+lipgloss.Width(string(r[h])) <= head {
		hw += lipgloss.Width(string(r[h]))
		h++
	}
	t, tw := len(r), 0
	for t > h && tw+lipgloss.Width(string(r[t-1])) <= keep-hw {
		tw += lipgloss.Width(string(r[t-1]))
		t--
	}
	return string(r[:h]) + "…" + string(r[t:])
}

// Init satisfies the bubbletea.Model interface — confirm has no startup
// commands of its own.
func (m Model) Init() tea.Cmd { return nil }

func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if m.scrollText {
		switch k.Type {
		case tea.KeyDown:
			m.scroll = min(m.scroll+1, max(0, len(m.fullTextLines())-max(1, min(12, m.h-14))))
			return m, nil
		case tea.KeyUp:
			m.scroll = max(m.scroll-1, 0)
			return m, nil
		case tea.KeyPgDown:
			m.scroll = min(m.scroll+8, max(0, len(m.fullTextLines())-max(1, min(12, m.h-14))))
			return m, nil
		case tea.KeyPgUp:
			m.scroll = max(m.scroll-8, 0)
			return m, nil
		}
	}
	switch k.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		return m, func() tea.Msg { return CloseMsg{} }
	case tea.KeyEnter:
		if m.cursor >= 0 && m.cursor < len(m.buttons) {
			id := m.buttons[m.cursor].ID
			return m, func() tea.Msg { return SelectMsg{ID: id} }
		}
	case tea.KeyLeft:
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyRight, tea.KeyTab:
		if m.cursor < len(m.buttons)-1 {
			m.cursor++
		}
	}
	return m, nil
}

// View renders the confirm modal centered on a dim backdrop. The
// production call path (app.View) wraps Box() with modalOverlay so the
// editor shows through; View() exists for tests and any caller that
// wants a standalone framed render.
func (m Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}

	box := m.Box()
	if box == "" {
		return ""
	}
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(theme.LG(theme.BgEditor)),
	)
}

// Bounds returns the absolute screen rectangle of the panel: x, y,
// width, height. Computed fresh from the current model state — does
// NOT depend on a prior Box() / View() call.
func (m Model) Bounds() (x, y, w, h int) {
	if m.w <= 0 || m.h <= 0 {
		return 0, 0, 0, 0
	}
	r, _ := m.computeRects()
	return r.x, r.y, r.w, r.h
}

// HandleMouse maps an absolute screen-space mouse event to confirm
// semantics:
//   - Left-click on a button → emit SelectMsg for that button (also
//     moves the keyboard cursor onto it).
//   - Left-click anywhere else INSIDE the panel → no-op (don't dismiss
//     destructive prompts on a stray click).
//   - Left-click OUTSIDE the panel → no-op (same — destructive
//     dialogs require explicit Cancel/Esc).
//   - Wheel events → ignored.
//
// The third return value reports whether the event landed inside the
// panel rect; the caller can use it to decide whether to swallow
// clicks outside the panel.
func (m Model) HandleMouse(x, y int, action tea.MouseAction, button tea.MouseButton) (Model, tea.Cmd, bool) {
	if m.w <= 0 || m.h <= 0 {
		return m, nil, false
	}
	boxRect, buttonRects := m.computeRects()
	inside := x >= boxRect.x && x < boxRect.x+boxRect.w &&
		y >= boxRect.y && y < boxRect.y+boxRect.h

	// Hover: move the highlight onto the button under the pointer.
	if action == tea.MouseActionMotion {
		for i, br := range buttonRects {
			if x >= br.x && x < br.x+br.w && y == br.y {
				m.cursor = i
				break
			}
		}
		return m, nil, inside
	}

	if button != tea.MouseButtonLeft || action != tea.MouseActionPress {
		return m, nil, inside
	}

	// The footer hints are buttons too: Enter picks the highlighted
	// button, Esc cancels.
	if inside && y == boxRect.y+boxRect.h-2 {
		rel := x - (boxRect.x + 1 + 2)
		for _, sp := range theme.HintSpans(confirmHints...) {
			if rel < sp.X0 || rel >= sp.X1 {
				continue
			}
			switch sp.Key {
			case "enter":
				if m.cursor >= 0 && m.cursor < len(m.buttons) {
					id := m.buttons[m.cursor].ID
					return m, func() tea.Msg { return SelectMsg{ID: id} }, true
				}
			case "esc":
				return m, func() tea.Msg { return CloseMsg{} }, true
			}
		}
	}

	// Hit-test each button rect (1-row tall).
	for i, br := range buttonRects {
		if x >= br.x && x < br.x+br.w && y == br.y {
			m.cursor = i
			id := m.buttons[i].ID
			return m, func() tea.Msg { return SelectMsg{ID: id} }, true
		}
	}

	// Inside the panel but not on a button → swallow click silently
	// (destructive: no implicit dismiss).
	return m, nil, inside
}

var confirmHints = []string{"←→", "move", "enter", "select", "esc", "cancel"}

// Palette mirrors the Settings modal so confirm shares the same visual
// idiom — same panel bg, dim rim, hairline separators, soft selection
// bar tint.
var (
	cmBoxBg         lipgloss.Color
	cmBorderRim     lipgloss.Color
	cmIconWarn      lipgloss.Color
	cmTextPri       lipgloss.Color
	cmTextBody      lipgloss.Color
	cmTextDim       lipgloss.Color
	cmGuideColor    lipgloss.Color
	cmSubtitleFg    lipgloss.Color
	cmSelBg         lipgloss.Color
	cmSelFg         lipgloss.Color
	cmPrimaryBg     lipgloss.Color
	cmDestructiveBg lipgloss.Color
)

func init() { refreshColors() }

// refreshColors derives this file's colours from the active theme. It runs
// before every render so a theme switch applies immediately.
func refreshColors() {
	cmBoxBg = theme.Float().Bg
	cmBorderRim = theme.Float().Border
	cmIconWarn = theme.LG(theme.DiagWarning) // amber for the ⚠ glyph
	cmTextPri = theme.Float().Title
	cmTextBody = theme.Float().Text
	cmTextDim = theme.Float().Dim
	cmGuideColor = theme.Float().Hairline
	cmSubtitleFg = theme.Float().Dim
	cmSelBg = theme.Float().SelBg // soft selection bar (matches settings)
	cmSelFg = theme.Float().SelFg
	cmPrimaryBg = theme.Float().Primary
	cmDestructiveBg = theme.Float().DangerBg
}

// confirmDims captures every measurement Box() needs in one place, so
// that Box() and HandleMouse() can both compute the same layout from
// the same source of truth (mirrors picker.layoutDims()).
type confirmDims struct {
	boxW         int
	bodyLines    []string
	footerSpacer int
	buttonLabels []string
	buttonXs     []int // inner-x offset of each button (relative to inner area)
	codeAt       int   // index in bodyLines of the code frame's top row; -1 without code
}

// layoutDims is the single source of truth for confirm panel geometry.
// Both Box() and HandleMouse() consult it so the rendered button
// positions and the hit-test rects line up exactly.
func (m Model) layoutDims() confirmDims {
	boxW := m.boxWidth()
	innerW := boxW - 2
	bodyW := innerW - 4
	if bodyW < 1 {
		bodyW = 1
	}
	bodyLines := wrap(m.message, bodyW)
	if m.scrollText {
		bodyLines = m.fullTextLines()
	}
	return m.layoutRest(boxW, innerW, bodyW, bodyLines)
}

// boxWidth is the card's width: 60 cells, wider for a code line, the
// buttons or (WithWideText) the longest message line, never past the
// window.
func (m Model) boxWidth() int {
	boxW := 60
	if m.wideText {
		longest := 0
		for _, l := range strings.Split(m.message, "\n") {
			longest = max(longest, lipgloss.Width(l))
		}
		boxW = max(boxW, min(longest+6, 110))
	}
	if m.code != "" {
		// Room for the command on one line: frame + padding + border.
		boxW = max(boxW, min(lipgloss.Width(m.code)+10, 110))
	}
	// Room for the button row on one line (four buttons need more than 60).
	btnW := 2 * max(len(m.buttons)-1, 0)
	for _, b := range m.buttons {
		btnW += lipgloss.Width(m.buttonLabel(b))
	}
	boxW = max(boxW, min(btnW+4, 110))
	if maxW := m.w - 8; boxW > maxW {
		boxW = maxW
	}
	if boxW < 36 {
		boxW = 36
	}
	return boxW
}

func (m Model) layoutRest(boxW, innerW, bodyW int, bodyLines []string) confirmDims {

	// Cap body to a sensible max so the modal doesn't fill the screen
	// when given a huge message; tail is replaced with an ellipsis.
	const maxBodyLines = 12
	if m.scrollText {
		capacity := max(1, min(maxBodyLines, m.h-14))
		if len(bodyLines) > capacity {
			start := min(m.scroll, len(bodyLines)-capacity)
			end := min(start+capacity, len(bodyLines))
			bodyLines = append(append([]string(nil), bodyLines[start:end]...), "Up/Down scroll · "+strconv.Itoa(start+1)+"-"+strconv.Itoa(end))
		}
	} else if len(bodyLines) > maxBodyLines {
		extra := len(bodyLines) - maxBodyLines + 1
		bodyLines = bodyLines[:maxBodyLines-1]
		bodyLines = append(bodyLines, "…and "+strconv.Itoa(extra)+" more lines")
	}
	codeAt := -1
	if m.code != "" {
		code := truncMiddle(m.code, max(bodyW-4, 1))
		cw := lipgloss.Width(code)
		codeAt = len(bodyLines)
		bodyLines = append(bodyLines,
			"╭"+strings.Repeat("─", cw+2)+"╮",
			"│ "+code+" │",
			"╰"+strings.Repeat("─", cw+2)+"╯")
		if m.note != "" {
			bodyLines = append(bodyLines, wrap(m.note, bodyW)...)
		}
	}

	// Button labels and their x-offsets (panel-bg gap of 2 between
	// adjacent buttons; whole row centered inside innerW).
	labels := make([]string, len(m.buttons))
	totalLabelW := 0
	for i, b := range m.buttons {
		labels[i] = m.buttonLabel(b)
		totalLabelW += lipgloss.Width(labels[i])
	}
	const gap = 2
	if len(m.buttons) > 1 {
		totalLabelW += gap * (len(m.buttons) - 1)
	}
	leftPad := 0
	if totalLabelW < innerW {
		leftPad = (innerW - totalLabelW) / 2
	}
	xs := make([]int, len(m.buttons))
	cur := leftPad
	for i, lbl := range labels {
		xs[i] = cur
		cur += lipgloss.Width(lbl) + gap
	}

	// Footer spacer (3..12) — same shape as settings_modal.Box().
	// Content rows so far: 1 header + 1 hairline + 1 blank + body + 1 blank + 1 buttons.
	// The card is sized to its content: one blank row before the footer.
	footerSpacer := 1

	return confirmDims{
		boxW:         boxW,
		bodyLines:    bodyLines,
		footerSpacer: footerSpacer,
		buttonLabels: labels,
		buttonXs:     xs,
		codeAt:       codeAt,
	}
}

// computeRects derives the absolute-screen rectangle of the panel and
// each button from layoutDims(). Call paths that need to hit-test or
// position cells (Box, HandleMouse, Bounds) all go through this.
func (m Model) computeRects() (boxRect rect, buttonRects []rect) {
	dims := m.layoutDims()

	// Total rendered rows = top border + (header + hairline + blank +
	// body + blank + buttons + footerSpacer + hairline + footer) + bottom border.
	contentRows := 1 + 1 + 1 + len(dims.bodyLines) + 1 + 1 + dims.footerSpacer + 1 + 1
	totalRows := contentRows + 2

	boxLeft := (m.w - dims.boxW) / 2
	if boxLeft < 0 {
		boxLeft = 0
	}
	boxTop := (m.h - totalRows) / 2
	if boxTop < 0 {
		boxTop = 0
	}
	boxRect = rect{x: boxLeft, y: boxTop, w: dims.boxW, h: totalRows}

	// Button row sits at: top-border + header + hairline + blank + body + blank.
	buttonRowY := boxTop + 1 + 1 + 1 + 1 + len(dims.bodyLines) + 1
	innerLeft := boxLeft + 1
	for i, lbl := range dims.buttonLabels {
		buttonRects = append(buttonRects, rect{
			x: innerLeft + dims.buttonXs[i],
			y: buttonRowY,
			w: lipgloss.Width(lbl),
			h: 1,
		})
	}
	return
}

// Box renders the confirm panel WITHOUT centering — useful for callers
// (or tests) that want the bare box. The dimensions follow the same
// conventions as the Settings modal: width band + auto-fit height +
// flexible footer spacer.
func (m Model) Box() string {
	refreshColors()
	if m.w <= 0 || m.h <= 0 {
		return ""
	}

	dims := m.layoutDims()
	innerW := dims.boxW - 2

	bgFill := lipgloss.NewStyle().Background(cmBoxBg)
	warn := "⚠"
	switch theme.CurrentIconMode {
	case theme.IconModeNerdFont:
		warn = "\uf071" // nf-fa-warning
	case theme.IconModeASCII:
		warn = "!"
	}
	headerIcon := lipgloss.NewStyle().Background(cmBoxBg).Foreground(cmIconWarn).Bold(true).Render(warn)
	headerTitle := lipgloss.NewStyle().Background(cmBoxBg).Foreground(cmTextPri).Bold(true).Render(" " + m.title)
	bodyStyle := lipgloss.NewStyle().Background(cmBoxBg).Foreground(cmTextBody)
	separatorStyle := lipgloss.NewStyle().Background(cmBoxBg).Foreground(cmGuideColor)
	footerStyle := lipgloss.NewStyle().Background(cmBoxBg).Foreground(cmTextDim)

	padBg := func(s string, w int) string {
		used := lipgloss.Width(s)
		if used >= w {
			return s
		}
		return s + bgFill.Render(strings.Repeat(" ", w-used))
	}

	blank := bgFill.Render(strings.Repeat(" ", innerW))

	var rows []string

	// Header — warning icon + bold title left; right side bg-fills the
	// row so the modal header keeps its full width.
	left := bgFill.Render("  ") + headerIcon + headerTitle
	rows = append(rows, padBg(left, innerW))

	// Top hairline separator.
	rows = append(rows, theme.Hairline(innerW, theme.BgFloat))

	// Body — wrap message into innerW-4 columns so it has 2 cells of
	// breathing room either side. Multi-line preserved.
	// One blank row above the body for breathing.
	rows = append(rows, blank)
	codeStyle := lipgloss.NewStyle().Background(cmBoxBg).Foreground(cmTextPri).Bold(true)
	for i, line := range dims.bodyLines {
		style := bodyStyle
		switch {
		case dims.codeAt >= 0 && (i == dims.codeAt || i == dims.codeAt+2):
			style = separatorStyle
		case dims.codeAt >= 0 && i == dims.codeAt+1:
			inner := strings.TrimSuffix(strings.TrimPrefix(line, "│ "), " │")
			row := bgFill.Render("  ") + separatorStyle.Render("│ ") + codeStyle.Render(inner) + separatorStyle.Render(" │")
			rows = append(rows, padBg(row, innerW))
			continue
		}
		row := bgFill.Render("  ") + style.Render(line)
		rows = append(rows, padBg(row, innerW))
	}
	// One blank row below the body before the button row.
	rows = append(rows, blank)

	// Button row — soft selection bar on the active button, muted bg on
	// the rest, with a thin gap of panel bg between buttons. The whole
	// row is centered horizontally inside innerW.
	rows = append(rows, padBg(m.renderButtonsRow(innerW, dims.buttonXs), innerW))

	// Spacer (flexes 3..12) so the footer hugs the bottom border on
	// tall terminals — same shape as settings_modal.Box().
	for i := 0; i < dims.footerSpacer; i++ {
		rows = append(rows, blank)
	}

	// Bottom hairline separator + footer key-hints.
	rows = append(rows, separatorStyle.Render(strings.Repeat("─", innerW)))
	_ = footerStyle
	footer := theme.HintBar(cmBoxBg, confirmHints...)
	rows = append(rows, padBg(bgFill.Render("  ")+footer, innerW))

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	// Height intentionally NOT set — auto-fit to content rows.
	return theme.FloatBox(content, dims.boxW+2)
}

// renderButtonsRow lays out all buttons centered within `w` cells. The
// active button gets the soft selection bar (#1f4f63); the rest sit on
// the panel bg with their style-appropriate accent fg.
//
// The buttonXs slice supplies the precomputed inner-x positions of each
// button (relative to the inner area, not the screen) so the rendered
// row exactly matches what HandleMouse will hit-test. We render
// gap-by-gap rather than via JoinHorizontal to keep widths predictable.
func (m Model) renderButtonsRow(w int, buttonXs []int) string {
	bgFill := lipgloss.NewStyle().Background(cmBoxBg)

	var b strings.Builder
	cur := 0
	for i, btn := range m.buttons {
		// Pad with panel-bg whitespace up to this button's start column.
		if buttonXs[i] > cur {
			b.WriteString(bgFill.Render(strings.Repeat(" ", buttonXs[i]-cur)))
			cur = buttonXs[i]
		}
		label := m.renderButton(btn, i == m.cursor)
		b.WriteString(label)
		cur += lipgloss.Width(m.buttonLabel(btn))
	}
	// Trailing fill to bring the row up to `w`.
	if cur < w {
		b.WriteString(bgFill.Render(strings.Repeat(" ", w-cur)))
	}
	return b.String()
}

// buttonLabel returns the rendered-width label for a button — used for
// layout calculations without paying for the full styled render.
func (m Model) buttonLabel(b Button) string {
	return "  " + b.Title + "  "
}

// renderButton paints one button. Active uses the soft selection bar
// (cmSelBg); inactive uses an accent bg derived from the button's style
// (default → panel bg, primary → cmPrimaryBg, destructive → cmDestructiveBg).
func (m Model) renderButton(b Button, active bool) string {
	label := "  " + b.Title + "  "
	f := theme.Float()
	if active {
		// The focused button is a solid fill: accent for primary / plain,
		// red for destructive.
		bg := cmPrimaryBg
		if b.Style == StyleDestructive {
			bg = theme.LG(theme.DiagError)
		}
		return lipgloss.NewStyle().Background(bg).Foreground(theme.LG(theme.OnAccent)).Bold(true).Render(label)
	}
	// Resting buttons are quiet chips; the label colour hints the role.
	fg := cmTextPri
	switch b.Style {
	case StylePrimary:
		fg = f.Primary
	case StyleDestructive:
		fg = f.Danger
	}
	return lipgloss.NewStyle().Background(f.KeycapBg).Foreground(fg).Render(label)
}

// wrap is a minimal word wrapper, preserving manual newlines.
func wrap(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}
	var out []string
	for _, paragraph := range strings.Split(s, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		var line string
		for _, word := range words {
			if line == "" {
				line = word
				continue
			}
			if lipgloss.Width(line)+1+lipgloss.Width(word) > w {
				out = append(out, line)
				line = word
			} else {
				line += " " + word
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// ButtonRect is where a button sits on screen (one row tall).
type ButtonRect struct {
	ID    string
	Label string // the button text as drawn
	X, Y  int
	W     int
}

// ButtonRects lists the buttons with the screen cells HandleMouse hit
// tests for them.
func (m Model) ButtonRects() []ButtonRect {
	if m.w <= 0 || m.h <= 0 {
		return nil
	}
	dims := m.layoutDims()
	_, rects := m.computeRects()
	out := make([]ButtonRect, len(rects))
	for i, r := range rects {
		out[i] = ButtonRect{ID: m.buttons[i].ID, Label: dims.buttonLabels[i], X: r.x, Y: r.y, W: r.w}
	}
	return out
}

// ButtonAt returns the id of the button at screen cell (x, y), or "".
func (m Model) ButtonAt(x, y int) string {
	for _, b := range m.ButtonRects() {
		if y == b.Y && x >= b.X && x < b.X+b.W {
			return b.ID
		}
	}
	return ""
}
