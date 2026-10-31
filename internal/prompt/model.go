package prompt

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/theme"
)

// SubmitMsg is emitted when the user presses Enter.
type SubmitMsg struct{ Value string }

// CloseMsg is emitted when the user dismisses the prompt.
type CloseMsg struct{}

type Model struct {
	multiline bool
	caret     int
	title     string
	label     string
	value     string
	masked    bool // render the value as bullets (secrets: tokens, passwords)
	w, h      int
	// complete, when set, is what Tab does to the value (SetCompleter).
	complete func(string) string
	// note is extra text under the label, wrapped to the card (SetNote).
	note string
	// allowEmpty lets Enter submit an empty value (SetAllowEmpty).
	allowEmpty bool
	// errText is a refusal shown under the input (SetError); typing clears it.
	errText string
}

// SetError shows a refusal in red under the input and keeps the prompt
// open. Any edit of the value clears it. "" clears it now.
func (m *Model) SetError(text string) { m.errText = text }

// Error is the refusal SetError set (empty: none).
func (m Model) Error() string { return m.errText }

// SetAllowEmpty lets Enter submit an empty value, for optional steps
// ("empty to finish"). Other prompts ignore Enter on an empty field.
func (m *Model) SetAllowEmpty(on bool) { m.allowEmpty = on }

// SetNote shows text (several lines, wrapped) between the label and the
// input: instructions that do not fit the one-line label.
func (m *Model) SetNote(text string) { m.note = text }

// wrapWords wraps s to width w by words; a word longer than w is cut.
func wrapWords(s string, w int) []string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			for runewidth.StringWidth(word) > w {
				if line != "" {
					out = append(out, line)
					line = ""
				}
				cut := runewidth.Truncate(word, w, "")
				out = append(out, cut)
				word = word[len(cut):]
			}
			switch {
			case line == "":
				line = word
			case runewidth.StringWidth(line)+1+runewidth.StringWidth(word) <= w:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
		}
		out = append(out, line)
	}
	return out
}

// SetMasked hides the typed value behind bullets (for secrets). The value
// itself is unchanged and still returned in SubmitMsg.
func (m *Model) SetMasked(on bool) { m.masked = on }

