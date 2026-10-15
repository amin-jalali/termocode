package theme

import (
	"github.com/charmbracelet/x/ansi"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// Floating chrome (modals, pickers, prompts, confirms, menus, toasts)
// shares one look: an opaque BgFloat card with a rounded border, a soft
// accent selection bar, keycap hints in the footer and a drop shadow
// drawn by the app's overlay compositor. These helpers derive every
// colour from palette tokens so the look follows the theme.

// FloatColors bundles the derived colours of a floating card.
type FloatColors struct {
	Bg, Border, Hairline        lipgloss.Color
	InputBg, InputFg            lipgloss.Color
	SelBg, SelFg, SelAccent     lipgloss.Color
	Title, Text, Sub, Dim, Hint lipgloss.Color
	Match, Icon                 lipgloss.Color
	KeycapBg, KeycapFg          lipgloss.Color
	Danger, DangerBg            lipgloss.Color
	Primary, PrimaryFg          lipgloss.Color
}

// Float returns the colours for floating chrome in the active theme.
func Float() FloatColors {
	light := IsLight()
	in := 0.07
	if light {
		in = 0.05
	}
	return FloatColors{
		Bg:        LG(BgFloat),
		Border:    Mix(BorderDefault, BgFloat, 0.15),
		Hairline:  Mix(BgFloat, BorderDefault, 0.75),
		InputBg:   Mix(BgFloat, TextPrimary, in),
		InputFg:   LG(TextWhite),
		SelBg:     Wash(BgFloat, WashSelectedFocused),
		SelFg:     LG(TextWhite),
		SelAccent: LG(BorderFocus),
		Title:     LG(TextWhite),
		Text:      LG(TextPrimary),
		Sub:       LG(TextSecondary),
		Dim:       LG(TextDim),
		Hint:      LG(TextMuted),
		Match:     LG(AccentAmber),
		Icon:      LG(BorderFocus),
		KeycapBg:  Mix(BgFloat, TextPrimary, 0.12),
		KeycapFg:  LG(TextSecondary),
		Danger:    LG(DiagError),
		DangerBg:  Mix(BgFloat, DiagError, 0.75),
		Primary:   LG(BorderFocus),
		PrimaryFg: LG(OnAccent),
	}
}

// FloatBox wraps content rows in the rounded float card. width is the
// outer width (border included); every content row should already be
// width-2 cells wide.
func FloatBox(content string, width int) string {
	c := Float()
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(c.Border).
		BorderBackground(c.Bg).
		Background(c.Bg).
		Width(width - 2).
		Render(content)
}

// Keycap renders a key hint chip (" ⌃P " on a raised background).
func Keycap(key string, bg lipgloss.Color) string {
	c := Float()
	_ = bg
	return lipgloss.NewStyle().Background(c.KeycapBg).Foreground(c.KeycapFg).Bold(true).Render(" " + KeyLabel(key) + " ")
}

// KeyLabel writes a key the way every hint in termocode does, the keyboard
// reference's notation (UI review #3): "enter" → "Enter", "ctrl+c" →
// "Ctrl+C", "^P" → "Ctrl+P", "⌃↵" → "Ctrl+Enter". Single letters ("a",
// "A/R") and arrows stay as they are: their case is the key.
func KeyLabel(key string) string {
	if strings.HasPrefix(key, "^") && len(key) == 2 {
		return "Ctrl+" + strings.ToUpper(key[1:])
	}
	if r := []rune(key); len(r) == 2 && (r[0] == '⌃' || r[0] == '⌥' || r[0] == '⇧') {
		return map[rune]string{'⌃': "Ctrl+", '⌥': "Alt+", '⇧': "Shift+"}[r[0]] + KeyLabel(string(r[1]))
	}
	parts := strings.Split(key, "+")
	if len(parts) > 1 && parts[len(parts)-1] == "" { // "ctrl++"
		parts = append(parts[:len(parts)-2], "+")
	}
	for i, p := range parts {
		parts[i] = keyWord(p, len(parts) > 1)
	}
	return strings.Join(parts, "+")
}

// keyWord names one part of a key. A single letter keeps its case unless
// it follows a modifier (ctrl+c → Ctrl+C).
func keyWord(p string, combo bool) string {
	switch strings.ToLower(p) {
	case "ctrl":
		return "Ctrl"
	case "alt":
		return "Alt"
	case "shift":
		return "Shift"
	case "enter", "↵", "return":
		return "Enter"
	case "esc", "escape":
		return "Esc"
	case "tab", "⇥":
		return "Tab"
	case "space":
		return "Space"
	case "backspace":
		return "Backspace"
	case "pgup":
		return "PgUp"
	case "pgdown":
		return "PgDn"
	case "home":
		return "Home"
	case "end":
		return "End"
	}
	if len(p) >= 2 && (p[0] == 'f' || p[0] == 'F') && p[1] >= '0' && p[1] <= '9' {
		return "F" + p[1:]
	}
	if combo && len(p) == 1 {
		return strings.ToUpper(p)
	}
	return p
}

// HintBar renders "key label  key label …" footer hints on bg, e.g.
// HintBar(bg, "↑↓", "navigate", "↵", "open", "esc", "close").
func HintBar(bg lipgloss.Color, pairs ...string) string {
	c := Float()
	gap := lipgloss.NewStyle().Background(bg)
	label := lipgloss.NewStyle().Background(bg).Foreground(c.Dim)
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			b.WriteString(gap.Render("   "))
		}
		b.WriteString(Keycap(pairs[i], bg))
		b.WriteString(gap.Render(" "))
		b.WriteString(label.Render(pairs[i+1]))
	}
	return b.String()
}

