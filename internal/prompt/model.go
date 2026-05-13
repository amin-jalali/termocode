package prompt

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// SubmitMsg is emitted when the user presses Enter.
type SubmitMsg struct{ Value string }

// CloseMsg is emitted when the user dismisses the prompt.
type CloseMsg struct{}

type Model struct {
	title string
	label string
	value string
	w, h  int
}

// New returns a prompt with the given title, input label, and initial value.
func New(title, label, initial string) Model {
	return Model{title: title, label: label, value: initial}
}

// NewWithDefault is an alias for New, provided for callers that want to
// be explicit that `initial` is a default suggestion rather than a forced
// value. The two constructors are otherwise identical.
func NewWithDefault(title, label, initial string) Model {
	return New(title, label, initial)
}

// Init satisfies the bubbletea.Model interface — the prompt has no
// startup commands of its own.
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
		v := strings.TrimSpace(m.value)
		if v == "" {
			return m, nil
		}
		return m, func() tea.Msg { return SubmitMsg{Value: v} }
	case tea.KeyBackspace:
		if len(m.value) > 0 {
			r := []rune(m.value)
			m.value = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		m.value += " "
	case tea.KeyRunes:
		// Bubble Tea sometimes delivers a single mouse-tracking SGR
		// (e.g. "[<35;70;19M") across MULTIPLE KeyRunes events. Drop
		// fragment-shaped events up front so nothing transient ever
		// lands in m.value (otherwise partial chunks flicker in and
		// out as the user moves the mouse). See picker/model.go for
		// the heuristic — kept in lockstep here.
		//
		// EXTRA: a single-rune `[` with Alt=true is what Bubble Tea
		// produces when the trailing `M` of a mouse SGR didn't arrive
		// in the same read — the alt-prefix path kept only the first
		// rune. Drop any Alt+mouse-code-rune event so that lone `[`
		// (or `<`) doesn't leak into the prompt input as the user
		// crosses the styled input row of the modal.
		if isMouseFragmentEvent(k.Runes) {
			return m, nil
		}
		if k.Alt && isMouseCodeRunes(k.Runes) {
			return m, nil
		}
		m.value = sanitizeInput(m.value + string(k.Runes))
	}
	return m, nil
}

// isMouseCodeRunes reports whether `runes` consists ENTIRELY of bytes
// that can appear inside an SGR mouse sequence — digits, `;`, `<`, `[`,
// `M`, `m`. Used together with k.Alt to drop the single-rune `[` Bubble
// Tea emits when an `\x1b[<…M` mouse SGR is split across reads and the
// parser falls back to the alt-prefix rune scanner (which only keeps
// one rune per event).
func isMouseCodeRunes(runes []rune) bool {
	if len(runes) == 0 {
		return false
	}
	for _, r := range runes {
		switch {
		case r >= '0' && r <= '9':
		case r == ';' || r == '<' || r == '[' || r == 'M' || r == 'm':
		default:
			return false
		}
	}
	return true
}

// isMouseFragmentEvent reports whether a KeyRunes event payload is
// almost certainly a fragment of a mouse-tracking SGR sequence (e.g.
// "[<35", ";70", ";19M") rather than something the user typed. See
// internal/picker/model.go for the rationale — this copy is kept in
// lockstep so both modals filter identically.
func isMouseFragmentEvent(runes []rune) bool {
	if len(runes) < 2 {
		return false
	}
	first := runes[0]
	if first != '[' && first != '<' && first != ';' {
		return false
	}
	for _, r := range runes {
		switch {
		case r >= '0' && r <= '9':
		case r == ';' || r == '<' || r == '[' || r == 'M' || r == 'm':
		default:
			return false
		}
	}
	return true
}

// sanitizeInput strips ANSI control sequences and other non-printable
// bytes that can sneak into a text-input field — typically when the
// terminal emits a mouse-tracking SGR code that the program's input
// parser didn't intercept (e.g. mid-toggle of mouse mode).
//
// Drops:
//   - full CSI escapes: \x1b[<params><final 0x40..0x7E>
//   - bare mouse-SGR fragments [<digits;digits;digits{M|m}] (ESC already
//     stripped by the upstream parser)
//   - control bytes (< 0x20 or 0x7F)
func sanitizeInput(s string) string {
	var b []byte
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
			i = j
			continue
		}
		if c == '[' && i+1 < len(s) && s[i+1] == '<' {
			j := i + 2
			ok := true
			for j < len(s) && s[j] != 'M' && s[j] != 'm' {
				if (s[j] < '0' || s[j] > '9') && s[j] != ';' {
					ok = false
					break
				}
				j++
			}
			if ok && j < len(s) {
				i = j + 1
				continue
			}
		}
		if c < 0x20 || c == 0x7f {
			i++
			continue
		}
		b = append(b, c)
		i++
	}
	return string(b)
}

