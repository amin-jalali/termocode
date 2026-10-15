package replacebar

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/theme"
)

// ReplaceNextMsg fires on Enter — replace the next match and advance.
type ReplaceNextMsg struct {
	Find    string
	Replace string
}

// ReplaceAllMsg fires on Ctrl+Enter / Alt+Enter — replace every occurrence
// in the current buffer.
type ReplaceAllMsg struct {
	Find    string
	Replace string
}

// CloseMsg fires on Esc.
type CloseMsg struct{}

// field identifies which of the two inputs has focus.
type field int

const (
	fieldFind field = iota
	fieldReplace
)

type Model struct {
	find    string
	replace string
	focus   field
	w       int
}

func New() Model { return Model{focus: fieldFind} }

func (m *Model) SetWidth(w int) { m.w = w }

func (m Model) Find() string    { return m.find }
func (m Model) Replace() string { return m.replace }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	// Ctrl+Enter / Alt+Enter → replace all. Different terminals encode this
	// in different ways; check the rendered key string to catch both.
	s := k.String()
	if s == "ctrl+enter" || s == "alt+enter" {
		find, repl := m.find, m.replace
		return m, func() tea.Msg { return ReplaceAllMsg{Find: find, Replace: repl} }
	}

	switch k.Type {
	case tea.KeyEsc:
		return m, func() tea.Msg { return CloseMsg{} }

	case tea.KeyEnter:
		// Alt+Enter sometimes arrives as KeyEnter with k.Alt set.
		if k.Alt {
			find, repl := m.find, m.replace
			return m, func() tea.Msg { return ReplaceAllMsg{Find: find, Replace: repl} }
		}
		find, repl := m.find, m.replace
		return m, func() tea.Msg { return ReplaceNextMsg{Find: find, Replace: repl} }

	case tea.KeyTab, tea.KeyDown:
		if m.focus == fieldFind {
			m.focus = fieldReplace
		} else {
			m.focus = fieldFind
		}
		return m, nil

	case tea.KeyShiftTab, tea.KeyUp:
		if m.focus == fieldReplace {
			m.focus = fieldFind
		} else {
			m.focus = fieldReplace
		}
		return m, nil

	case tea.KeyBackspace:
		s := m.value()
		if len(s) > 0 {
			r := []rune(s)
			m.setValue(string(r[:len(r)-1]))
		}
		return m, nil

	case tea.KeySpace:
		m.setValue(m.value() + " ")
		return m, nil

	case tea.KeyRunes:
		m.setValue(m.value() + string(k.Runes))
		return m, nil
	}
	return m, nil
}

func (m Model) value() string {
	if m.focus == fieldFind {
		return m.find
	}
	return m.replace
}

func (m *Model) setValue(v string) {
	if m.focus == fieldFind {
		m.find = v
	} else {
		m.replace = v
	}
}

var (
	bgStyle    lipgloss.Style
	labelStyle lipgloss.Style
	inputStyle lipgloss.Style
	inputDim   lipgloss.Style
	hintStyle  lipgloss.Style
)

func init() { refreshColors() }

// refreshColors derives this file's colours from the active theme. It runs
// before every render so a theme switch applies immediately.
func refreshColors() {
	bgStyle = lipgloss.NewStyle().Background(theme.LG(theme.BgSidebar))
	labelStyle = lipgloss.NewStyle().
		Background(theme.LG(theme.BgKeycap)).
		Foreground(theme.LG(theme.AccentAmber)).
		Bold(true)
	inputStyle = lipgloss.NewStyle().
		Background(theme.LG(theme.BgEditor)).
		Foreground(theme.LG(theme.TextWhite))
	inputDim = lipgloss.NewStyle().
		Background(theme.LG(theme.BgEditor)).
		Foreground(theme.LG(theme.TextMuted))
	hintStyle = lipgloss.NewStyle().
		Background(theme.LG(theme.BgSidebar)).
		Foreground(theme.LG(theme.TextMuted))
}

func (m Model) View() string {
	refreshColors()
	if m.w <= 0 {
		return ""
	}
	findRow := m.renderRow(" Find    ", m.find, m.focus == fieldFind, " Enter replace · Tab next field · Esc close ")
	replRow := m.renderRow(" Replace ", m.replace, m.focus == fieldReplace, " Ctrl+Enter / Alt+Enter replace all · Esc close ")
	return findRow + "\n" + replRow
}

// renderRow draws one input row in the same shape as findbar.View.
func (m Model) renderRow(label, value string, focused bool, hint string) string {
	lbl := labelStyle.Render(label)
	hnt := hintStyle.Render(hint)
	used := lipgloss.Width(lbl) + lipgloss.Width(hnt) + 2 // two 1-col gaps

	inputW := m.w - used
	if inputW < 4 {
		inputW = 4
	}

	cursor := ""
	if focused {
		cursor = "▍"
	}
	body := " " + value + cursor
	if pad := inputW - lipgloss.Width(body); pad > 0 {
		body += strings.Repeat(" ", pad)
	}
	if lipgloss.Width(body) > inputW {
		body = body[len(body)-inputW:]
	}

	style := inputDim
	if focused {
		style = inputStyle
	}
	gap := bgStyle.Render(" ")
	return lbl + gap + style.Render(body) + gap + hnt
}