// Pill renders text as a status-bar "pill": half-block caps in Nerd Font
// mode, a padded block otherwise. outer is the surface the pill sits on
// (the caps are drawn fg=pill bg on that surface). Not the rounded
// Powerline caps: without a Nerd terminal font, fallback draws those from
// whatever text font claims U+E0B4/E0B6 (see glyph_clash_test.go).
func Pill(text string, fg, bg, outer lipgloss.Color, bold bool) string {
	body := lipgloss.NewStyle().Foreground(fg).Background(bg).Bold(bold)
	if CurrentIconMode == IconModeNerdFont {
		cap := lipgloss.NewStyle().Foreground(bg).Background(outer)
		return cap.Render("▐") + body.Render(text) + cap.Render("▌")
	}
	return body.Render(" " + text + " ")
}

// PillWidth is the cell width Pill(text, …) will take.
func PillWidth(text string) int {
	return runewidth.StringWidth(text) + 2
}

// Hairline renders a horizontal rule of width w on bg. With truecolor it
// fades from the accent at the left into the hairline colour, which gives
// float titles a subtle lit edge; otherwise it is a flat hairline.
func Hairline(w int, bg Color256) string {
	if w <= 0 {
		return ""
	}
	c := Float()
	if !TrueColor() {
		return lipgloss.NewStyle().Foreground(c.Hairline).Background(LG(bg)).Render(strings.Repeat("─", w))
	}
	hair := Lerp(ToRGB(bg), ToRGB(BorderDefault), 0.75)
	head := w / 3
	if head < 1 {
		head = 1
	}
	stops := []RGB{ToRGB(BorderFocus), Lerp(ToRGB(BorderFocus), ToRGB(AccentLavender), 0.5), hair}
	out := GradientBar(head, "─", bg, stops...)
	if rest := w - head; rest > 0 {
		out += lipgloss.NewStyle().Foreground(hair.LG()).Background(LG(bg)).Render(strings.Repeat("─", rest))
	}
	return out
}

// WashLevel is one step of the accent tint ladder used for interactive
// states. Every widget uses these four steps, so the same state looks the
// same everywhere (one hover, one selection, ...).
type WashLevel float64

const (
	WashHover           WashLevel = 0.08 // pointer over a row or button
	WashSelected        WashLevel = 0.12 // selected / cursor row, pane not focused
	WashSelectedFocused WashLevel = 0.22 // selected / cursor row in the focused pane
	WashPressed         WashLevel = 0.30 // toggled on, active chip, cursor pill
)

// Wash is bg tinted toward the accent by level. On themes where the full
// step would drop body text under 4.5:1 (Solarized, Latte) the tint is
// lightened until text reads (wash_test.go).
func Wash(bg Color256, level WashLevel) lipgloss.Color {
	b, a, fg := ToRGB(bg), ToRGB(BorderFocus), ToRGB(TextPrimary)
	t := float64(level)
	for ; t > 0.02; t -= 0.02 {
		if contrastRatio(fg, Lerp(b, a, t)) >= 4.5 {
			break
		}
	}
	return lipgloss.Color(Lerp(b, a, t).Hex())
}

// PaneTitle is the colour of a pane's title: the accent while the pane has
// the keyboard, secondary text otherwise (UI review #6).
func PaneTitle(focused bool) lipgloss.Color {
	if focused {
		return LG(BorderFocus)
	}
	return LG(TextSecondary)
}

// PaneHairline is the line under a pane's header, w cells on bg. The pane
// with the keyboard gets the lit Hairline (accent fading out): termocode's
// mark of "you are here"; the others a quiet rule.
func PaneHairline(w int, bg Color256, focused bool) string {
	if w <= 0 {
		return ""
	}
	if focused {
		return Hairline(w, bg)
	}
	return lipgloss.NewStyle().Foreground(LG(BorderSubtle)).Background(LG(bg)).Render(strings.Repeat("─", w))
}

// HintSpan is where one "key label" pair of a HintBar sits, in cells from
// the bar's start (X1 exclusive). Key is the key as passed to HintBar.
type HintSpan struct {
	Key    string
	X0, X1 int
}

// HintSpans lays out HintBar(pairs...) without drawing it, so a click on a
// hint can do what the key does.
func HintSpans(pairs ...string) []HintSpan {
	var out []HintSpan
	x := 0
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			x += 3
		}
		w := ansi.StringWidth(" "+KeyLabel(pairs[i])+" ") + 1 + ansi.StringWidth(pairs[i+1])
		out = append(out, HintSpan{Key: pairs[i], X0: x, X1: x + w})
		x += w
	}
	return out
}