// Masked, Note and Value report the prompt's state (tests check that a
// secret prompt is masked and that a submitted one was cleared).
func (m Model) Masked() bool  { return m.masked }
func (m Model) Note() string  { return m.note }
func (m Model) Value() string { return m.value }

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
	if m.multiline {
		return m.updateMultiline(k)
	}
	switch k.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		return m, func() tea.Msg { return CloseMsg{} }
	case tea.KeyBackspace, tea.KeySpace, tea.KeyRunes:
		m.errText = ""
	}
	switch k.Type {
	case tea.KeyEnter:
		v := strings.TrimSpace(m.value)
		if v == "" && !m.allowEmpty {
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
	case tea.KeyTab:
		if m.complete != nil {
			m.value = m.complete(m.value)
		}
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

// View renders the prompt card.
func (m Model) View() string { return m.Box() }

// Box renders the prompt as a compact floating card:
//
//	╭──────────────────────────────────────╮
//	│  ❯ Title                     label   │
//	│  ────────────────────                 │
//	│  label                                │
//	│  ▏ value                              │
//	│                                       │
//	│  enter submit   esc cancel            │
//	╰──────────────────────────────────────╯
//
// Colours come from theme.Float(), shared with every floating card.
// Origin is the box's top-left cell when the app centres it (modalOverlay
// default placement).
func (m Model) Origin() (x, y int) {
	box := m.Box()
	return (m.w - lipgloss.Width(box)) / 2, (m.h - lipgloss.Height(box)) / 2
}

// HandleMouse makes the footer hints buttons: a click on "Enter submit"
// submits, on "Esc cancel" closes. Other clicks do nothing.
func (m Model) HandleMouse(x, y int, action tea.MouseAction, button tea.MouseButton) (Model, tea.Cmd) {
	if m.multiline {
		return m, nil
	}
	if action != tea.MouseActionPress || button != tea.MouseButtonLeft || m.w <= 0 {
		return m, nil
	}
	bx, by := m.Origin()
	box := m.Box()
	if y != by+lipgloss.Height(box)-2 {
		return m, nil
	}
	rel := x - (bx + 1 + 2)
	for _, sp := range theme.HintSpans(promptHints...) {
		if rel < sp.X0 || rel >= sp.X1 {
			continue
		}
		switch sp.Key {
		case "enter":
			if v := strings.TrimSpace(m.value); v != "" || m.allowEmpty {
				return m, func() tea.Msg { return SubmitMsg{Value: v} }
			}
		case "esc":
			return m, func() tea.Msg { return CloseMsg{} }
		}
	}
	return m, nil
}

var promptHints = []string{"enter", "submit", "esc", "cancel"}

func (m Model) Box() string {
	if m.multiline {
		return m.multilineBox()
	}
	if m.w < 3 || m.h <= 0 {
		return ""
	}
	boxW := 64
	if maxW := m.w - 4; boxW > maxW {
		boxW = maxW
	}
	boxW = max(3, min(boxW, m.w))
	innerW := boxW - 2
	f := theme.Float()

	bgFill := lipgloss.NewStyle().Background(f.Bg)
	inputBgFill := lipgloss.NewStyle().Background(f.InputBg)
	inputStyle := lipgloss.NewStyle().Background(f.InputBg).Foreground(f.InputFg)
	caret := lipgloss.NewStyle().Background(f.InputBg).Foreground(f.Icon).Bold(true)
	headerIcon := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Icon).Bold(true).Render(promptGlyph())
	headerTitle := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Title).Bold(true).Render(" " + m.title)
	labelStyle := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Sub)

	padBg := func(s string, w int) string {
		if used := lipgloss.Width(s); used < w {
			return s + bgFill.Render(strings.Repeat(" ", w-used))
		}
		return s
	}
	blank := bgFill.Render(strings.Repeat(" ", innerW))

	rows := []string{padBg(bgFill.Render("  ")+headerIcon+headerTitle, innerW)}
	rows = append(rows, theme.Hairline(innerW, theme.BgFloat))
	if strings.TrimSpace(m.label) != "" {
		label := m.label
		if max := innerW - 4; runewidth.StringWidth(label) > max && max > 0 {
			label = runewidth.Truncate(label, max, "…")
		}
		rows = append(rows, padBg(bgFill.Render("  ")+labelStyle.Render(label), innerW))
	} else {
		rows = append(rows, blank)
	}
	if strings.TrimSpace(m.note) != "" {
		for _, l := range wrapWords(m.note, innerW-4) {
			rows = append(rows, padBg(bgFill.Render("  ")+labelStyle.Render(l), innerW))
		}
	}

	value := m.value
	if m.masked {
		value = strings.Repeat("•", len([]rune(m.value)))
	}
	maxValW := innerW - 6
	if maxValW < 1 {
		maxValW = 1
	}
	if runewidth.StringWidth(value) > maxValW {
		// Keep the end of long values visible (that is where the caret is).
		r := []rune(value)
		for runewidth.StringWidth(string(r)) > maxValW-1 && len(r) > 0 {
			r = r[1:]
		}
		value = "…" + string(r)
	}
	field := bgFill.Render(" ") + inputBgFill.Render(" ") + inputStyle.Render(value) + caret.Render("▏")
	if w := lipgloss.Width(field); w < innerW-1 {
		field += inputBgFill.Render(strings.Repeat(" ", innerW-1-w))
	}
	rows = append(rows, field+bgFill.Render(" "))
	if strings.TrimSpace(m.errText) != "" {
		errStyle := lipgloss.NewStyle().Background(f.Bg).Foreground(f.Danger)
		for _, l := range wrapWords(m.errText, innerW-4) {
			rows = append(rows, padBg(bgFill.Render("  ")+errStyle.Render(l), innerW))
		}
	}
	rows = append(rows, blank)
	rows = append(rows, padBg(bgFill.Render("  ")+theme.HintBar(f.Bg, promptHints...), innerW))

	for i := range rows {
		rows[i] = ansi.Truncate(rows[i], innerW, "")
	}
	return theme.FloatBox(lipgloss.JoinVertical(lipgloss.Left, rows...), boxW)
}

// promptGlyph is the header icon for the active icon mode.
func promptGlyph() string {
	switch theme.CurrentIconMode {
	case theme.IconModeNerdFont:
		return "\uf044" // nf-fa-pencil_square_o
	case theme.IconModeUnicode:
		return "❯"
	}
	return ">"
}
