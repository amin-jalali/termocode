package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Fast renders one-line text in a lipgloss style without going through
// lipgloss each time. For a style of colours and attributes only (no
// width, padding, border or margins), lipgloss writes plain text as the
// style's opening sequence, the text and a reset; Fast takes the opening
// sequence once and adds the text. Text it cannot do that for (empty,
// several lines, tabs or escapes) and styles that do not render that way
// go through lipgloss, so the output is always the same bytes.
type Fast struct {
	st  lipgloss.Style
	pre string // "" = always lipgloss
}

const fastReset = "\x1b[0m"

// NewFast prepares st. Call it again after a theme or profile change.
func NewFast(st lipgloss.Style) Fast {
	f := Fast{st: st}
	r := st.Render("x")
	if !strings.HasSuffix(r, "x"+fastReset) {
		return f
	}
	pre := r[:len(r)-1-len(fastReset)]
	if pre == "" || strings.ContainsAny(pre, "\n") {
		return f
	}
	// The probe must hold for wider text too (no width, padding, …).
	if st.Render("a b") != pre+"a b"+fastReset {
		return f
	}
	f.pre = pre
	return f
}

// Render is f's style applied to s, as lipgloss.Style.Render.
func (f Fast) Render(s string) string {
	if f.pre == "" || s == "" || strings.ContainsAny(s, "\n\r\t\x1b") {
		return f.st.Render(s)
	}
	return f.pre + s + fastReset
}
