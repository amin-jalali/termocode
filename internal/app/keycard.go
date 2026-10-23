package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/help"
)

// ── Key cards ────────────────────────────────────────────────────────────
//
// "?" in Databases, Containers, the import panel and the MCP server status
// screen opens one card: the keys of that view as a table (key, what it
// does) and an Open guide button for the guide page of the view. The card
// stays open until Esc or Close; a toast would be cut and gone after a few
// seconds. All views use openKeyCard; each only supplies its rows.

// confirmKindKeyCard is a key card (keycard.go).
const confirmKindKeyCard confirmKindEnum = 2100

// openKeyCard shows the key card of a view. back, when set, runs after the
// card closes (a screen that was under it comes back).
func (m *Model) openKeyCard(title string, rows []confirm.KeyRow, guide help.Loc, back func(*Model) tea.Cmd) tea.Cmd {
	m.confirm = confirm.New(title, "", []confirm.Button{
		{ID: "guide", Title: "Open guide", Style: confirm.StylePrimary},
		{ID: "close", Title: "Close"},
	}).WithKeys(rows)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindKeyCard
	m.keyCardGuide = guide
	m.keyCardBack = back
	return nil
}

// answerKeyCard runs a key card button: Open guide opens the page at the
// view's section, Close goes back to what was under the card.
func (m *Model) answerKeyCard(id string) tea.Cmd {
	back := m.keyCardBack
	m.keyCardBack = nil
	if id == "guide" {
		return m.openHelp(m.keyCardGuide)
	}
	if back != nil {
		return back(m)
	}
	return nil
}

// closeKeyCard is Esc on a key card.
func (m *Model) closeKeyCard() tea.Cmd { return m.answerKeyCard("close") }

// keyRows builds rows from key, text pairs; a one-element pair is a heading.
func keyRows(pairs ...[]string) []confirm.KeyRow {
	out := make([]confirm.KeyRow, 0, len(pairs))
	for _, p := range pairs {
		if len(p) == 1 {
			out = append(out, confirm.KeyRow{What: p[0]})
			continue
		}
		out = append(out, confirm.KeyRow{Key: p[0], What: p[1]})
	}
	return out
}
