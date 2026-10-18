package mdview

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/theme"
)

// cells is the plain text of row between cells x0 and x1.
func cells(row string, x0, x1 int) string { return ansi.Strip(ansi.Cut(row, x0, x1)) }

// RenderDoc draws exactly what Render draws: the F7 preview stays
// byte-identical, with or without link recording.
func TestRenderDocRowsEqualRender(t *testing.T) {
	withMode(t, theme.IconModeNerdFont)
	srcs := []string{sample(t)}
	for _, p := range help.Pages() {
		pg, err := help.Load(p.Path)
		if err != nil {
			t.Fatal(err)
		}
		srcs = append(srcs, pg.Source)
	}
	for _, src := range srcs {
		for _, w := range []int{12, 50, 120} {
			for _, rtl := range []bool{false, true} {
				want := Render(src, Options{Width: w, RTL: rtl})
				for _, links := range []bool{false, true} {
					got, _ := RenderDoc(src, Options{Width: w, RTL: rtl, Links: links})
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("width %d rtl %v links %v: RenderDoc rows differ from Render", w, rtl, links)
					}
				}
			}
		}
	}
}

// Heading marks point at the heading's first row, at every width, and
// their anchors are the website's (the same as internal/help's).
func TestRenderDocHeadingRows(t *testing.T) {
	withMode(t, theme.IconModeNerdFont)
	src := "# Title\n\nSome text that is long enough to wrap at forty cells, and then some more words.\n\n" +
		"## A heading long enough to wrap onto a second row at forty cells wide\n\nText.\n\n" +
		"> ### In a quote\n\n- ### In a list\n\n## Title\n\n```\n# not a heading\n```\n"
	for _, w := range []int{40, 100} {
		rows, mk := RenderDoc(src, Options{Width: w})
		want := []struct {
			level      int
			text, slug string
		}{
			{1, "Title", "title"},
			{2, "A heading long enough to wrap onto a second row at forty cells wide", "a-heading-long-enough-to-wrap-onto-a-second-row-at-forty-cells-wide"},
			{3, "In a quote", "in-a-quote"},
			{3, "In a list", "in-a-list"},
			{2, "Title", "title_1"},
		}
		if len(mk.Headings) != len(want) {
			t.Fatalf("width %d: %d heading marks, want %d: %+v", w, len(mk.Headings), len(want), mk.Headings)
		}
		for i, h := range mk.Headings {
			if h.Level != want[i].level || h.Text != want[i].text || h.Slug != want[i].slug {
				t.Errorf("width %d mark %d = %+v, want %+v", w, i, h, want[i])
			}
			first := strings.Fields(want[i].text)[0]
			if row := ansi.Strip(rows[h.Row]); !strings.Contains(row, first) {
				t.Errorf("width %d: mark %q points at row %d %q", w, h.Slug, h.Row, row)
			}
		}
		if mk.Headings[0].Row != 1 {
			t.Errorf("first heading at row %d, want 1 (one blank row on top)", mk.Headings[0].Row)
		}
		if w == 40 && mk.Headings[2].Row-mk.Headings[1].Row < 5 {
			t.Errorf("wrapped heading did not push the next mark down: %+v", mk.Headings)
		}
	}
}

// Every embedded page: the renderer's heading anchors are help's.
func TestRenderDocSlugsMatchHelp(t *testing.T) {
	for _, p := range help.Pages() {
		pg, err := help.Load(p.Path)
		if err != nil {
			t.Fatal(err)
		}
		_, mk := RenderDoc(pg.Source, Options{Width: 100})
		if len(mk.Headings) != len(pg.Headings) {
			t.Errorf("%s: %d marks, help has %d headings", p.Path, len(mk.Headings), len(pg.Headings))
			continue
		}
		for i, h := range pg.Headings {
			if mk.Headings[i].Slug != h.Slug {
				t.Errorf("%s: mark %d %q, help %q", p.Path, i, mk.Headings[i].Slug, h.Slug)
			}
		}
	}
}

func linkByDest(mk Marks, dest string) []Link {
	var out []Link
	for _, l := range mk.Links {
		if l.Dest == dest {
			out = append(out, l)
		}
	}
	return out
}

