package preview

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/mdview"
)

// The reader as a guide viewer (the in-app help): scroll to a heading,
// a link cursor (Tab / Shift+Tab, Enter), clicks on links, and Backspace
// to go back. The model only reports what the user picked; the app
// opens pages and keeps the history.

// LinkMsg is emitted when the user follows a link (Enter on the focused
// link, or a click on one). Dest is the link as written in the page.
type LinkMsg struct{ Dest string }

// BackMsg is emitted on Backspace or u: go back to the previous place.
type BackMsg struct{}

func linkCmd(dest string) tea.Cmd { return func() tea.Msg { return LinkMsg{Dest: dest} } }

// SetHeader sets the header: icon (drawn instead of the file icon; ""
// for none), title and badge.
func (m *Model) SetHeader(icon, title, badge string) {
	m.icon, m.iconSet, m.title, m.badge = icon, true, title, badge
}

// SetHints sets the keys on the left of the footer (see GuideHints).
// Under 60 cells only "esc close" and the position show.
func (m *Model) SetHints(left string) { m.hints = left }

// GuideHints is the footer of the guide reader: link keys only when the
// page has links, "backspace back" only when there is a place to go back.
func GuideHints(links, back bool) string {
	s := " esc close"
	if links {
		s += " · tab links · enter open"
	}
	if back {
		s += " · backspace back"
	}
	return s + " "
}

// Top is the first row in view.
func (m Model) Top() int {
	return min(m.top, max(len(m.content())-m.contentHeight(), 0))
}

// Marks returns the rows for the current width and where their headings
// and links are (empty for content without marks).
func (m Model) Marks() ([]string, mdview.Marks) {
	if m.doc == nil {
		return m.lines, mdview.Marks{}
	}
	rows := m.doc.Lines(m.w)
	return rows, m.doc.Marks()
}

// ScrollToRow puts row r at the top of the view (as far as the end of
// the content allows).
func (m *Model) ScrollToRow(r int) {
	m.top = r
	m.clamp()
	m.dropHiddenLink()
}

// ScrollToSlug scrolls to the heading with this anchor; false when the
// page has none.
func (m *Model) ScrollToSlug(slug string) bool {
	_, mk := m.Marks()
	for _, h := range mk.Headings {
		if h.Slug == slug {
			m.ScrollToRow(h.Row)
			return true
		}
	}
	return false
}

// FindBelow returns the first row at or below row whose plain text
// contains text, or -1.
func (m Model) FindBelow(row int, text string) int {
	rows, _ := m.Marks()
	for i := max(row, 0); i < len(rows); i++ {
		if strings.Contains(ansi.Strip(rows[i]), text) {
			return i
		}
	}
	return -1
}

// LinkAt returns the link drawn at screen cell (x, y), if any.
func (m Model) LinkAt(x, y int) (mdview.Link, bool) {
	if m.doc == nil || y < 1 || y > m.contentHeight() {
		return mdview.Link{}, false
	}
	row := m.Top() + y - 1
	_, mk := m.Marks()
	for _, l := range mk.Links {
		if l.Row == row && x >= l.X0 && x < l.X1 {
			return l, true
		}
	}
	return mdview.Link{}, false
}

// visibleLinks returns the IDs of the links with a row in view, in
// document order.
func (m Model) visibleLinks() []int {
	_, mk := m.Marks()
	top := m.Top()
	end := top + m.contentHeight()
	var ids []int
	seen := map[int]bool{}
	for _, l := range mk.Links {
		if l.Row >= top && l.Row < end && !seen[l.ID] {
			seen[l.ID] = true
			ids = append(ids, l.ID)
		}
	}
	return ids
}

// cycleLink moves the link cursor to the next (dir 1) or previous (-1)
// link in view, wrapping around. With no cursor yet, Tab picks the first
// link in view and Shift+Tab the last.
func (m *Model) cycleLink(dir int) {
	ids := m.visibleLinks()
	if len(ids) == 0 {
		m.cur = 0
		return
	}
	at := -1
	for i, id := range ids {
		if id == m.cur {
			at = i
		}
	}
	switch {
	case at < 0 && dir > 0:
		m.cur = ids[0]
	case at < 0:
		m.cur = ids[len(ids)-1]
	default:
		m.cur = ids[(at+dir+len(ids))%len(ids)]
	}
}

// focusedLink is the link under the cursor, when it is in view.
func (m Model) focusedLink() (mdview.Link, bool) {
	if m.cur == 0 {
		return mdview.Link{}, false
	}
	_, mk := m.Marks()
	top := m.Top()
	end := top + m.contentHeight()
	for _, l := range mk.Links {
		if l.ID == m.cur && l.Row >= top && l.Row < end {
			return l, true
		}
	}
	return mdview.Link{}, false
}

// dropHiddenLink clears the link cursor once its link is out of view.
func (m *Model) dropHiddenLink() {
	if _, ok := m.focusedLink(); !ok {
		m.cur = 0
	}
}

// highlightLinks draws the focused link's cells on row i in the
// selection colours. The cached row is not changed; if the splice would
// change the row's width, the row is drawn as is.
func (m Model) highlightLinks(line string, i int) string {
	_, mk := m.Marks()
	out := line
	for _, l := range mk.Links {
		if l.ID != m.cur || l.Row != i {
			continue
		}
		mid := ansi.Strip(ansi.Cut(out, l.X0, l.X1))
		s := ansi.Truncate(out, l.X0, "") + "\x1b[0m" + linkSel.Render(mid) + ansi.TruncateLeft(out, l.X1, "")
		if ansi.StringWidth(s) != ansi.StringWidth(line) {
			return line
		}
		out = s
	}
	return out
}
