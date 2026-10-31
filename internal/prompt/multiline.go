package prompt

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

// SetMultiline enables a names editor. Enter adds a line; Ctrl+S submits.
func (m *Model) SetMultiline() { m.multiline = true; m.caret = 0 }
func (m Model) updateMultiline(k tea.KeyMsg) (Model, tea.Cmd) {
	r := []rune(m.value)
	pos := min(m.caret, len(r))
	insert := ""
	switch k.String() {
	case "esc", "ctrl+c":
		return m, func() tea.Msg { return CloseMsg{} }
	case "ctrl+s":
		return m, func() tea.Msg { return SubmitMsg{Value: m.value} }
	case "left":
		pos = max(pos-1, 0)
	case "right":
		pos = min(pos+1, len(r))
	case "home", "ctrl+a":
		for pos > 0 && r[pos-1] != '\n' {
			pos--
		}
	case "end", "ctrl+e":
		for pos < len(r) && r[pos] != '\n' {
			pos++
		}
	case "up", "down":
		start := pos
		for start > 0 && r[start-1] != '\n' {
			start--
		}
		col := pos - start
		if k.String() == "up" && start > 0 {
			end := start - 1
			start = end
			for start > 0 && r[start-1] != '\n' {
				start--
			}
			pos = min(start+col, end)
		}
		if k.String() == "down" {
			end := pos
			for end < len(r) && r[end] != '\n' {
				end++
			}
			if end < len(r) {
				start = end + 1
				end = start
				for end < len(r) && r[end] != '\n' {
					end++
				}
				pos = min(start+col, end)
			}
		}
	case "backspace":
		if pos > 0 {
			r = append(r[:pos-1], r[pos:]...)
			pos--
		}
	case "delete":
		if pos < len(r) {
			r = append(r[:pos], r[pos+1:]...)
		}
	case "ctrl+k":
		end := pos
		for end < len(r) && r[end] != '\n' {
			end++
		}
		r = append(r[:pos], r[end:]...)
	case "enter":
		insert = "\n"
	case " ":
		insert = " "
	default:
		if k.Type == tea.KeyRunes {
			parts := strings.Split(strings.ReplaceAll(string(k.Runes), "\r\n", "\n"), "\n")
			for i := range parts {
				parts[i] = sanitizeInput(parts[i])
			}
			insert = strings.Join(parts, "\n")
		}
	}
	if insert != "" {
		r = append(append(append([]rune{}, r[:pos]...), []rune(insert)...), r[pos:]...)
		pos += len([]rune(insert))
	}
	m.value = string(r)
	m.caret = pos
	return m, nil
}
func (m Model) multilineBox() string {
	if m.w < 3 || m.h <= 0 {
		return ""
	}
	w := max(3, min(76, m.w-4))
	f := theme.Float()
	style := lipgloss.NewStyle().Background(f.Bg).Foreground(f.InputFg)
	r := []rune(m.value)
	pos := min(m.caret, len(r))
	text := string(r[:pos]) + "▏" + string(r[pos:])
	lines := strings.Split(text, "\n")
	row := strings.Count(string(r[:pos]), "\n")
	h := max(m.h-9, 1)
	top := max(row-h+1, 0)
	rows := []string{m.title, "One name per line"}
	for i := top; i < min(top+h, len(lines)); i++ {
		rows = append(rows, ansi.Truncate(lines[i], w-4, "…"))
	}
	rows = append(rows, "Ctrl+S preview   Esc cancel")
	for i := range rows {
		rows[i] = ansi.Truncate(rows[i], w-2, "")
	}
	return theme.FloatBox(style.Width(w-2).Render(strings.Join(rows, "\n")), w)
}
