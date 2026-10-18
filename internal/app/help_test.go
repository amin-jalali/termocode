package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/preview"
)

func helpTestModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := New()
	m.w, m.h = 100, 40
	return m
}

// helpFeed runs cmd and delivers the viewer's own messages (pages,
// links, back, close) back into the model, as the runtime would.
func helpFeed(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for n := 0; cmd != nil && n < 20; n++ {
		msg := cmd()
		cmd = nil
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				m = helpFeed(t, m, c)
			}
			return m
		}
		switch msg.(type) {
		case PreviewMsg, preview.LinkMsg, preview.BackMsg, preview.CloseMsg:
			m, cmd = m.update(msg)
		}
	}
	return m
}

// helpSend delivers msg to the model and runs what it returns.
func helpSend(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	m, cmd := m.update(msg)
	return helpFeed(t, m, cmd)
}

func toastText(m Model) string {
	var b strings.Builder
	for _, c := range m.toast.Cards(120) {
		b.WriteString(ansi.Strip(strings.Join(c.Lines, "\n")))
		b.WriteByte('\n')
	}
	return b.String()
}

// headingRow is the row of the heading with this anchor in the viewer.
func headingRow(t *testing.T, m Model, slug string) int {
	t.Helper()
	_, mk := m.preview.Marks()
	for _, h := range mk.Headings {
		if h.Slug == slug {
			return h.Row
		}
	}
	t.Fatalf("no heading #%s in the viewer", slug)
	return -1
}

func viewerRows(m Model) []string {
	return strings.Split(ansi.Strip(m.preview.View()), "\n")
}

// Spec: a topic opens the guide page in the reader with the "Guide"
// badge, scrolled so its heading is the first content row.
func TestHelpOpensAtAnchor(t *testing.T) {
	m := helpTestModel(t)
	m = helpFeed(t, m, m.openHelp(help.Loc{Page: "guides/ai-use.md", Anchor: "chat-panel"}))
	if !m.previewOpen {
		t.Fatal("viewer did not open")
	}
	rows := viewerRows(m)
	if !strings.Contains(rows[0], "Use AI") || !strings.Contains(rows[0], "Guide") {
		t.Errorf("header = %q", rows[0])
	}
	if got, want := m.preview.Top(), headingRow(t, m, "chat-panel"); got != want {
		t.Errorf("top = %d, want the heading row %d", got, want)
	}
	if !strings.Contains(rows[1], "Chat panel") {
		t.Errorf("first content row = %q, want the Chat panel heading", rows[1])
	}
	if !strings.Contains(rows[len(rows)-1], "tab links") || strings.Contains(rows[len(rows)-1], "backspace") {
		t.Errorf("footer = %q", rows[len(rows)-1])
	}
}

// Spec: a find text brings its row into view below the heading.
func TestHelpOpensAtFindRow(t *testing.T) {
	m := helpTestModel(t)
	m.h = 12
	m = helpFeed(t, m, m.openHelp(help.Loc{Page: "reference/settings.md", Anchor: "editor", Find: "`persistent_session`"}))
	found := false
	for _, r := range viewerRows(m)[1:] {
		if strings.Contains(r, "persistent_session") {
			found = true
		}
	}
	if !found {
		t.Errorf("the persistent_session row is not in view:\n%s", strings.Join(viewerRows(m), "\n"))
	}
}

// Spec: a missing anchor opens the page at the top and says so.
func TestHelpMissingAnchor(t *testing.T) {
	m := helpTestModel(t)
	m = helpFeed(t, m, m.openHelp(help.Loc{Page: "guides/ai-use.md", Anchor: "no-such-part"}))
	if !m.previewOpen || m.preview.Top() != 0 {
		t.Fatalf("open=%v top=%d, want the page at the top", m.previewOpen, m.preview.Top())
	}
	if !strings.Contains(toastText(m), "Section moved") {
		t.Errorf("toast = %q", toastText(m))
	}
}

