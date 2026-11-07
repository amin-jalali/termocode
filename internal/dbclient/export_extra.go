package dbclient

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"strings"
	"unicode/utf8"
)

// More export formats. They are not in the ExportFormats picker list yet.
const (
	FormatJSONLines ExportFormat = "jsonl"
	FormatYAML      ExportFormat = "yaml"
	FormatHTML      ExportFormat = "html"
	FormatXML       ExportFormat = "xml"
	FormatSQLUpdate ExportFormat = "sql-update"
)

// MoreExportFormats are the formats the writer knows beyond the picker list.
// HTML is in the picker (DBX-19, a full page); htmlWriter (a bare table) stays
// for callers that embed the table.
var MoreExportFormats = []ExportFormatInfo{
	{FormatJSONLines, "JSON lines", ".jsonl"},
	{FormatYAML, "YAML", ".yaml"},
	{FormatXML, "XML", ".xml"},
	{FormatSQLUpdate, "SQL UPDATEs", ".sql"},
}

// checkSeparator refuses a CSV separator that would break the file.
func checkSeparator(r rune) error {
	if r == 0 {
		return nil
	}
	if r == '"' || r == '\r' || r == '\n' || r == utf8.RuneError || !utf8.ValidRune(r) {
		return fmt.Errorf("%q cannot be the separator", r)
	}
	return nil
}

// columnFilter passes on only the named columns, in the given order.
type columnFilter struct {
	w     RowWriter
	names []string
	idx   []int
	buf   []Cell
}

func (c *columnFilter) Columns(cols []Column) error {
	c.idx = c.idx[:0]
	out := make([]Column, 0, len(c.names))
	for _, n := range c.names {
		found := -1
		for i, col := range cols {
			if col.Name == n {
				found = i
				break
			}
		}
		if found < 0 {
			return fmt.Errorf("the result has no column %q", n)
		}
		c.idx = append(c.idx, found)
		out = append(out, cols[found])
	}
	return c.w.Columns(out)
}

func (c *columnFilter) Row(row []Cell) error {
	b := c.buf[:0]
	for _, i := range c.idx {
		cell := Cell{Null: true}
		if i < len(row) {
			cell = row[i]
		}
		b = append(b, cell)
	}
	c.buf = b
	return c.w.Row(b)
}

func (c *columnFilter) End() error { return c.w.End() }

type jsonLinesWriter struct {
	w     io.Writer
	names [][]byte
	buf   []byte
}

func (j *jsonLinesWriter) Columns(cols []Column) error {
	j.names = make([][]byte, len(cols))
	for i, c := range cols {
		j.names[i] = jsonCell(Cell{Text: c.Name})
	}
	return nil
}

func (j *jsonLinesWriter) Row(row []Cell) error {
	b := append(j.buf[:0], '{')
	for i, n := range j.names {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, n...)
		b = append(b, ':')
		cell := Cell{Null: true}
		if i < len(row) {
			cell = row[i]
		}
		b = appendJSONCell(b, cell)
	}
	b = append(b, '}', '\n')
	j.buf = b
	_, err := j.w.Write(b)
	return err
}

func (j *jsonLinesWriter) End() error { return nil }

// yamlWriter writes a YAML list of maps. Keys and strings are JSON strings,
// which YAML reads as double-quoted scalars, so no value changes type.
type yamlWriter struct {
	w     io.Writer
	names [][]byte
	buf   []byte
	rows  int
}

func (y *yamlWriter) Columns(cols []Column) error {
	y.names = make([][]byte, len(cols))
	for i, c := range cols {
		y.names[i] = jsonCell(Cell{Text: c.Name})
	}
	return nil
}

func (y *yamlWriter) Row(row []Cell) error {
	y.rows++
	b := y.buf[:0]
	if len(y.names) == 0 {
		b = append(b, "- {}\n"...)
	}
	for i, n := range y.names {
		if i == 0 {
			b = append(b, "- "...)
		} else {
			b = append(b, "  "...)
		}
		b = append(b, n...)
		b = append(b, ": "...)
		cell := Cell{Null: true}
		if i < len(row) {
			cell = row[i]
		}
		b = appendJSONCell(b, cell)
		b = append(b, '\n')
	}
	y.buf = b
	_, err := y.w.Write(b)
	return err
}

func (y *yamlWriter) End() error {
	if y.rows == 0 {
		_, err := io.WriteString(y.w, "[]\n")
		return err
	}
	return nil
}

type htmlWriter struct {
	w    io.Writer
	cols int
	sb   strings.Builder
}