func TestRenderDocLinkRects(t *testing.T) {
	withMode(t, theme.IconModeNerdFont)
	src := "See [Set up AI](ai-setup.md#keys) first, or [the site](https://example.com/x).\n\n" +
		"- item with [a link](../reference/keys.md)\n\n" +
		"> quoted [link](#chat-panel) here\n\n" +
		"1. one\n2. two [numbered](a.md)\n"
	for _, w := range []int{60, 120} {
		rows, mk := RenderDoc(src, Options{Width: w, Links: true})
		for _, c := range []struct{ dest, text string }{
			{"ai-setup.md#keys", "Set up AI"},
			{"https://example.com/x", "the site"},
			{"../reference/keys.md", "a link"},
			{"#chat-panel", "link"},
			{"a.md", "numbered"},
		} {
			ls := linkByDest(mk, c.dest)
			if len(ls) != 1 {
				t.Fatalf("width %d: %d rects for %s: %+v", w, len(ls), c.dest, mk.Links)
			}
			l := ls[0]
			if got := cells(rows[l.Row], l.X0, l.X1); got != c.text {
				t.Errorf("width %d: rect of %s covers %q, want %q", w, c.dest, got, c.text)
			}
		}
		ids := map[int]bool{}
		for _, l := range mk.Links {
			ids[l.ID] = true
		}
		if len(ids) != 5 {
			t.Errorf("link ids = %v, want 5 distinct", ids)
		}
	}
	// Links off: no rects.
	if _, mk := RenderDoc(src, Options{Width: 80}); len(mk.Links) != 0 {
		t.Errorf("links recorded with Links off: %+v", mk.Links)
	}
}

// A link that wraps keeps one ID over its rows.
func TestRenderDocWrappedLink(t *testing.T) {
	withMode(t, theme.IconModeNerdFont)
	src := "Words before the link [one two three four five six seven](x.md) after.\n"
	rows, mk := RenderDoc(src, Options{Width: 30, Links: true})
	ls := linkByDest(mk, "x.md")
	if len(ls) < 2 {
		t.Fatalf("rects = %+v", ls)
	}
	var got []string
	for _, l := range ls {
		if l.ID != ls[0].ID {
			t.Errorf("wrapped link has two ids: %+v", ls)
		}
		got = append(got, cells(rows[l.Row], l.X0, l.X1))
	}
	if strings.Join(got, " ") != "one two three four five six seven" {
		t.Errorf("rects cover %q", got)
	}
}

// A right-to-left paragraph is right aligned: the rect follows the text.
func TestRenderDocRTLLink(t *testing.T) {
	withMode(t, theme.IconModeNerdFont)
	src := "سلام [پیوند](guides/git.md) دنیا\n"
	rows, mk := RenderDoc(src, Options{Width: 60, RTL: true, Links: true})
	ls := linkByDest(mk, "guides/git.md")
	if len(ls) != 1 {
		t.Fatalf("rects = %+v", mk.Links)
	}
	l := ls[0]
	got := cells(rows[l.Row], l.X0, l.X1)
	if got == "" || strings.TrimSpace(got) != got || strings.Contains(got, "سلام") || strings.Contains(got, "دنیا") {
		t.Errorf("RTL rect covers %q", got)
	}
	if l.X0 < 30 {
		t.Errorf("RTL row is not right aligned: rect at %d", l.X0)
	}
}

// Relative links never become OSC 8 hyperlinks; web links still do.
func TestRenderDocOSC8(t *testing.T) {
	withMode(t, theme.IconModeNerdFont)
	rows, _ := RenderDoc("[guide](guides/git.md) and [mail](mailto:a@b.c)\n", Options{Width: 80, Links: true})
	all := strings.Join(rows, "\n")
	if strings.Contains(all, "guides/git.md") {
		t.Errorf("a relative link reached the terminal: %q", all)
	}
	if !strings.Contains(all, "\x1b]8;;mailto:a@b.c") {
		t.Errorf("the mail link lost its OSC 8")
	}
}

// A table wider than the window is clipped: rects never pass the edge.
func TestRenderDocRectsInsideRow(t *testing.T) {
	withMode(t, theme.IconModeNerdFont)
	for _, p := range help.Pages() {
		pg, _ := help.Load(p.Path)
		for _, w := range []int{14, 80} {
			rows, mk := RenderDoc(pg.Source, Options{Width: w, Links: true})
			for _, l := range mk.Links {
				if l.Row < 0 || l.Row >= len(rows) || l.X0 < 0 || l.X1 > w || l.X0 >= l.X1 {
					t.Fatalf("%s width %d: bad rect %+v", p.Path, w, l)
				}
			}
		}
	}
}
