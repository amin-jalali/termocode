package mdview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/rtl"
)

const persianDoc = "# راهنما\n\nاین یک متن فارسی است با `code` و English و عدد 42.\n\nپاراگراف دوم (با پرانتز) که بلند است و باید در چند خط شکسته شود تا ترتیب خط‌ها دیده شود.\n\n```\nx := 1 // سلام\n```\n\n| نام | سن |\n|---|---|\n| علی رضا | 30 |\n"

// Spec: with RTL on, Persian paragraphs and headings are shaped, in
// reading order and right aligned; code blocks keep LTR order (only an
// RTL comment turns); every
// row is exactly the view width.
func TestRenderRTL(t *testing.T) {
	const w = 60
	rows := Render(persianDoc, Options{Width: w, RTL: true})
	left, col := Layout(w, 0)
	var plain []string
	for i, r := range rows {
		if ansi.StringWidth(r) != w {
			t.Fatalf("row %d width %d", i, ansi.StringWidth(r))
		}
		plain = append(plain, ansi.Strip(r))
	}
	all := strings.Join(plain, "\n")
	for _, want := range []string{
		rtl.Visual("راهنما", rtl.RTL),
		" code ", "English", "42",
		"(" + rtl.Visual("با پرانتز", rtl.RTL) + ")",
		"x := 1 // " + rtl.Visual("سلام", rtl.RTL), // code stays LTR, the comment turns
		rtl.Visual("علی رضا", rtl.RTL),
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in\n%s", want, all)
		}
	}
	if strings.Contains(all, "راهنما") {
		t.Error("heading left in logical order")
	}
	// Right aligned: the heading row ends at the column's right edge.
	for _, p := range plain {
		if strings.Contains(p, rtl.Visual("راهنما", rtl.RTL)) {
			if end := ansi.StringWidth(strings.TrimRight(p, " ")); end != left+col {
				t.Errorf("heading ends at %d, want %d: %q", end, left+col, p)
			}
		}
	}
	// Wrapped in logical order: the first words of the long paragraph
	// are on the right end of its first row.
	first := rtl.Visual("پاراگراف دوم", rtl.RTL)
	found := false
	for _, p := range plain {
		if strings.HasSuffix(strings.TrimRight(p, " "), first) {
			found = true
		}
	}
	if !found {
		t.Errorf("first row of the long paragraph does not end with %q:\n%s", first, all)
	}
}

// Spec: RTL off draws the text as written (the terminal may lay it out).
func TestRenderRTLOff(t *testing.T) {
	all := ansi.Strip(strings.Join(Render(persianDoc, Options{Width: 60}), "\n"))
	if !strings.Contains(all, "راهنما") {
		t.Errorf("logical text missing:\n%s", all)
	}
}

// Spec: English documents render the same with RTL on.
func TestRenderRTLNoChangeForLTR(t *testing.T) {
	src := "# Title\n\nSome *text* with `code`.\n\n- a\n- b\n\n> quote\n"
	a := Render(src, Options{Width: 70})
	b := Render(src, Options{Width: 70, RTL: true})
	if strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Error("RTL option changed an English document")
	}
}

// Spec: a Persian list has its markers on the right of the column, the
// text right aligned next to them.
func TestRenderRTLList(t *testing.T) {
	const w = 60
	rows := Render("- مورد اول\n- مورد دوم که بلند است و باید در چند خط شکسته شود تا دیده شود\n", Options{Width: w, RTL: true})
	left, col := Layout(w, 0)
	want := rtl.Visual("مورد اول", rtl.RTL) + " " + newRenderer(nil, Options{}).g.bullets[0]
	found := false
	for i, r := range rows {
		if ansi.StringWidth(r) != w {
			t.Fatalf("row %d width %d", i, ansi.StringWidth(r))
		}
		p := strings.TrimRight(ansi.Strip(r), " ")
		if strings.HasSuffix(p, want) {
			found = true
			// The marker sits where an LTR marker's indent ends (2 + 1 + 1).
			if end := ansi.StringWidth(p); end != left+col-2 {
				t.Errorf("item ends at %d, want %d", end, left+col-2)
			}
		}
	}
	if !found {
		t.Errorf("no row ends with %q:\n%s", want, ansi.Strip(strings.Join(rows, "\n")))
	}
}

func TestRTLPrependMarkRowWidth(t *testing.T) {
	for _, src := range []string{"\u0604ا", "# \u0604ا", "- \u0604ا", "> \u0604ا", "text \u0604ا"} {
		for _, width := range []int{12, 40, 80} {
			for i, row := range Render(src, Options{Width: width, RTL: true}) {
				if got := ansi.StringWidth(row); got != width {
					t.Errorf("%q row %d: width %d, want %d", src, i, got, width)
				}
			}
		}
	}
}
