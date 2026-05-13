package confirm

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	title   string
	message string
	buttons []Button
	cursor  int
	w, h    int
}

func New(title, message string, buttons []Button) Model {
	return Model{title: title, message: message, buttons: buttons}
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
		lipgloss.WithWhitespaceForeground(lipgloss.Color("#1c1c1c")),
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

	if button != tea.MouseButtonLeft || action != tea.MouseActionPress {
		return m, nil, inside
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

// Palette mirrors the Settings modal so confirm shares the same visual
// idiom — same panel bg, dim rim, hairline separators, soft selection
// bar tint.
var (
	cmBoxBg      = lipgloss.Color("#262626")
	cmBorderRim  = lipgloss.Color("#3a3a3a")
	cmIconWarn   = lipgloss.Color("#e0a050") // amber for the ⚠ glyph
	cmTextPri    = lipgloss.Color("#d0d0d0")
	cmTextBody   = lipgloss.Color("#cccccc")
	cmTextDim    = lipgloss.Color("#6c6c6c")
	cmGuideColor = lipgloss.Color("#363636")
	cmSubtitleFg = lipgloss.Color("#5a5a66")
	cmSelBg      = lipgloss.Color("#1f4f63") // soft selection bar (matches settings)
	cmSelFg      = lipgloss.Color("#ffffff")
	// Destructive / primary tints are kept softer than the previous
	// solid VSCode blues so they harmonise with the muted panel bg.
	cmPrimaryBg     = lipgloss.Color("#0e639c")
	cmDestructiveBg = lipgloss.Color("#a04030")
)

// confirmDims captures every measurement Box() needs in one place, so
// that Box() and HandleMouse() can both compute the same layout from
// the same source of truth (mirrors picker.layoutDims()).
type confirmDims struct {
	boxW         int
	bodyLines    []string
	footerSpacer int
	buttonLabels []string
	buttonXs     []int // inner-x offset of each button (relative to inner area)
}

// layoutDims is the single source of truth for confirm panel geometry.
// Both Box() and HandleMouse() consult it so the rendered button
// positions and the hit-test rects line up exactly.
func (m Model) layoutDims() confirmDims {
	boxW := 60
	if maxW := m.w - 8; boxW > maxW {
		boxW = maxW
	}
	if boxW < 36 {
		boxW = 36
	}
	innerW := boxW - 2
	bodyW := innerW - 4
	if bodyW < 1 {
		bodyW = 1
	}
	bodyLines := wrap(m.message, bodyW)

	// Cap body to a sensible max so the modal doesn't fill the screen
	// when given a huge message; tail is replaced with an ellipsis.
	const maxBodyLines = 12
	if len(bodyLines) > maxBodyLines {
		extra := len(bodyLines) - maxBodyLines + 1
		bodyLines = bodyLines[:maxBodyLines-1]
		bodyLines = append(bodyLines, "…and "+strconv.Itoa(extra)+" more lines")
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
	contentH := 1 + 1 + 1 + len(bodyLines) + 1 + 1
	desiredH := m.h * 80 / 100
	footerSpacer := desiredH - contentH - 3
	if footerSpacer < 3 {
		footerSpacer = 3
	}
	if footerSpacer > 12 {
		footerSpacer = 12
	}

	return confirmDims{
		boxW:         boxW,
		bodyLines:    bodyLines,
		footerSpacer: footerSpacer,
		buttonLabels: labels,
		buttonXs:     xs,
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
	if m.w <= 0 || m.h <= 0 {
		return ""
	}

	dims := m.layoutDims()
	innerW := dims.boxW - 2

	bgFill := lipgloss.NewStyle().Background(cmBoxBg)
	headerIcon := lipgloss.NewStyle().Background(cmBoxBg).Foreground(cmIconWarn).Bold(true).Render("⚠")
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
	rows = append(rows, separatorStyle.Render(strings.Repeat("─", innerW)))

	// Body — wrap message into innerW-4 columns so it has 2 cells of
	// breathing room either side. Multi-line preserved.
	// One blank row above the body for breathing.
	rows = append(rows, blank)
	for _, line := range dims.bodyLines {
		row := bgFill.Render("  ") + bodyStyle.Render(line)
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
	footer := footerStyle.Render("←→ Move    ↵ Select    Esc Cancel")
	rows = append(rows, padBg(bgFill.Render("  ")+footer, innerW))

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	// Height intentionally NOT set — auto-fit to content rows.
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cmBorderRim).
		BorderBackground(cmBoxBg).
		Background(cmBoxBg).
		Width(dims.boxW)
	return border.Render(content)
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

	if active {
		// Active button — selection bar. Style-tint shows through as the
		// fg accent so the user can still distinguish destructive vs
		// primary even when highlighted.
		fg := cmSelFg
		switch b.Style {
		case StyleDestructive:
			fg = lipgloss.Color("#ffd0c4")
		case StylePrimary:
			fg = lipgloss.Color("#cce6f4")
		}
		return lipgloss.NewStyle().
			Background(cmSelBg).
			Foreground(fg).
			Bold(true).
			Render(label)
	}

	// Inactive — keep the accent bg, just less prominent than before.
	bg := cmBoxBg
	fg := cmTextPri
	switch b.Style {
	case StylePrimary:
		bg = cmPrimaryBg
		fg = lipgloss.Color("#ffffff")
	case StyleDestructive:
		bg = cmDestructiveBg
		fg = lipgloss.Color("#ffffff")
	default:
		bg = lipgloss.Color("#3a3a3a")
		fg = lipgloss.Color("#cccccc")
	}
	return lipgloss.NewStyle().Background(bg).Foreground(fg).Render(label)
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
