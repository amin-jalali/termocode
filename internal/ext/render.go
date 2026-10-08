package ext

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/theme"
)

// tokenColor maps a colour token to the live theme palette. Read on every
// call so a theme switch applies at once.
func tokenColor(tok string) (theme.Color256, bool) {
	switch tok {
	case "primary":
		return theme.TextPrimary, true
	case "secondary":
		return theme.TextSecondary, true
	case "muted":
		return theme.TextMuted, true
	case "dim":
		return theme.TextDim, true
	case "white":
		return theme.TextWhite, true
	case "accent":
		return theme.BorderFocus, true
	case "blue":
		return theme.AccentBlue, true
	case "green", "success":
		return theme.AccentGreen, true
	case "amber", "yellow":
		return theme.AccentAmber, true
	case "red":
		return theme.AccentRedCoral, true
	case "magenta":
		return theme.AccentMagenta, true
	case "lavender":
		return theme.AccentLavender, true
	case "error":
		return theme.DiagError, true
	case "warning":
		return theme.DiagWarning, true
	case "info":
		return theme.DiagInfo, true
	case "hint":
		return theme.DiagHint, true
	case "keyword":
		return theme.SyntaxKeyword, true
	case "function":
		return theme.SyntaxFunction, true
	case "string":
		return theme.SyntaxString, true
	case "number":
		return theme.SyntaxNumber, true
	case "type":
		return theme.SyntaxType, true
	case "comment":
		return theme.SyntaxComment, true
	case "constant":
		return theme.SyntaxConstant, true
	case "variable":
		return theme.SyntaxVariable, true
	case "added":
		return theme.GitAdded, true
	case "modified":
		return theme.GitModified, true
	case "deleted":
		return theme.GitDeleted, true
	case "untracked":
		return theme.GitUntracked, true
	}
	return 0, false
}

// Style returns the lipgloss style for a stack of tokens on bg. fg is the
// colour of unstyled text. Later colour tokens win over earlier ones.
func Style(tokens []string, fg theme.Color256, bg lipgloss.TerminalColor) lipgloss.Style {
	st := lipgloss.NewStyle().Background(bg).Foreground(theme.LG(fg))
	for _, t := range tokens {
		switch t {
		case "bold":
			st = st.Bold(true)
		case "italic":
			st = st.Italic(true)
		case "underline":
			st = st.Underline(true)
		case "faint":
			st = st.Faint(true)
		default:
			if c, ok := tokenColor(t); ok {
				st = st.Foreground(theme.LG(c))
			}
		}
	}
	return st
}

// Render turns one markup line into a styled string on bg. Every cell —
// plain text included — carries bg, so the line never leaks the
// terminal's default background.
func Render(line string, fg theme.Color256, bg lipgloss.TerminalColor) string {
	var b strings.Builder
	for _, sp := range ParseMarkup(line) {
		b.WriteString(Style(sp.Styles, fg, bg).Render(sp.Text))
	}
	return b.String()
}
