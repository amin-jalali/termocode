package dbclient

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
)

func Grid(r Result, page int, widths []int) []string {
	if page > 0 && page*100 >= len(r.Rows) {
		return nil
	}
	var out []string
	header := make([]string, len(r.Columns))
	for i, c := range r.Columns {
		w := 18
		if i < len(widths) && widths[i] > 0 {
			w = widths[i]
		}
		header[i] = fit(c.Name, w)
	}
	out = append(out, strings.Join(header, " | "))
	start := page * 100
	if start < 0 {
		start = 0
	}
	end := start + 100
	if end > len(r.Rows) {
		end = len(r.Rows)
	}
	for _, row := range r.Rows[start:end] {
		v := make([]string, len(r.Columns))
		for i := range v {
			if i < len(row) {
				w := 18
				if i < len(widths) && widths[i] > 0 {
					w = widths[i]
				}
				v[i] = fit(row[i].String(), w)
			}
		}
		out = append(out, strings.Join(v, " | "))
	}
	return out
}
func fit(s string, n int) string {
	if n < 1 {
		return ""
	}
	if len([]rune(s)) > n {
		rr := []rune(s)
		return string(rr[:n-1]) + "…"
	}
	return s
}
func Transposed(r Result, row int) []string {
	if row < 0 || row >= len(r.Rows) {
		return nil
	}
	out := make([]string, 0, len(r.Columns))
	for i, c := range r.Columns {
		v := "<null>"
		if i < len(r.Rows[row]) {
			v = r.Rows[row][i].String()
		}
		out = append(out, c.Name+" | "+v)
	}
	return out
}
func ToCSV(r Result) string { return toDelimited(r, ',') }
func ToTSV(r Result) string { return toDelimited(r, '\t') }
func toDelimited(r Result, delim rune) string {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	w.Comma = delim
	w.UseCRLF = true
	h := make([]string, len(r.Columns))
	for i, c := range r.Columns {
		h[i] = c.Name
	}
	_ = w.Write(h)
	for _, row := range r.Rows {
		a := make([]string, len(r.Columns))
		for i := range a {
			if i < len(row) && !row[i].Null {
				a[i] = row[i].Text
			}
		}
		_ = w.Write(a)
	}
	w.Flush()
	return b.String()
}
func ToJSON(r Result) string {
	var b bytes.Buffer
	b.WriteByte('[')
	for ri, row := range r.Rows {
		if ri > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('{')
		for i, c := range r.Columns {
			if i > 0 {
				b.WriteByte(',')
			}
			k, err := json.Marshal(c.Name)
			if err != nil {
				k = []byte(`""`)
			}
			b.Write(k)
			b.WriteByte(':')
			cell := Cell{Null: true}
			if i < len(row) {
				cell = row[i]
			}
			b.Write(jsonCell(cell))
		}
		b.WriteByte('}')
	}
	b.WriteByte(']')
	return b.String()
}
func ToSQLInserts(r Result, table, dialect string) string {
	if len(r.Columns) == 0 {
		return ""
	}
	q := func(s string) string {
		if dialect == "mysql" {
			return "`" + strings.ReplaceAll(s, "`", "``") + "`"
		}
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	cols := make([]string, len(r.Columns))
	for i, c := range r.Columns {
		cols[i] = q(c.Name)
	}
	var out []string
	for _, row := range r.Rows {
		vals := make([]string, len(r.Columns))
		for i := range vals {
			if i >= len(row) || row[i].Null {
				vals[i] = "NULL"
			} else {
				vals[i] = "'" + strings.ReplaceAll(row[i].Text, "'", "''") + "'"
			}
		}
		out = append(out, "INSERT INTO "+q(table)+" ("+strings.Join(cols, ", ")+") VALUES ("+strings.Join(vals, ", ")+");")
	}
	return strings.Join(out, "\n")
}
func ToMarkdown(r Result) string {
	var b strings.Builder
	for i, c := range r.Columns {
		if i > 0 {
			b.WriteString(" | ")
		}
		b.WriteString(strings.ReplaceAll(c.Name, "|", "\\|"))
	}
	b.WriteString("\n")
	for i := range r.Columns {
		if i > 0 {
			b.WriteString(" | ")
		}
		b.WriteString("---")
	}
	for _, row := range r.Rows {
		b.WriteString("\n")
		for i := range r.Columns {
			if i > 0 {
				b.WriteString(" | ")
			}
			v := "<null>"
			if i < len(row) {
				v = row[i].String()
			}
			b.WriteString(strings.ReplaceAll(v, "|", "\\|"))
		}
	}
	return b.String()
}
func Sort(r *Result, col int, desc bool) {
	if col < 0 || len(r.Rows) == 0 {
		return
	}
	sort.SliceStable(r.Rows, func(i, j int) bool {
		a, aOK := cellAt(r.Rows[i], col)
		b, bOK := cellAt(r.Rows[j], col)
		if !aOK || a.Null {
			return (!bOK || b.Null) == false && !desc
		}
		if !bOK || b.Null {
			return desc
		}
		cmp := compareCells(a, b)
		if desc {
			return cmp > 0
		}
		return cmp < 0
	})
}

func cellAt(row []Cell, col int) (Cell, bool) {
	if col < 0 || col >= len(row) {
		return Cell{}, false
	}
	return row[col], true
}

func compareCells(a, b Cell) int {
	if a.Kind == "number" && b.Kind == "number" {
		av, aOK := new(big.Rat).SetString(a.Text)
		bv, bOK := new(big.Rat).SetString(b.Text)
		if aOK && bOK {
			return av.Cmp(bv)
		}
	}
	return strings.Compare(a.Text, b.Text)
}
func Filter(r Result, text string) Result {
	if text == "" {
		return r
	}
	out := r
	out.Rows = nil
	needle := strings.ToLower(text)
	for _, row := range r.Rows {
		for _, c := range row {
			if strings.Contains(strings.ToLower(c.String()), needle) {
				out.Rows = append(out.Rows, row)
				break
			}
		}
	}
	return out
}

// jsonCell is the JSON text of one cell: NULL, a valid number, a boolean or
// a string.
func jsonCell(c Cell) []byte {
	var v any
	if c.Null {
		v = nil
	} else {
		switch c.Kind {
		case "number":
			n := json.Number(c.Text)
			if json.Valid([]byte(n.String())) {
				v = n
			} else {
				v = c.Text
			}
		case "bool":
			v = c.Text == "true"
		default:
			v = c.Text
		}
	}
	x, err := json.Marshal(v)
	if err != nil {
		x, err = json.Marshal(fmt.Sprint(v))
		if err != nil {
			x = []byte("null")
		}
	}
	return x
}
