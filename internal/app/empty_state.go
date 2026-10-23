package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

// emptyState is what a panel shows when it has nothing to list (UI review
// #10): one look everywhere instead of a different sentence per panel. An
// empty or error state is always: a title, one plain sentence, then the
// action as a labelled button with its key, for example
//
//	No databases yet
//	Add a connection, or put DATABASE_URL in .env.
//	[Add connection (a)]
type emptyState struct {
	Glyph theme.Icon // drawn dim above the title; zero = none
	Title string     // short, e.g. "No problems"
	Hint  string     // one sentence: what to do
	Key   string     // optional key to press, shown as a keycap ("f8")
	Label string     // optional name of the action: with Key it is drawn as a button, [Label (key)]
}

// renderEmptyState draws es centred in w×h cells on bgTok: exactly h rows,
// each exactly w wide. When the area is too small the glyph goes first,
// then the key, then hint lines; the title always stays.
func renderEmptyState(w, h int, bgTok theme.Color256, es emptyState) []string {
	bg := lipgloss.NewStyle().Background(theme.LG(bgTok))
	center := func(s string) string {
		sw := ansi.StringWidth(s)
		if sw > w {
			s, sw = ansi.Truncate(s, w, "…"), w
		}
		l := (w - sw) / 2
		return bg.Render(strings.Repeat(" ", l)) + s + bg.Render(strings.Repeat(" ", w-l-sw))
	}
	title := []string{center(theme.FgBg(theme.TextSecondary, bgTok).Bold(true).Render(es.Title))}
	var glyph, hint, key []string
	if g := es.Glyph.String(); g != "" {
		glyph = []string{center(theme.FgBg(theme.TextDim, bgTok).Render(g)), center("")}
	}
	if es.Hint != "" {
		for _, l := range wrapPlain(es.Hint, max(w-4, 8)) {
			hint = append(hint, center(theme.FgBg(theme.TextDim, bgTok).Render(l)))
		}
	}
	if es.Key != "" {
		cap := theme.Keycap(es.Key, theme.LG(bgTok))
		if es.Label != "" {
			cap = theme.FgBg(theme.TextPrimary, bgTok).Bold(true).Render("[" + es.Label + " (" + es.Key + ")]")
		}
		key = []string{center(""), center(cap)}
	}
	block := append(append(append(append([]string{}, glyph...), title...), hint...), key...)
	if len(block) > h && len(glyph) > 0 { // the glyph is decoration: it goes first
		glyph, block = nil, append(append(append([]string{}, title...), hint...), key...)
	}
	if len(block) > h && len(key) > 0 {
		key, block = nil, append(append([]string{}, title...), hint...)
	}
	for len(block) > h && len(hint) > 0 {
		hint = hint[:len(hint)-1]
		block = append(append([]string{}, title...), hint...)
	}
	if len(block) > h {
		block = block[:h]
	}
	top := (h - len(block)) / 2
	rows := make([]string, 0, h)
	for i := 0; i < top; i++ {
		rows = append(rows, center(""))
	}
	rows = append(rows, block...)
	for len(rows) < h {
		rows = append(rows, center(""))
	}
	return rows
}

// scmNotRepoEmpty is Source Control's empty state outside a git repository.
func scmNotRepoEmpty(w, h int) []string {
	return renderEmptyState(w, h, theme.BgSidebar, emptyState{Glyph: theme.IconGit, Title: "Not a git repository",
		Hint: "Run git init in a terminal to use Source Control."})
}
