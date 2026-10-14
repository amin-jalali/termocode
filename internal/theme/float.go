package theme

import (
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
		SelBg:     Mix(BgFloat, BorderFocus, 0.24),
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
	return lipgloss.NewStyle().Background(c.KeycapBg).Foreground(c.KeycapFg).Bold(true).Render(" " + key + " ")
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
