package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/mdview"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/preview"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The in-app guide: the pages docs/embed.go puts in the binary, shown in
// the preview reader (the F7 view) with a "Guide" badge. It opens a page
// at a heading, follows links between pages and keeps a history for
// Backspace. Web links go to the system browser; a page that is not in
// the binary (an ADR, concepts/…) shows its website address.

// helpState is the guide viewer's place and history. cur.Page is "" when
// the open preview (if any) is not the guide.
type helpState struct {
	cur   help.Loc
	doc   *preview.Doc // the viewer's Doc: tells the guide from other previews
	stack []helpPlace
}

// helpPlace is a place to go back to: the page and the row at the top.
type helpPlace struct {
	loc help.Loc
	top int
}

// helpPage is the part of a PreviewMsg that opens a guide page.
type helpPage struct {
	loc  help.Loc
	push bool // put the current place on the history first
	top  int  // with back: the row to restore
	back bool // a place from the history: restore top, ignore the anchor
}

// helpActive reports whether the open preview is the guide viewer.
func (m Model) helpActive() bool {
	return m.previewOpen && m.help.cur.Page != "" && m.help.doc != nil && m.preview.Doc() == m.help.doc
}

// openHelp opens the guide at l with a fresh history.
func (m *Model) openHelp(l help.Loc) tea.Cmd {
	m.help.stack = nil
	return m.helpPageCmd(helpPage{loc: l})
}

// helpPageCmd loads and renders a page off the UI goroutine and returns
// it as a PreviewMsg; a page that can not be loaded becomes a toast.
func (m *Model) helpPageCmd(hp helpPage) tea.Cmd {
	pg, err := help.Load(hp.loc.Page)
	if err != nil {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Warn, "Not in the built-in guide", help.SiteURL(hp.loc))
		return c
	}
	width := m.w
	if width <= 0 {
		width = 80
	}
	rtl := rtlOn()
	return func() tea.Msg {
		doc := preview.NewMarkedDoc(func(w int) ([]string, mdview.Marks) {
			return mdview.RenderDoc(pg.Source, mdview.Options{Width: w, RTL: rtl, Links: true})
		})
		doc.Lines(width) // render here; View reuses it
		return PreviewMsg{Title: pg.Title, Doc: doc, Badge: "Guide", help: &hp}
	}
}

// helpIcon is the header mark of the guide viewer.
func helpIcon() string {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return "(i)"
	}
	return "ⓘ"
}

// showHelpPage puts a rendered guide page in the viewer (PreviewMsg with
// help set).
func (m *Model) showHelpPage(msg PreviewMsg) tea.Cmd {
	hp := *msg.help
	if hp.push && m.helpActive() {
		m.help.stack = append(m.help.stack, helpPlace{loc: m.help.cur, top: m.preview.Top()})
	} else if !hp.back && !hp.push {
		m.help.stack = nil
	}
	m.preview = preview.NewReader(msg.Title, msg.Badge, msg.Doc)
	m.preview.SetHeader(helpIcon(), msg.Title, msg.Badge)
	m.preview.SetSize(m.w, m.h)
	m.previewOpen = true
	m.help.cur, m.help.doc = hp.loc, msg.Doc
	var cmd tea.Cmd
	if hp.back {
		m.preview.ScrollToRow(hp.top)
	} else {
		cmd = m.helpScrollTo(hp.loc)
	}
	m.helpHints()
	return cmd
}

// helpScrollTo brings l's heading to the top and its find text (if any)
// into view. A missing heading leaves the page at the top with a toast.
func (m *Model) helpScrollTo(l help.Loc) tea.Cmd {
	if l.Anchor == "" {
		m.preview.ScrollToRow(0)
		return nil
	}
	if !m.preview.ScrollToSlug(l.Anchor) {
		m.preview.ScrollToRow(0)
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, "Section moved", l.Anchor)
		return c
	}
	if l.Find == "" {
		return nil
	}
	top := m.preview.Top()
	r := m.preview.FindBelow(top, strings.ReplaceAll(l.Find, "`", ""))
	if view := max(m.h-2, 1); r >= top+view {
		m.preview.ScrollToRow(r - view/2)
	}
	return nil
}

// helpHints sets the footer: link keys when the page has links, the
// back key when there is history.
func (m *Model) helpHints() {
	_, mk := m.preview.Marks()
	m.preview.SetHints(preview.GuideHints(len(mk.Links) > 0, len(m.help.stack) > 0))
}

// onHelpLink follows a link the user picked in the guide viewer.
func (m *Model) onHelpLink(dest string) tea.Cmd {
	if !m.helpActive() {
		return nil
	}
	low := strings.ToLower(dest)
	if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "mailto:") {
		var c tea.Cmd
		if err := osOpen(dest); err != nil {
			m.toast, c = m.toast.PushDetail(toast.Warn, "Open the link in your browser", dest)
			return c
		}
		m.toast, c = m.toast.PushDetail(toast.Info, "Opened", dest)
		return c
	}
	l, ok := help.RelLink(m.help.cur.Page, dest)
	if !ok {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, "This link can not open here", dest)
		return c
	}
	if l.Page == m.help.cur.Page {
		m.help.stack = append(m.help.stack, helpPlace{loc: m.help.cur, top: m.preview.Top()})
		m.help.cur = l
		cmd := m.helpScrollTo(l)
		m.helpHints()
		return cmd
	}
	if !help.Has(l.Page) {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, "Not in the built-in guide", help.SiteURL(l))
		return c
	}
	return m.helpPageCmd(helpPage{loc: l, push: true})
}

// onHelpBack goes back to the previous place in the guide viewer. It does
// nothing in other previews (F7 has no history) or with no history.
func (m *Model) onHelpBack() tea.Cmd {
	if !m.helpActive() || len(m.help.stack) == 0 {
		return nil
	}
	p := m.help.stack[len(m.help.stack)-1]
	m.help.stack = m.help.stack[:len(m.help.stack)-1]
	if p.loc.Page == m.help.cur.Page {
		m.help.cur = p.loc
		m.preview.ScrollToRow(p.top)
		m.helpHints()
		return nil
	}
	return m.helpPageCmd(helpPage{loc: p.loc, back: true, top: p.top})
}

// closeHelp forgets the guide's place when the viewer closes.
func (m *Model) closeHelp() { m.help = helpState{} }

// helpGuideItems are the rows of "Help: Open Guide…": every built-in
// page in site order, its section on the right.
func helpGuideItems() []picker.Item {
	pages := help.Pages()
	items := make([]picker.Item, 0, len(pages))
	for _, p := range pages {
		items = append(items, picker.Item{ID: "help:" + p.Path, Title: p.Title, Hint: p.Section})
	}
	return items
}

// openHelpGuidePicker opens "Help: Open Guide…".
func (m *Model) openHelpGuidePicker() {
	m.picker = picker.NewItems(" Guide (Enter opens) ", helpGuideItems())
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindHelpGuide
}

// onHelpGuidePicked opens the picked page at the top.
func (m *Model) onHelpGuidePicked(id string) tea.Cmd {
	page, ok := strings.CutPrefix(id, "help:")
	if !ok {
		return nil
	}
	return m.openHelp(help.Loc{Page: page})
}
