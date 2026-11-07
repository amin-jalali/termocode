package diagram

import (
	"encoding/csv"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
)

const (
	maxTableRows = 200
	maxTableCols = 30
	maxCellW     = 40
	minCellW     = 3
)

// table draws CSV (or TSV) text as a box-drawn table: the first record is
// the header, a column of numbers is right-aligned, long cells are cut
// with "…" so the table fits width, and rows past maxTableRows become one
// "… n more rows" line.
func table(src string, sep rune, width int) ([]string, bool) {
	r := csv.NewReader(strings.NewReader(src))
	r.Comma = sep
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = sep != '\t' // for TSV it would eat empty fields
	recs, err := r.ReadAll()
	if err != nil || len(recs) == 0 {
		return nil, false
	}
	more := 0
	if len(recs) > maxTableRows+1 {
		more = len(recs) - maxTableRows - 1
		recs = recs[:maxTableRows+1]
	}
	cols := 0
	for _, rec := range recs {
		cols = max(cols, len(rec))
	}
	if cols == 0 || cols > maxTableCols {
		return nil, false
	}
	ws := make([]int, cols)
	num := make([]bool, cols)
	for c := range num {
		num[c] = len(recs) > 1
	}
	for i, rec := range recs {
		for c := range cols {
			v := ""
			if c < len(rec) {
				v = cleanText(rec[c])
				rec[c] = v
			}
			ws[c] = min(max(ws[c], runewidth.StringWidth(v)), maxCellW)
			if i > 0 && strings.TrimSpace(v) != "" {
				if _, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(v), ",", ""), 64); err != nil {
					num[c] = false
				}
			}
		}
	}
	// Each column costs its width + 3 (a bar and two spaces), plus one bar.
	total := func() int {
		t := 1
		for _, w := range ws {
			t += w + 3
		}
		return t
	}
	for total() > width {
		wi := 0
		for c, w := range ws {
			if w > ws[wi] {
				wi = c
			}
		}
		if ws[wi] <= minCellW {
			return nil, false
		}
		ws[wi]--
	}
	for c := range ws {
		ws[c] = max(ws[c], 1)
	}
	rule := func(l, m, r string) string {
		var b strings.Builder
		b.WriteString(l)
		for c, w := range ws {
			if c > 0 {
				b.WriteString(m)
			}
			b.WriteString(strings.Repeat("─", w+2))
		}
		b.WriteString(r)
		return b.String()
	}
	row := func(rec []string, head bool) string {
		var b strings.Builder
		b.WriteString("│")
		for c, w := range ws {
			v := ""
			if c < len(rec) {
				v = cut(rec[c], w)
			}
			pad := strings.Repeat(" ", w-runewidth.StringWidth(v))
			if num[c] && !head {
				b.WriteString(" " + pad + v + " │")
			} else {
				b.WriteString(" " + v + pad + " │")
			}
		}
		return b.String()
	}
	out := []string{rule("┌", "┬", "┐"), row(recs[0], true)}
	if len(recs) > 1 {
		out = append(out, rule("├", "┼", "┤"))
		for _, rec := range recs[1:] {
			out = append(out, row(rec, false))
		}
	}
	out = append(out, rule("└", "┴", "┘"))
	if more > 0 {
		out = append(out, cut("… "+strconv.Itoa(more)+" more rows", width))
	}
	return out, true
}
