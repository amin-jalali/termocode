package dbclient

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ExportFormat is a file format the export writes.
type ExportFormat string

const (
	FormatCSV      ExportFormat = "csv"
	FormatTSV      ExportFormat = "tsv"
	FormatJSON     ExportFormat = "json"
	FormatSQL      ExportFormat = "sql"
	FormatMarkdown ExportFormat = "md"
)

// ExportFormatInfo is one row of the export picker.
type ExportFormatInfo struct {
	Format ExportFormat
	Title  string
	Ext    string
}

// ExportFormats is the picker's list, in order.
var ExportFormats = []ExportFormatInfo{
	{FormatCSV, "CSV", ".csv"},
	{FormatTSV, "TSV", ".tsv"},
	{FormatJSON, "JSON", ".json"},
	{FormatSQL, "SQL INSERTs", ".sql"},
	{FormatMarkdown, "Markdown table", ".md"},
}

// ExportOptions says where INSERT statements go: the dialect of Driver and
// the table (Schema may be empty).
type ExportOptions struct {
	Driver, Schema, Table string
}

// RowSink receives a result one row at a time, so nothing has to hold the
// whole result: Columns once, then Row for every row.
type RowSink interface {
	Columns([]Column) error
	Row([]Cell) error
}

// RowWriter is a RowSink that writes a file format; End closes the document.
type RowWriter interface {
	RowSink
	End() error
}

// RowStreamer is a session that can hand rows over as the server sends
// them, with no row limit and nothing kept in memory.
type RowStreamer interface {
	StreamRows(ctx context.Context, sql string, args []any, sink RowSink) error
}

// ErrExportExists is returned when the file exists and overwrite is false.
var ErrExportExists = errors.New("the file already exists")

// NewRowWriter returns the writer of format f on w.
func NewRowWriter(f ExportFormat, w io.Writer, o ExportOptions) (RowWriter, error) {
	switch f {
	case FormatCSV:
		return newDelimitedWriter(w, ','), nil
	case FormatTSV:
		return newDelimitedWriter(w, '\t'), nil
	case FormatJSON:
		return &jsonWriter{w: w}, nil
	case FormatMarkdown:
		return &markdownWriter{w: w}, nil
	case FormatSQL:
		d := DialectFor(o.Driver)
		if d == nil {
			return nil, errors.New("this connection does not use SQL")
		}
		if strings.TrimSpace(o.Table) == "" {
			return nil, errors.New("INSERT statements need a table name")
		}
		table := d.QuoteIdent(o.Table)
		if o.Schema != "" {
			table = d.QuoteIdent(o.Schema) + "." + d.QuoteIdent(o.Table)
		}
		return &sqlWriter{w: w, d: d, table: table}, nil
	}
	return nil, fmt.Errorf("unknown export format %q", f)
}

// WriteResult writes the loaded result r through w and ends the document.
func WriteResult(w RowWriter, r Result) error {
	if err := emitResult(w, r); err != nil {
		return err
	}
	return w.End()
}

func emitResult(sink RowSink, r Result) error {
	if err := sink.Columns(r.Columns); err != nil {
		return err
	}
	for _, row := range r.Rows {
		if err := sink.Row(row); err != nil {
			return err
		}
	}
	return nil
}

var formulaStarters = "=+-@\t\r"

// ProtectFormula keeps a spreadsheet from running a cell as a formula: text
// that starts with = + - @ (or a tab or carriage return) gets a leading
// single quote.
func ProtectFormula(s string) string {
	if s != "" && strings.ContainsRune(formulaStarters, rune(s[0])) {
		return "'" + s
	}
	return s
}

// protectCell is ProtectFormula for a cell; a real number keeps its sign.
func protectCell(c Cell) string {
	if c.Null {
		return ""
	}
	if c.Kind == "number" && editNumber.MatchString(c.Text) {
		return c.Text
	}
	return ProtectFormula(c.Text)
}

type delimitedWriter struct {
	w    *csv.Writer
	cols int
}

func newDelimitedWriter(w io.Writer, comma rune) *delimitedWriter {
	cw := csv.NewWriter(w)
	cw.Comma = comma
	cw.UseCRLF = true
	return &delimitedWriter{w: cw}
}

func (d *delimitedWriter) Columns(cols []Column) error {
	d.cols = len(cols)
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = ProtectFormula(c.Name)
	}
	return d.w.Write(names)
}

