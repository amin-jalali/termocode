package profileform

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// stepAIRow draws the line under a step's Model button: for a sign-in
// AI what it uses; for a step whose AI is not ready why, with a Fix pill
// that opens the Add AI card at that AI (the form never touches a key or
// a login).
func (m Model) stepAIRow(b *built, i, indent, innerW int, bg, text, dim, danger, accent lipgloss.Style, f theme.FloatColors) {
	c := m.steps[i].ch
	need := m.needs(i)
	mk := bg.Render("  ")
	it := item{iSFix, i}
	foc := m.foc == it
	if foc {
		mk = accent.Render("▸ ")
	}
	lead := mk + bg.Render(strings.Repeat(" ", indent))
	x := 2 + indent
	if need == "" {
		if c.cli == "" || ai.CLITitle(c.cli) == "" {
			return
		}
		s := "uses your " + ai.CLITitle(c.cli) + " sign-in · no API key"
		if m.cfg.CLIs[c.cli] == CLIReady {
			s = theme.GlyphOK.String() + " " + s
		}
		room := max(innerW-x-2, 8)
		b.body = append(b.body, line{s: padTo(lead+dim.Render(runewidth.Truncate(s, room, "…")), innerW, bg)})
		return
	}
	y0 := len(b.body)
	lbl := "  Fix  "
	st := lipgloss.NewStyle().Background(f.KeycapBg).Foreground(f.Primary)
	if foc {
		st = lipgloss.NewStyle().Background(f.Primary).Foreground(f.PrimaryFg).Bold(true)
	}
	room := max(innerW-x-2-runewidth.StringWidth(lbl)-4, 8)
	head := danger.Render(theme.GlyphFail.String()+" ") + text.Render(runewidth.Truncate(need, room, "…"))
	px := x + lipgloss.Width(head) + 2
	s := lead + head + bg.Render("  ") + st.Render(lbl)
	b.body = append(b.body, line{s: padTo(s, innerW, bg), tg: []target{{x: px, w: runewidth.StringWidth(lbl), it: it}}})
	b.ranges = append(b.ranges, rng{it: it, y0: y0, y1: len(b.body) - 1})
}