// Spec: a relative link opens the target page inside the viewer;
// Backspace goes back to the same place.
func TestHelpLinkAndBack(t *testing.T) {
	m := helpTestModel(t)
	m = helpFeed(t, m, m.openHelp(help.Loc{Page: "guides/ai-use.md", Anchor: "agent-let-the-chat-use-tools"}))
	top := m.preview.Top()
	m = helpSend(t, m, preview.LinkMsg{Dest: "../reference/ai-tools.md"})
	if m.help.cur.Page != "reference/ai-tools.md" {
		t.Fatalf("page = %q, want reference/ai-tools.md", m.help.cur.Page)
	}
	rows := viewerRows(m)
	if !strings.Contains(rows[0], "AI tools") || m.preview.Top() != 0 {
		t.Errorf("header = %q top = %d", rows[0], m.preview.Top())
	}
	if !strings.Contains(rows[len(rows)-1], "backspace back") {
		t.Errorf("footer has no back key: %q", rows[len(rows)-1])
	}
	m = helpSend(t, m, preview.BackMsg{})
	if m.help.cur.Page != "guides/ai-use.md" || m.preview.Top() != top {
		t.Errorf("back: page %q top %d, want guides/ai-use.md top %d", m.help.cur.Page, m.preview.Top(), top)
	}
	if len(m.help.stack) != 0 {
		t.Errorf("stack = %v, want empty", m.help.stack)
	}
	// Back with an empty stack keeps the page.
	m = helpSend(t, m, preview.BackMsg{})
	if !m.previewOpen || m.help.cur.Page != "guides/ai-use.md" {
		t.Errorf("back on an empty stack changed the view: open=%v page=%q", m.previewOpen, m.help.cur.Page)
	}
}

// Spec: "#frag" scrolls on the same page and can be undone.
func TestHelpSamePageAnchor(t *testing.T) {
	m := helpTestModel(t)
	m = helpFeed(t, m, m.openHelp(help.Loc{Page: "guides/ai-use.md"}))
	m = helpSend(t, m, preview.LinkMsg{Dest: "#review-ai-changes"})
	if got, want := m.preview.Top(), headingRow(t, m, "review-ai-changes"); got != want {
		t.Errorf("top = %d, want %d", got, want)
	}
	m = helpSend(t, m, preview.BackMsg{})
	if m.preview.Top() != 0 {
		t.Errorf("back: top = %d, want 0", m.preview.Top())
	}
}

// Spec: a web link goes to the system browser; the viewer stays.
func TestHelpWebLink(t *testing.T) {
	var opened string
	old := osOpen
	osOpen = func(u string) error { opened = u; return nil }
	defer func() { osOpen = old }()
	m := helpTestModel(t)
	m = helpFeed(t, m, m.openHelp(help.Loc{Page: "guides/ai-use.md"}))
	m = helpSend(t, m, preview.LinkMsg{Dest: "https://example.com/x"})
	if opened != "https://example.com/x" {
		t.Errorf("osOpen got %q", opened)
	}
	if !m.previewOpen || m.help.cur.Page != "guides/ai-use.md" || len(m.help.stack) != 0 {
		t.Errorf("viewer changed: open=%v page=%q stack=%v", m.previewOpen, m.help.cur.Page, m.help.stack)
	}
	// Other schemes never reach the system.
	opened = ""
	m = helpSend(t, m, preview.LinkMsg{Dest: "javascript:alert(1)"})
	if opened != "" {
		t.Errorf("osOpen got %q for a javascript: link", opened)
	}
}

// Spec: a page that is not in the binary shows its website address.
func TestHelpLinkNotEmbedded(t *testing.T) {
	m := helpTestModel(t)
	m = helpFeed(t, m, m.openHelp(help.Loc{Page: "guides/search.md"}))
	m = helpSend(t, m, preview.LinkMsg{Dest: "../concepts/find-in-files.md#x"})
	if m.help.cur.Page != "guides/search.md" || len(m.help.stack) != 0 {
		t.Errorf("viewer moved: page=%q stack=%v", m.help.cur.Page, m.help.stack)
	}
	txt := toastText(m)
	if !strings.Contains(txt, "Not in the built-in guide") || !strings.Contains(txt, "amin-jalali.github.io") {
		t.Errorf("toast = %q", txt)
	}
}