func (d *delimitedWriter) Row(row []Cell) error {
	rec := make([]string, d.cols)
	for i := range rec {
		if i < len(row) {
			rec[i] = protectCell(row[i])
		}
	}
	return d.w.Write(rec)
}

func (d *delimitedWriter) End() error {
	d.w.Flush()
	return d.w.Error()
}

type jsonWriter struct {
	w    io.Writer
	cols []Column
	rows int
}

func (j *jsonWriter) Columns(cols []Column) error { j.cols = cols; return nil }

func (j *jsonWriter) Row(row []Cell) error {
	var b strings.Builder
	if j.rows == 0 {
		b.WriteString("[\n")
	} else {
		b.WriteString(",\n")
	}
	j.rows++
	b.WriteByte('{')
	for i, c := range j.cols {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(jsonCell(Cell{Text: c.Name}))
		b.WriteByte(':')
		cell := Cell{Null: true}
		if i < len(row) {
			cell = row[i]
		}
		b.Write(jsonCell(cell))
	}
	b.WriteByte('}')
	_, err := io.WriteString(j.w, b.String())
	return err
}

func (j *jsonWriter) End() error {
	text := "\n]\n"
	if j.rows == 0 {
		text = "[]\n"
	}
	_, err := io.WriteString(j.w, text)
	return err
}

type markdownWriter struct {
	w    io.Writer
	cols int
}

var mdEscaper = strings.NewReplacer("|", `\|`, "\r\n", "<br>", "\n", "<br>", "\r", "")

func (m *markdownWriter) line(cells []string) error {
	_, err := io.WriteString(m.w, "| "+strings.Join(cells, " | ")+" |\n")
	return err
}

func (m *markdownWriter) Columns(cols []Column) error {
	m.cols = len(cols)
	names := make([]string, len(cols))
	rule := make([]string, len(cols))
	for i, c := range cols {
		names[i], rule[i] = mdEscaper.Replace(c.Name), "---"
	}
	if err := m.line(names); err != nil {
		return err
	}
	return m.line(rule)
}

func (m *markdownWriter) Row(row []Cell) error {
	cells := make([]string, m.cols)
	for i := range cells {
		v := "NULL"
		if i < len(row) && !row[i].Null {
			v = row[i].Text
		} else if i >= len(row) {
			v = ""
		}
		cells[i] = mdEscaper.Replace(v)
	}
	return m.line(cells)
}

func (m *markdownWriter) End() error { return nil }

type sqlWriter struct {
	w     io.Writer
	d     Dialect
	table string
	head  string
	cols  int
}

func (s *sqlWriter) Columns(cols []Column) error {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = s.d.QuoteIdent(c.Name)
	}
	s.cols = len(cols)
	s.head = "INSERT INTO " + s.table + " (" + strings.Join(names, ", ") + ") VALUES ("
	return nil
}

func (s *sqlWriter) Row(row []Cell) error {
	if s.head == "" {
		return errors.New("row before columns")
	}
	vals := make([]string, s.cols)
	for i := range vals {
		c := Cell{Null: true}
		if i < len(row) {
			c = row[i]
		}
		vals[i] = SQLLiteral(s.d, c)
	}
	_, err := io.WriteString(s.w, s.head+strings.Join(vals, ", ")+");\n")
	return err
}

func (s *sqlWriter) End() error { return nil }

var hexBytes = regexp.MustCompile(`^0[xX]([0-9a-fA-F]{2})*$`)

