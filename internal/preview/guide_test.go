package preview_test

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/mdview"
	"github.com/amin-jalali/termocode/internal/preview"
	"github.com/amin-jalali/termocode/internal/testkit"
	"github.com/amin-jalali/termocode/internal/theme"
)

// guideSrc has headings far apart and links spread over the page.
func guideSrc() string {
	var b strings.Builder
	b.WriteString("# Use AI\n\nSee [Set up AI](ai-setup.md) first.\n\n")
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "Filler paragraph %d.\n\n", i)
	}
	b.WriteString("## Chat panel\n\nOpen it. Read [Keys](../reference/keys.md#ai) and [the site](https://example.com).\n\n")
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "More filler %d.\n\n", i)
	}
	b.WriteString("## Last\n\nThe `theme` row is here.\n\nEnd [back up](#use-ai).\n")
	return b.String()
}

func guide(t *testing.T, w, h int) preview.Model {
	t.Helper()
	src := guideSrc()
	doc := preview.NewMarkedDoc(func(width int) ([]string, mdview.Marks) {
		return mdview.RenderDoc(src, mdview.Options{Width: width, Links: true})
	})
	m := preview.NewReader("Use AI", "Guide", doc)
	m.SetSize(w, h)
	return m
}

func key(m preview.Model, k tea.KeyMsg) (preview.Model, tea.Msg) {
	m, cmd := m.Update(k)
	if cmd == nil {
		return m, nil
	}
	return m, cmd()
}

var (
	tab      = tea.KeyMsg{Type: tea.KeyTab}
	shiftTab = tea.KeyMsg{Type: tea.KeyShiftTab}
	enter    = tea.KeyMsg{Type: tea.KeyEnter}
	bksp     = tea.KeyMsg{Type: tea.KeyBackspace}
)

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// firstContentRow is the plain text of the first row under the header.
func firstContentRow(m preview.Model) string {
	return strings.TrimSpace(ansi.Strip(strings.Split(m.View(), "\n")[1]))
}

func TestScrollToSlug(t *testing.T) {
	m := guide(t, 80, 20)
	if !m.ScrollToSlug("chat-panel") {
		t.Fatal("chat-panel not found")
	}
	if got := firstContentRow(m); got != "Chat panel" {
		t.Errorf("first content row = %q, want the heading", got)
	}
	if m.ScrollToSlug("nope") {
		t.Error("a missing anchor scrolled")
	}
	// Near the end the view clamps: the heading is in view, not on top.
	if !m.ScrollToSlug("last") {
		t.Fatal("last not found")
	}
	if !strings.Contains(ansi.Strip(m.View()), "Last") {
		t.Error("the last heading is not in view")
	}
	m.ScrollToRow(0)
	if got := firstContentRow(m); got != "" {
		t.Errorf("row 0 is the blank top row, got %q", got)
	}
}

func TestFindBelow(t *testing.T) {
	m := guide(t, 80, 20)
	m.ScrollToSlug("chat-panel")
	_, mk := m.Marks()
	last := mk.Headings[len(mk.Headings)-1].Row
	r := m.FindBelow(mk.Headings[1].Row, "theme")
	if r <= last {
		t.Fatalf("FindBelow = %d, want below row %d", r, last)
	}
	if m.FindBelow(r+1, "theme") != -1 {
		t.Error("found twice")
	}
	if m.FindBelow(0, "no such text") != -1 {
		t.Error("found missing text")
	}
}

func TestTabCyclesLinksInView(t *testing.T) {
	m := guide(t, 80, 20)
	m.ScrollToSlug("chat-panel")
	var got []string
	for i := 0; i < 3; i++ {
		var msg tea.Msg
		m, msg = key(m, tab)
		if msg != nil {
			t.Fatalf("Tab emitted %#v", msg)
		}
		_, out := key(m, enter)
		lm, ok := out.(preview.LinkMsg)
		if !ok {
			t.Fatalf("Enter after Tab gave %#v", out)
		}
		got = append(got, lm.Dest)
	}
	// Two links in view (the "Set up AI" link above the top is skipped),
	// then the cursor wraps.
	want := []string{"../reference/keys.md#ai", "https://example.com", "../reference/keys.md#ai"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("Tab order = %q, want %q", got, want)
	}
	m, _ = key(m, shiftTab)
	if _, out := key(m, enter); out.(preview.LinkMsg).Dest != "https://example.com" {
		t.Errorf("Shift+Tab after wrap = %#v", out)
	}
	m, _ = key(m, shiftTab)
	m, _ = key(m, shiftTab)
	if _, out := key(m, enter); out.(preview.LinkMsg).Dest != "https://example.com" {
		t.Errorf("Shift+Tab wraps backwards: %#v", out)
	}
}

func TestShiftTabFirstPicksLast(t *testing.T) {
	m := guide(t, 80, 20)
	m.ScrollToSlug("chat-panel")
	m, _ = key(m, shiftTab)
	if _, out := key(m, enter); out.(preview.LinkMsg).Dest != "https://example.com" {
		t.Errorf("first Shift+Tab = %#v", out)
	}
}

