package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/theme"
)

// ── "/" filter of the Databases tree ─────────────────────────────────────
//
// The same inline filter as Containers (inline_filter.go): "/" starts
// typing, every key narrows the tree to the rows whose name contains the
// text (ignoring case), Enter keeps the filter, Esc clears it. The text
// shows in the title row. "Databases: Filter…" in the palette starts it too.

// databaseFilterKey runs a key through the filter. It returns true when the
// filter took the key.
func (m *Model) databaseFilterKey(k tea.KeyMsg) bool {
	s := m.databases
	text, typing, handled := inlineFilterStep(s.filter, s.filterTyping, k)
	s.filterTyping = typing
	if !handled {
		return false
	}
	m.databaseSetFilter(text)
	if !typing { // Enter or Esc: the filter is final; keep it for the next start
		m.databasesSaveSession()
	}
	return true
}

// databaseSetFilter changes the filter text. The cursor stays on its row
// when that row is still shown, else it goes to the first row.
func (m *Model) databaseSetFilter(text string) {
	s := m.databases
	s.touch()
	if s.filter == text {
		return
	}
	cur, had := m.databaseCurrentRow()
	s.filter = text
	s.cursor = 0
	if had {
		for i, r := range s.rows() {
			if r.path == cur.path {
				s.cursor = i
				break
			}
		}
	}
	s.scroll = 0
}

// dbSidebarHeader is the DATABASES title row. While a filter is on, the
// title carries it ("DATABASES · /text▏") as the Containers title does.
func dbSidebarHeader(w int, filter string, typing bool) string {
	text := inlineFilterTitle(filter, typing, theme.CurrentIconMode == theme.IconModeASCII)
	if text == "" {
		return sidebarHeader(w, "DATABASES", "")
	}
	fill := theme.Bg(theme.BgSidebar)
	label := theme.FgBg(theme.TextSecondary, theme.BgSidebar).Bold(true)
	muted := theme.FgBg(theme.TextMuted, theme.BgSidebar)
	more := muted.Render(theme.GlyphMore.String()) + fill.Render(" ")
	left := fill.Render("  ") + label.Render("DATABASES") + muted.Render(" · ")
	room := w - lipgloss.Width(left) - lipgloss.Width(more) - 1
	if room < 3 {
		return sidebarHeader(w, "DATABASES", "")
	}
	text = runewidth.Truncate(text, room, "…")
	left += theme.FgBg(theme.TextSecondary, theme.BgSidebar).Render(text)
	gap := max(w-lipgloss.Width(left)-lipgloss.Width(more), 0)
	return left + fill.Render(strings.Repeat(" ", gap)) + more
}