// SQLLiteral writes c as a SQL literal of dialect d: NULL, a plain number,
// TRUE / FALSE, a binary value in the dialect's own form, or a quoted
// string (a quote doubled; for MySQL a backslash too).
func SQLLiteral(d Dialect, c Cell) string {
	if c.Null {
		return "NULL"
	}
	switch c.Kind {
	case "number":
		if editNumber.MatchString(c.Text) {
			return c.Text
		}
	case "bool":
		switch strings.ToLower(c.Text) {
		case "true", "t", "1":
			return "TRUE"
		case "false", "f", "0":
			return "FALSE"
		}
	case "binary":
		if hexBytes.MatchString(c.Text) {
			hex := c.Text[2:]
			switch d.(type) {
			case PostgresDialect:
				return `'\x` + hex + `'`
			case MySQLDialect:
				if hex == "" {
					return "X''"
				}
				return "0x" + hex
			default:
				return "X'" + hex + "'"
			}
		}
	}
	text := strings.ReplaceAll(c.Text, "'", "''")
	if _, mysql := d.(MySQLDialect); mysql {
		text = strings.ReplaceAll(c.Text, `\`, `\\`)
		text = strings.ReplaceAll(text, "'", "''")
	}
	return "'" + text + "'"
}

// StreamRows runs sql (one plain read) on sess and passes every row to
// sink, however many there are. A session that streams does it with one
// cursor; any other is read a page of 1000 rows at a time through a
// wrapping SELECT with LIMIT / OFFSET, so memory stays bounded either way.
func StreamRows(ctx context.Context, sess Session, driver, sql string, sink RowSink) error {
	if rs, ok := sess.(RowStreamer); ok {
		return rs.StreamRows(ctx, sql, nil, sink)
	}
	d := DialectFor(driver)
	if d == nil {
		return errors.New("this connection does not use SQL")
	}
	inner := strings.TrimSpace(sql)
	for strings.HasSuffix(inner, ";") {
		inner = strings.TrimSpace(strings.TrimSuffix(inner, ";"))
	}
	tail := ")"
	if strings.Contains(inner, "--") || strings.Contains(inner, "#") {
		tail = "\n)"
	}
	const page = 1000
	first := true
	for offset := 0; ; offset += page {
		if err := ctx.Err(); err != nil {
			return err
		}
		q := "SELECT * FROM (" + inner + tail + " AS termocode_export" + d.Limit(page, offset)
		res := sess.Run(ctx, q, RunOptions{MaxRows: page})
		if res.Err != nil {
			return res.Err
		}
		if first && len(res.Columns) > 0 {
			if err := sink.Columns(res.Columns); err != nil {
				return err
			}
			first = false
		}
		for _, row := range res.Rows {
			if err := sink.Row(row); err != nil {
				return err
			}
		}
		if len(res.Rows) < page {
			return nil
		}
	}
}

// countingSink counts rows, reports progress and stops when ctx ends.
type countingSink struct {
	ctx      context.Context
	w        RowWriter
	n        int64
	progress func(int64)
}

func (c *countingSink) Columns(cols []Column) error { return c.w.Columns(cols) }

func (c *countingSink) Row(row []Cell) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if err := c.w.Row(row); err != nil {
		return err
	}
	c.n++
	if c.progress != nil && c.n%500 == 0 {
		c.progress(c.n)
	}
	return nil
}

// ExportToFile writes the rows src produces to path in format f and
// returns the row count. The bytes go to a temporary file in the same folder
// first and replace path only when everything is written, so a failure or a
// cancel leaves no partial file and keeps an older one. An existing file is
// replaced only when overwrite is true (ErrExportExists otherwise).
func ExportToFile(ctx context.Context, path string, f ExportFormat, o ExportOptions, overwrite bool, src func(context.Context, RowSink) error, progress func(int64)) (int64, error) {
	dir := filepath.Dir(path)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return 0, fmt.Errorf("the folder %s does not exist", dir)
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		if st.IsDir() {
			return 0, errors.New("that path is a folder")
		}
		if !overwrite {
			return 0, ErrExportExists
		}
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".termocode-export-*.tmp")
	if err != nil {
		return 0, err
	}
	done := false
	defer func() {
		if !done {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	bw := bufio.NewWriterSize(tmp, 64<<10)
	w, err := NewRowWriter(f, bw, o)
	if err != nil {
		return 0, err
	}
	sink := &countingSink{ctx: ctx, w: w, progress: progress}
	if err := src(ctx, sink); err != nil {
		return sink.n, err
	}
	if err := ctx.Err(); err != nil {
		return sink.n, err
	}
	if err := w.End(); err != nil {
		return sink.n, err
	}
	if err := bw.Flush(); err != nil {
		return sink.n, err
	}
	if err := tmp.Chmod(mode); err != nil {
		return sink.n, err
	}
	if err := tmp.Close(); err != nil {
		return sink.n, err
	}
	if !overwrite {
		if _, err := os.Lstat(path); err == nil {
			return sink.n, ErrExportExists
		}
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return sink.n, err
	}
	done = true
	return sink.n, nil
}

// WriteRows passes the loaded result r to sink (columns, then every row).
func WriteRows(sink RowSink, r Result) error { return emitResult(sink, r) }
