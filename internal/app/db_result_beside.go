package app

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
	tea "github.com/charmbracelet/bubbletea"
)

// Two results side by side (DBX-26). Databases: Show Result Beside…
// picks another result tab; the grid view then draws the page's rows and
// that tab's rows on shared lines, half the width each. The beside grid
// is read-only: edits, sort and filters stay on the page's own tab.

const pickerKindDBBeside pickerKindEnum = 4311

// besideRun is the run of the tab drawn beside the page, or nil when none
// is chosen, the tab is gone, is the page's own or has no rows.
func (s *dbConsoleState) besideRun() *dbRun {
	if s.beside == nil || s.beside == s.curTab() || s.tabIndex(s.beside) < 0 {
		return nil
	}
	r := s.beside.run
	if r == nil || r.result.Err != nil || len(r.result.Columns) == 0 || r.plan != nil || r.redis != nil {
		return nil
	}
	return r
}

// databaseBesideItems are the other tabs with rows, then Close when one
// is beside.
func (m *Model) databaseBesideItems() []picker.Item {
	s := m.dbConsole()
	var items []picker.Item
	for i, t := range s.tabs {
		if t == s.curTab() || t.run == nil || t.run.result.Err != nil || len(t.run.result.Columns) == 0 || t.run.plan != nil || t.run.redis != nil {
			continue
		}
		it := picker.Item{ID: strconv.Itoa(i), Title: strconv.Itoa(i+1) + " " + dbRunLabel(t.run)}
		if t == s.beside {
			it.Hint = "beside now"
		}
		items = append(items, it)
	}
	if s.besideRun() != nil {
		items = append(items, picker.Item{ID: "close", Title: "Close side by side"})
	}
	return items
}

// databaseBesidePicker is Databases: Show Result Beside….
func (m *Model) databaseBesidePicker() tea.Cmd {
	if _, cmd, ok := m.databaseResultReady(); !ok {
		return cmd
	}
	items := m.databaseBesideItems()
	if len(items) == 0 {
		return m.pushToastDetail(toast.Info, "No other result with rows", "Run another statement in a new tab (pin this one first), then pick it here.")
	}
	m.picker = picker.NewItems("Show beside this result", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBBeside
	m.pickerOpen = true
	return nil
}

// databaseSetBeside applies the pick: a tab index, or "close".
func (m *Model) databaseSetBeside(id string) tea.Cmd {
	s := m.dbConsole()
	if id == "close" {
		s.beside = nil
		return m.databaseShowPage()
	}
	i, err := strconv.Atoi(id)
	if err != nil || i < 0 || i >= len(s.tabs) {
		return nil
	}
	s.beside = s.tabs[i]
	return m.databaseShowPage()
}

// databaseBesideView draws the page's rows and other's on shared lines.
func (m *Model) databaseBesideView(p *dbPage, s *dbConsoleState, other *dbRun, width int) {
	half := max((width-3)/2, 10)
	rows := s.pageSize()
	left := dbGridLines(s.pageResult(), half, rows)
	right := dbGridLines(other.result, half, rows)
	p.add(" Beside: " + ansi.Truncate(dbRunLabel(other), max(width-10, 10), dbGlyphMore.String()))
	p.hl(len(p.lines), 0, len(p.lines[len(p.lines)-1]), "TermocodeHttpSummary")
	bar := " " + dbGlyphVBar.String() + " "
	for i := 0; i < max(len(left), len(right)); i++ {
		var l, r string
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = strings.TrimPrefix(right[i], "  ")
		}
		l = ansi.Truncate(l, half, "")
		p.add(l + strings.Repeat(" ", max(0, half-ansi.StringWidth(l))) + bar + r)
	}
}
