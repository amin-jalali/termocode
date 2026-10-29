package keyhint

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func manyEntries(n int) []Entry {
	var es []Entry
	for i := 0; i < n; i++ {
		es = append(es, Entry{Key: string(rune('a' + i%26)), Desc: fmt.Sprintf("Do thing number %d with a long text", i)})
	}
	return es
}

// Spec: the popup fits every screen size, is bottom-right above the
// status row, and every row has the same width.
func TestLayoutFits(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {150, 42}, {40, 12}, {100, 30}} {
		for _, n := range []int{1, 6, 30, 120} {
			c := Content{Title: "<space>", Entries: manyEntries(n)}
			rows, x, y, ok := Layout(c, sz[0], sz[1])
			if !ok {
				t.Errorf("%v n=%d: no layout", sz, n)
				continue
			}
			if y < 0 || y+len(rows) > sz[1]-1 {
				t.Errorf("%v n=%d: rows %d at y=%d leave no status row", sz, n, len(rows), y)
			}
			w := ansi.StringWidth(rows[0])
			if x < 0 || x+w != sz[0] {
				t.Errorf("%v n=%d: x=%d w=%d, not at the right edge", sz, n, x, w)
			}
			for _, r := range rows {
				if ansi.StringWidth(r) != w {
					t.Errorf("%v n=%d: ragged row %q", sz, n, ansi.Strip(r))
				}
			}
			if sz[0] < narrowBelow && w != sz[0] {
				t.Errorf("%v n=%d: narrow box is %d wide, want full width", sz, n, w)
			}
		}
	}
}

// Spec: the title is the prefix; "<space>" reads "Space"; a group shows +.
func TestLayoutText(t *testing.T) {
	c := Content{Title: "<space> g", Entries: []Entry{{Key: "s", Desc: "Stage"}, {Key: "<cr>", Desc: "2 more", Group: true}}}
	rows, _, _, _ := Layout(c, 80, 24)
	plain := ansi.Strip(strings.Join(rows, "\n"))
	for _, want := range []string{"Space g", "Stage", "Enter  +2 more"} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q in\n%s", want, plain)
		}
	}
}

// Spec: the "no binding" hint is one short row.
func TestLayoutNotice(t *testing.T) {
	rows, _, _, ok := Layout(Content{Notice: "no binding: q"}, 80, 24)
	if !ok || len(rows) != 3 || !strings.Contains(ansi.Strip(rows[1]), "no binding: q") {
		t.Errorf("notice rows = %q", rows)
	}
}

// Spec: too small a screen draws nothing instead of overflowing.
func TestLayoutTooSmall(t *testing.T) {
	if _, _, _, ok := Layout(Content{Title: "x", Entries: manyEntries(3)}, 10, 3); ok {
		t.Error("drew on a 10x3 screen")
	}
}
