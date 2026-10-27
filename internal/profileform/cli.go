package profileform

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// cliRow draws the line under a CLI step's Model choice: what the step
// uses, or why it cannot run with an Install / Log in pill (CS-5).
func (m Model) cliRow(b *built, i int, cli string, indent, innerW int, bg, text, dim, danger, accent lipgloss.Style, f theme.FloatColors) {
	title := ai.CLITitle(cli)
	if title == "" {
		title = cli
	}
	it := item{iSCLI, i}
	foc := m.foc == it
	y0 := len(b.body)
	mk := bg.Render("  ")
	if foc {
		mk = accent.Render("▸ ")
	}
	lead := mk + bg.Render(strings.Repeat(" ", indent))
	x := 2 + indent
	var msg, pill string
	switch m.cfg.CLIs[cli] {
	case CLIMissing:
		msg, pill = title+" is not installed", "Install"
	case CLILoggedOut:
		msg, pill = title+" is not logged in", "Log in"
	}
	if pill == "" {
		s := theme.GlyphOK.String() + " uses your " + title + " login (subscription) · no API key"
		if m.cfg.CLIs[cli] == CLIUnknown {
			s = "uses your " + title + " login (subscription) · no API key"
		}
		room := max(innerW-x-2, 8)
		b.body = append(b.body, line{s: padTo(lead+dim.Render(runewidth.Truncate(s, room, "…")), innerW, bg)})
		return
	}
	lbl := "  " + pill + "  "
	st := lipgloss.NewStyle().Background(f.KeycapBg).Foreground(f.Primary)
	if foc {
		st = lipgloss.NewStyle().Background(f.Primary).Foreground(f.PrimaryFg).Bold(true)
	}
	room := max(innerW-x-2-runewidth.StringWidth(lbl)-4, 8)
	head := danger.Render(theme.GlyphFail.String()+" ") + text.Render(runewidth.Truncate(msg, room, "…"))
	px := x + lipgloss.Width(head) + 2
	s := lead + head + bg.Render("  ") + st.Render(lbl)
	b.body = append(b.body, line{s: padTo(s, innerW, bg), tg: []target{{x: px, w: runewidth.StringWidth(lbl), it: it}}})
	b.ranges = append(b.ranges, rng{it: it, y0: y0, y1: len(b.body) - 1})
}