// Spec: the F7 preview has no history: Backspace and links do nothing.
func TestHelpF7PreviewIgnoresBack(t *testing.T) {
	m := helpTestModel(t)
	m = helpFeed(t, m, m.openHelp(help.Loc{Page: "guides/ai-use.md"}))
	m = helpSend(t, m, preview.LinkMsg{Dest: "ai-setup.md"})
	m = helpSend(t, m, preview.CloseMsg{})
	if m.previewOpen || m.help.cur.Page != "" || len(m.help.stack) != 0 {
		t.Fatalf("close kept help state: open=%v %+v", m.previewOpen, m.help)
	}
	doc := markdownDoc("# Notes\n\n"+strings.Repeat("text\n\n", 60), "")
	m = helpSend(t, m, PreviewMsg{Title: "notes.md", Doc: doc, Badge: "Preview"})
	m = helpSend(t, m, tea.KeyMsg{Type: tea.KeyDown})
	before := m.preview.View()
	m = helpSend(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	m = helpSend(t, m, preview.LinkMsg{Dest: "ai-setup.md"})
	if !m.previewOpen || m.preview.View() != before || m.help.cur.Page != "" {
		t.Errorf("F7 preview changed on Backspace / link: open=%v page=%q", m.previewOpen, m.help.cur.Page)
	}
}

// Spec: a click on a link in the viewer resolves to that link (the
// hit map and the real click share the route), not to "overlay".
func TestHelpClickOnLink(t *testing.T) {
	m := helpTestModel(t)
	m = helpFeed(t, m, m.openHelp(help.Loc{Page: "guides/ai-use.md"}))
	_, mk := m.preview.Marks()
	var l *struct {
		x, y int
		dest string
	}
	for _, k := range mk.Links {
		if k.Row-m.preview.Top()+1 >= 1 && k.Row-m.preview.Top()+1 < m.h-1 && k.Dest == "ai-setup.md" {
			l = &struct {
				x, y int
				dest string
			}{k.X0, k.Row - m.preview.Top() + 1, k.Dest}
			break
		}
	}
	if l == nil {
		t.Fatal("no ai-setup.md link in view")
	}
	if got := m.probeClick(l.x, l.y); got != "preview:link:ai-setup.md" {
		t.Errorf("probeClick = %q", got)
	}
	if got := m.probeClick(0, 3); got != "overlay" {
		t.Errorf("probeClick off a link = %q, want overlay", got)
	}
	m = helpSend(t, m, tea.MouseMsg{X: l.x, Y: l.y, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.help.cur.Page != "guides/ai-setup.md" {
		t.Errorf("click: page = %q, want guides/ai-setup.md", m.help.cur.Page)
	}
}

// Spec: "Help: Open Guide…" lists every built-in page in site order;
// Enter opens the page at the top with a fresh history.
func TestHelpGuidePicker(t *testing.T) {
	m := helpTestModel(t)
	cmd := m.dispatchPaletteAction("help-guide")
	m = helpFeed(t, m, cmd)
	if !m.pickerOpen || m.pickerKind != pickerKindHelpGuide {
		t.Fatalf("picker open=%v kind=%v", m.pickerOpen, m.pickerKind)
	}
	items := helpGuideItems()
	pages := help.Pages()
	if len(items) != len(pages) {
		t.Fatalf("%d items for %d pages", len(items), len(pages))
	}
	for i, p := range pages {
		if items[i].Title != p.Title {
			t.Errorf("item %d = %q, want %q", i, items[i].Title, p.Title)
		}
	}
	m.pickerOpen = false
	var id string
	for _, it := range items {
		if it.Title == "Use AI" {
			id = it.ID
		}
	}
	m = helpFeed(t, m, m.handlePickerSelect(picker.SelectMsg{ID: id, Title: "Use AI"}))
	if !m.previewOpen || m.help.cur.Page != "guides/ai-use.md" || m.preview.Top() != 0 {
		t.Errorf("open=%v page=%q top=%d", m.previewOpen, m.help.cur.Page, m.preview.Top())
	}
}