// Palette mirrors the Settings modal (see internal/app/settings_modal.go)
// so every overlay in termocode shares the same visual idiom — same
// panel bg, same rim, same hairline separators, same input bar tint.
var (
	pmBoxBg      = lipgloss.Color("#262626")
	pmBorderRim  = lipgloss.Color("#3a3a3a")
	pmIconBlue   = lipgloss.Color("#569cd6")
	pmTextPri    = lipgloss.Color("#d0d0d0")
	pmTextDim    = lipgloss.Color("#6c6c6c") // footer hints
	pmGuideColor = lipgloss.Color("#363636") // hairline separators
	pmSubtitleFg = lipgloss.Color("#5a5a66")
	pmInputBg    = lipgloss.Color("#1f3447")
	pmInputFg    = lipgloss.Color("#ffffff")
	pmLabelFg    = lipgloss.Color("#9090a0")
)

func (m Model) View() string { return m.Box() }

// Box renders the prompt modal: header (icon + title + faint subtitle),
// hairline separator, label row, input row, spacer, hairline separator,
// footer hints. Width capped in a comfortable band; height auto-fits to
// content + flexible spacer so the footer hugs the bottom on tall
// terminals without padding short ones.
func (m Model) Box() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	boxW := 64
	if maxW := m.w - 4; boxW > maxW {
		boxW = maxW
	}
	if boxW < 36 {
		boxW = 36
	}
	innerW := boxW - 2

	bgFill := lipgloss.NewStyle().Background(pmBoxBg)
	inputBgFill := lipgloss.NewStyle().Background(pmInputBg)
	inputStyle := lipgloss.NewStyle().Background(pmInputBg).Foreground(pmInputFg)
	inputCursorStyle := lipgloss.NewStyle().Background(pmInputBg).Foreground(pmInputFg).Underline(true)
	headerIcon := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmIconBlue).Bold(true).Render("›")
	headerTitle := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmTextPri).Bold(true).Render(" " + m.title)
	subtitle := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmSubtitleFg).Faint(true).Render(m.label)
	labelStyle := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmLabelFg)
	separatorStyle := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmGuideColor)
	footerStyle := lipgloss.NewStyle().Background(pmBoxBg).Foreground(pmTextDim)

	padBg := func(s string, w int) string {
		used := lipgloss.Width(s)
		if used >= w {
			return s
		}
		return s + bgFill.Render(strings.Repeat(" ", w-used))
	}

	blank := bgFill.Render(strings.Repeat(" ", innerW))

	var rows []string

	// Header — icon + bold title left, faint label as subtitle right.
	left := bgFill.Render("  ") + headerIcon + headerTitle
	right := subtitle + bgFill.Render("  ")
	gap := innerW - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		// If the subtitle would overflow, drop it; the title alone fits.
		right = bgFill.Render("  ")
		gap = innerW - lipgloss.Width(left) - lipgloss.Width(right)
		if gap < 1 {
			gap = 1
		}
	}
	rows = append(rows, left+bgFill.Render(strings.Repeat(" ", gap))+right)

	// Top hairline separator.
	rows = append(rows, separatorStyle.Render(strings.Repeat("─", innerW)))

	// Optional label row above the input field — kept compact and muted
	// so the input itself remains the focal element.
	if strings.TrimSpace(m.label) != "" {
		labelRow := bgFill.Render("  ") + labelStyle.Render(m.label)
		rows = append(rows, padBg(labelRow, innerW))
	} else {
		rows = append(rows, blank)
	}

	// Input row — chevron prefix on the input bg, value, soft cursor.
	// Layout: " › <value>▎" then padded to innerW with input bg so the
	// whole row reads as a single field.
	value := m.value
	// Truncate value if it would overflow the input width.
	maxValW := innerW - 4 /*" › "*/ - 1 /*cursor*/
	if maxValW < 1 {
		maxValW = 1
	}
	if runewidth.StringWidth(value) > maxValW {
		value = runewidth.Truncate(value, maxValW, "…")
	}
	inputContent := inputBgFill.Render(" › ") + inputStyle.Render(value) + inputCursorStyle.Render("▎")
	if w := lipgloss.Width(inputContent); w < innerW {
		inputContent += inputBgFill.Render(strings.Repeat(" ", innerW-w))
	}
	rows = append(rows, inputContent)

	// Spacer — flexes 3..12 rows so the footer sits near the bottom on
	// tall terminals while staying compact on short ones. Math mirrors
	// settings_modal.Box() so all overlays share the same vertical feel.
	desiredH := m.h * 80 / 100
	contentH := len(rows)
	footerSpacer := desiredH - contentH - 3 // 3 = footer + 2 border rows
	if footerSpacer < 3 {
		footerSpacer = 3
	}
	if footerSpacer > 12 {
		footerSpacer = 12
	}
	for i := 0; i < footerSpacer; i++ {
		rows = append(rows, blank)
	}

	// Bottom hairline separator + footer key-hints.
	rows = append(rows, separatorStyle.Render(strings.Repeat("─", innerW)))
	footer := footerStyle.Render("↵ Submit    Esc Cancel")
	rows = append(rows, padBg(bgFill.Render("  ")+footer, innerW))

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	// Height intentionally NOT set — lipgloss auto-fits the rounded
	// border to the content row count. (Setting Height(N) inserts blank
	// rows BELOW the footer before the bottom rim — visible as a gap.)
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pmBorderRim).
		BorderBackground(pmBoxBg).
		Background(pmBoxBg).
		Width(boxW)
	return border.Render(content)
}