func TestEnterWithoutLinkDoesNothing(t *testing.T) {
	m := guide(t, 80, 20)
	if _, out := key(m, enter); out != nil {
		t.Errorf("Enter with no focused link = %#v", out)
	}
	// A focused link that scrolls out of view is dropped.
	m, _ = key(m, tab)
	m, _ = key(m, runes("G"))
	m, _ = key(m, runes("g"))
	m, _ = key(m, tea.KeyMsg{Type: tea.KeyPgDown})
	if _, out := key(m, enter); out != nil {
		t.Errorf("Enter on a link out of view = %#v", out)
	}
}

func TestBack(t *testing.T) {
	m := guide(t, 80, 20)
	for _, k := range []tea.KeyMsg{bksp, runes("u")} {
		if _, out := key(m, k); out != (preview.BackMsg{}) {
			t.Errorf("%v = %#v, want BackMsg", k, out)
		}
	}
}

func TestClickOnLink(t *testing.T) {
	m := guide(t, 80, 20)
	m.ScrollToSlug("chat-panel")
	_, mk := m.Marks()
	var l mdview.Link
	for _, x := range mk.Links {
		if x.Dest == "https://example.com" {
			l = x
		}
	}
	y := l.Row - m.Top() + 1 // one header row
	if got, ok := m.LinkAt(l.X0, y); !ok || got.Dest != l.Dest {
		t.Fatalf("LinkAt(%d,%d) = %+v %v", l.X0, y, got, ok)
	}
	if _, ok := m.LinkAt(l.X1, y); ok {
		t.Errorf("LinkAt past the rect hit")
	}
	if _, ok := m.LinkAt(l.X0, 0); ok {
		t.Errorf("LinkAt on the header hit")
	}
	press := tea.MouseMsg{X: l.X0 + 1, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	m2, cmd := m.Update(press)
	if cmd == nil {
		t.Fatal("click on a link: no cmd")
	}
	if out := cmd(); out != (preview.LinkMsg{Dest: "https://example.com"}) {
		t.Errorf("click = %#v", out)
	}
	if m2.Top() != m.Top() {
		t.Errorf("click scrolled")
	}
	press.X = 0
	if _, cmd := m.Update(press); cmd != nil {
		t.Errorf("click off a link: %#v", cmd())
	}
}

// The focused link is drawn highlighted; rows stay exactly w wide.
func TestFocusedLinkHighlight(t *testing.T) {
	defer theme.ApplyTheme("vscode-dark-plus")
	for _, th := range []string{"vscode-dark-plus", "catppuccin-latte"} {
		theme.ApplyTheme(th)
		m := guide(t, 80, 20)
		m.ScrollToSlug("chat-panel")
		before := m.View()
		m, _ = key(m, tab)
		after := m.View()
		if before == after {
			t.Errorf("%s: Tab changed nothing on screen", th)
		}
		if ansi.Strip(before) != ansi.Strip(after) {
			t.Errorf("%s: the highlight changed the text", th)
		}
		testkit.CheckFrame(t, after, 80, 20)
	}
}

func TestGuideHeaderAndHints(t *testing.T) {
	m := guide(t, 150, 20)
	m.SetHeader("ⓘ", "Use AI", "Guide")
	m.SetHints(preview.GuideHints(true, true))
	v := strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.HasPrefix(v[0], " ⓘ Use AI") || !strings.Contains(v[0], "Guide") {
		t.Errorf("header = %q", v[0])
	}
	foot := v[len(v)-1]
	for _, s := range []string{"esc close", "tab links", "enter open", "backspace back", "↑↓ scroll", "%"} {
		if !strings.Contains(foot, s) {
			t.Errorf("footer %q lacks %q", foot, s)
		}
	}
	m.SetHints(preview.GuideHints(false, false))
	foot = lastRow(m)
	if strings.Contains(foot, "tab links") || strings.Contains(foot, "backspace") {
		t.Errorf("footer without links and stack = %q", foot)
	}
	m.SetHints(preview.GuideHints(true, true))
	m.SetSize(50, 20)
	foot = lastRow(m)
	if !strings.Contains(foot, "esc close") || strings.Contains(foot, "tab links") || !strings.Contains(foot, "%") {
		t.Errorf("narrow footer = %q", foot)
	}
	for _, w := range []int{150, 50, 20, 8} {
		m.SetSize(w, 12)
		testkit.CheckFrame(t, m.View(), w, 12)
	}
}

func lastRow(m preview.Model) string {
	v := strings.Split(ansi.Strip(m.View()), "\n")
	return v[len(v)-1]
}

// The F7 preview (NewDoc, no hints, no header override) draws exactly as
// before: the plain file-name header and the old footer.
func TestPlainReaderUnchanged(t *testing.T) {
	m := guide(t, 100, 20)
	foot := lastRow(m)
	if !strings.HasPrefix(foot, " esc · q  close") || !strings.Contains(foot, "g/G  top/bottom") {
		t.Errorf("default footer changed: %q", foot)
	}
	doc := preview.NewDoc(func(w int) []string { return mdview.Render(guideSrc(), mdview.Options{Width: w}) })
	p := preview.NewReader("x.md", "Preview", doc)
	p.SetSize(100, 20)
	p, _ = key(p, tab)
	if _, out := key(p, enter); out != nil {
		t.Errorf("Enter in a doc without marks = %#v", out)
	}
	if _, mk := p.Marks(); len(mk.Links)+len(mk.Headings) != 0 {
		t.Errorf("NewDoc has marks: %+v", mk)
	}
}