func (h *htmlWriter) Columns(cols []Column) error {
	h.cols = len(cols)
	h.sb.Reset()
	h.sb.WriteString("<table>\n<thead><tr>")
	for _, c := range cols {
		h.sb.WriteString("<th>" + html.EscapeString(c.Name) + "</th>")
	}
	h.sb.WriteString("</tr></thead>\n<tbody>\n")
	_, err := io.WriteString(h.w, h.sb.String())
	return err
}

func (h *htmlWriter) Row(row []Cell) error {
	h.sb.Reset()
	h.sb.WriteString("<tr>")
	for i := 0; i < h.cols; i++ {
		if i >= len(row) || row[i].Null {
			h.sb.WriteString(`<td class="null">NULL</td>`)
			continue
		}
		h.sb.WriteString("<td>" + html.EscapeString(row[i].Text) + "</td>")
	}
	h.sb.WriteString("</tr>\n")
	_, err := io.WriteString(h.w, h.sb.String())
	return err
}

func (h *htmlWriter) End() error {
	_, err := io.WriteString(h.w, "</tbody>\n</table>\n")
	return err
}

type xmlWriter struct {
	w     io.Writer
	names []string
	buf   bytes.Buffer
}

func xmlEscape(b *bytes.Buffer, s string) { _ = xml.EscapeText(b, []byte(s)) }

func (x *xmlWriter) Columns(cols []Column) error {
	x.names = make([]string, len(cols))
	for i, c := range cols {
		var b bytes.Buffer
		xmlEscape(&b, c.Name)
		x.names[i] = b.String()
	}
	_, err := io.WriteString(x.w, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<rows>\n")
	return err
}

func (x *xmlWriter) Row(row []Cell) error {
	b := &x.buf
	b.Reset()
	b.WriteString("  <row>\n")
	for i, n := range x.names {
		b.WriteString(`    <column name="` + n + `"`)
		if i >= len(row) || row[i].Null {
			b.WriteString(" null=\"true\"/>\n")
			continue
		}
		b.WriteByte('>')
		xmlEscape(b, row[i].Text)
		b.WriteString("</column>\n")
	}
	b.WriteString("  </row>\n")
	_, err := x.w.Write(b.Bytes())
	return err
}

func (x *xmlWriter) End() error {
	_, err := io.WriteString(x.w, "</rows>\n")
	return err
}

// sqlUpdateWriter writes one UPDATE per row: every column that is not a key
// goes to SET, the keys go to WHERE (a NULL key is "IS NULL").
type sqlUpdateWriter struct {
	w     io.Writer
	d     Dialect
	table string
	keys  []string
	kIdx  []int
	sIdx  []int
	names []string
	ready bool
}

func (s *sqlUpdateWriter) Columns(cols []Column) error {
	isKey := make([]bool, len(cols))
	for _, k := range s.keys {
		found := -1
		for i, c := range cols {
			if c.Name == k {
				found = i
				break
			}
		}
		if found < 0 {
			return fmt.Errorf("the result has no key column %q", k)
		}
		isKey[found] = true
		s.kIdx = append(s.kIdx, found)
	}
	s.names = make([]string, len(cols))
	for i, c := range cols {
		s.names[i] = s.d.QuoteIdent(c.Name)
		if !isKey[i] {
			s.sIdx = append(s.sIdx, i)
		}
	}
	if len(s.sIdx) == 0 {
		return errors.New("every column is a key: nothing to SET")
	}
	s.ready = true
	return nil
}

func (s *sqlUpdateWriter) Row(row []Cell) error {
	if !s.ready {
		return errors.New("row before columns")
	}
	cell := func(i int) Cell {
		if i < len(row) {
			return row[i]
		}
		return Cell{Null: true}
	}
	set := make([]string, len(s.sIdx))
	for j, i := range s.sIdx {
		set[j] = s.names[i] + " = " + SQLLiteral(s.d, cell(i))
	}
	where := make([]string, len(s.kIdx))
	for j, i := range s.kIdx {
		if c := cell(i); c.Null {
			where[j] = s.names[i] + " IS NULL"
		} else {
			where[j] = s.names[i] + " = " + SQLLiteral(s.d, c)
		}
	}
	_, err := io.WriteString(s.w, "UPDATE "+s.table+" SET "+strings.Join(set, ", ")+" WHERE "+strings.Join(where, " AND ")+";\n")
	return err
}

func (s *sqlUpdateWriter) End() error { return nil }
