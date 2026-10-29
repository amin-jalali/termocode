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

func ToCSV(r Result) string { return toDelimited(r, ',') }
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

// appendJSONCell appends jsonCell(c) to dst. Plain text and valid numbers
// are written directly; anything encoding/json would escape goes through
// jsonCell.
func appendJSONCell(dst []byte, c Cell) []byte {
	switch {
	case c.Null:
		return append(dst, "null"...)
	case c.Kind == "number":
		if p := (jsonScan{s: c.Text}); p.number() && p.i == len(c.Text) {
			return append(dst, c.Text...)
		}
	case c.Kind == "bool":
		if c.Text == "true" {
			return append(dst, "true"...)
		}
		return append(dst, "false"...)
	}
	for i := 0; i < len(c.Text); i++ {
		switch ch := c.Text[i]; {
		case ch < 0x20 || ch >= 0x80 || ch == '"' || ch == '\\' || ch == '<' || ch == '>' || ch == '&':
			return append(dst, jsonCell(c)...)
		}
	}
	dst = append(dst, '"')
	dst = append(dst, c.Text...)
	return append(dst, '"')
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
