package dbclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// CSVSniff is what the preview card shows about a file: the encoding, the
// delimiter and whether the first row is a header, all guessed from the
// start of the file (the user can change the header guess).
type CSVSniff struct {
	Delimiter rune
	Header    bool
	Encoding  string     // UTF-8, UTF-16LE, UTF-16BE or ISO-8859-1
	Fields    int        // columns in the first row
	Columns   []string   // header names, or "column N"
	Sample    [][]string // the first five data rows
	head      [][]string // the first six records, for WithHeader
	// Options are the reading options the user chose (lines to skip,
	// quote and escape characters); the zero value is plain CSV.
	Options CSVOptions
}

// QuoteNone is CSVOptions.Quote for a file without quoting: a " is
// text like any other character.
const QuoteNone rune = -1

// CSVOptions change how a file is read. Quote 0 is the double quote;
// Escape 0 means a quote inside a quoted field is doubled ("").
type CSVOptions struct {
	SkipLines int  // lines dropped before the header (or the first row)
	Quote     rune // 0 ("), '\'', or QuoteNone
	Escape    rune // 0 (doubled quote) or '\\' (backslash)
}

// quotePlaceholder stands for a literal " while a QuoteNone file is
// parsed; it is turned back into " in every field.
const quotePlaceholder = "\uE000"

func (o CSVOptions) quote() byte {
	if o.Quote == 0 || o.Quote == QuoteNone {
		return '"'
	}
	return byte(o.Quote)
}

// transforms reports whether the stream must be rewritten before the CSV
// reader sees it.
func (o CSVOptions) transforms() bool {
	return o.Quote == QuoteNone || o.Quote != 0 && o.Quote != '"' || o.Escape != 0
}

// Valid says what is wrong with the options, or nil.
func (o CSVOptions) Valid(delim rune) error {
	switch {
	case o.SkipLines < 0:
		return errors.New("lines to skip cannot be negative")
	case o.Quote != 0 && o.Quote != QuoteNone && (o.Quote < 0x21 || o.Quote > 0x7e):
		return errors.New("the quote must be one visible ASCII character")
	case o.Escape != 0 && (o.Escape < 0x21 || o.Escape > 0x7e):
		return errors.New("the escape must be one visible ASCII character")
	case o.Quote != QuoteNone && o.Quote != 0 && o.Quote == delim, o.Escape != 0 && o.Escape == delim:
		return errors.New("the quote and escape must differ from the delimiter")
	case o.Escape != 0 && rune(o.quote()) == o.Escape:
		return errors.New("an escape the same as the quote is the default (doubled quote)")
	}
	return nil
}

// mapByte is what one byte of the file becomes for the CSV reader: the
// chosen quote becomes " (and a real " takes its place), or with
// QuoteNone a " becomes the placeholder. Quote and escape are ASCII, so a
// byte of a multi-byte UTF-8 character never matches.
func (o CSVOptions) mapByte(out []byte, b byte) []byte {
	switch {
	case o.Quote == QuoteNone:
		if b == '"' {
			return append(out, quotePlaceholder...)
		}
	case o.Quote != 0 && o.Quote != '"':
		if b == byte(o.Quote) {
			return append(out, '"')
		}
		if b == '"' {
			return append(out, byte(o.Quote))
		}
	}
	return append(out, b)
}

// Restore turns a parsed field back into the file's text (it undoes what
// mapByte did).
func (o CSVOptions) Restore(field string) string {
	switch {
	case o.Quote == QuoteNone:
		return strings.ReplaceAll(field, quotePlaceholder, `"`)
	case o.Quote != 0 && o.Quote != '"':
		q := o.Quote
		return strings.Map(func(r rune) rune {
			switch r {
			case q:
				return '"'
			case '"':
				return q
			}
			return r
		}, field)
	}
	return field
}

// quoteReader rewrites the decoded stream for the CSV reader: an escaped
// quote (\" with a backslash escape) becomes a doubled one, and the quote
// character is swapped as mapByte says. Newlines are never changed, so
// line numbers stay right.
type quoteReader struct {
	r    *bufio.Reader
	o    CSVOptions
	pend []byte
}

func (q *quoteReader) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		if len(q.pend) > 0 {
			c := copy(p[n:], q.pend)
			q.pend = q.pend[c:]
			n += c
			continue
		}
		b, err := q.r.ReadByte()
		if err != nil {
			if n > 0 {
				return n, nil
			}
			return 0, err
		}
		out := make([]byte, 0, 4)
		if q.o.Escape != 0 && b == byte(q.o.Escape) {
			nx, nerr := q.r.ReadByte()
			switch {
			case nerr != nil:
				out = q.o.mapByte(out, b)
			case q.o.Quote != QuoteNone && nx == q.o.quote():
				out = append(out, '"', '"')
			case nx == b:
				out = q.o.mapByte(out, b)
			default:
				_ = q.r.UnreadByte()
				out = q.o.mapByte(out, b)
			}
		} else {
			out = q.o.mapByte(out, b)
		}
		q.pend = out
	}
	return n, nil
}

// transformText is quoteReader for text already in memory (the sniff).
func (o CSVOptions) transformText(text string) string {
	if !o.transforms() {
		return text
	}
	out, _ := io.ReadAll(&quoteReader{r: bufio.NewReader(strings.NewReader(text)), o: o})
	return string(out)
}

// dropLines removes the first n lines of text.
func dropLines(text string, n int) string {
	for ; n > 0 && text != ""; n-- {
		i := strings.IndexByte(text, '\n')
		if i < 0 {
			return ""
		}
		text = text[i+1:]
	}
	return text
}

// skipLines reads and drops the first n lines of br.
func skipLines(br *bufio.Reader, n int) error {
	for ; n > 0; n-- {
		for {
			_, err := br.ReadSlice('\n')
			if err == nil {
				break
			}
			if err == bufio.ErrBufferFull {
				continue
			}
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
	return nil
}

const (
	sniffBytes      = 256 << 10
	sniffRecords    = 50
	sampleRows      = 5
	importRowsMax   = 1000
	importSavepoint = "termocode_import"
	importBatchSP   = "termocode_import_batch"
	importRowSP     = "termocode_import_row"
	// importSkipKeep is how many skipped rows a result lists (the count
	// has them all).
	importSkipKeep = 1000
)

var sniffDelimiters = []rune{',', ';', '\t', '|'}

// SniffCSV reads the start of the file at path and guesses its encoding,
// delimiter and header. It reads at most 256 KB.
func SniffCSV(path string) (CSVSniff, error) {
	return SniffCSVWith(path, CSVOptions{})
}

// SniffCSVWith is SniffCSV with reading options: the skipped lines and the
// quote and escape characters are applied before the guesses.
func SniffCSVWith(path string, opts CSVOptions) (CSVSniff, error) {
	f, err := os.Open(path)
	if err != nil {
		return CSVSniff{}, fmt.Errorf("cannot open the file: %s", pathErr(err))
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.IsDir() {
		return CSVSniff{}, errors.New("that path is a folder")
	}
	raw, err := io.ReadAll(io.LimitReader(f, sniffBytes))
	if err != nil {
		return CSVSniff{}, fmt.Errorf("cannot read the file: %s", pathErr(err))
	}
	truncated := len(raw) == sniffBytes
	enc, body := detectEncoding(raw, truncated)
	text := decodeBytes(body, enc)
	if strings.TrimSpace(text) == "" {
		return CSVSniff{}, errors.New("the file is empty")
	}
	if opts.SkipLines > 0 {
		text = dropLines(text, opts.SkipLines)
		if strings.TrimSpace(text) == "" {
			return CSVSniff{}, fmt.Errorf("nothing is left after skipping %d lines", opts.SkipLines)
		}
	}
	text = opts.transformText(text)
	best, bestScore := ',', -1
	for _, d := range sniffDelimiters {
		if opts.Valid(d) != nil {
			continue
		}
		recs := readRecords(text, d, sniffRecords, truncated)
		if score := delimiterScore(recs); score > bestScore {
			best, bestScore = d, score
		}
	}
	if err := opts.Valid(best); err != nil {
		return CSVSniff{}, err
	}
	recs := readRecords(text, best, sampleRows+1, truncated)
	if len(recs) == 0 {
		return CSVSniff{}, errors.New("the file has no rows")
	}
	for _, rec := range recs {
		for i := range rec {
			rec[i] = opts.Restore(rec[i])
		}
	}
	sn := CSVSniff{Delimiter: best, Encoding: enc, Fields: len(recs[0]), head: recs, Options: opts}
	return sn.WithHeader(looksLikeHeader(recs[0])), nil
}

// WithHeader is the sniff with the first row taken as the header (or as data).
func (s CSVSniff) WithHeader(header bool) CSVSniff {
	s.Header = header
	s.Columns, s.Sample = nil, nil
	data := s.head
	if header && len(data) > 0 {
		data = data[1:]
	}
	for i := 0; i < s.Fields; i++ {
		name := ""
		if header && i < len(s.head[0]) {
			name = strings.TrimSpace(s.head[0][i])
		}
		if name == "" {
			name = fmt.Sprintf("column %d", i+1)
		}
		s.Columns = append(s.Columns, name)
	}
	for _, r := range data {
		if len(s.Sample) == sampleRows {
			break
		}
		s.Sample = append(s.Sample, r)
	}
	return s
}

func pathErr(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

func detectEncoding(raw []byte, truncated bool) (string, []byte) {
	switch {
	case bytes.HasPrefix(raw, []byte{0xFF, 0xFE}):
		return "UTF-16LE", raw[2:]
	case bytes.HasPrefix(raw, []byte{0xFE, 0xFF}):
		return "UTF-16BE", raw[2:]
	case bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}):
		return "UTF-8", raw[3:]
	}
	probe := raw
	if truncated { // the cut may fall inside a character
		for k := 0; k < utf8.UTFMax-1 && len(probe) > 0 && !utf8.Valid(probe); k++ {
			probe = probe[:len(probe)-1]
		}
	}
	if utf8.Valid(probe) {
		return "UTF-8", raw
	}
	return "ISO-8859-1", raw
}

func decodeBytes(b []byte, enc string) string {
	out, _ := io.ReadAll(decodeReader(bytes.NewReader(b), enc))
	return string(out)
}

func readRecords(text string, delim rune, limit int, dropLast bool) [][]string {
	r := csv.NewReader(strings.NewReader(text))
	r.Comma, r.FieldsPerRecord, r.LazyQuotes = delim, -1, true
	var out [][]string
	for len(out) < limit {
		rec, err := r.Read()
		if err != nil {
			break
		}
		out = append(out, append([]string(nil), rec...))
	}
	if dropLast && len(out) == limit {
		out = out[:len(out)-1]
	}
	return out
}

// delimiterScore prefers the delimiter that gives the same number (above
// one) of fields in the most rows.
func delimiterScore(recs [][]string) int {
	counts := map[int]int{}
	for _, r := range recs {
		counts[len(r)]++
	}
	best, fields := 0, 0
	for n, c := range counts {
		if n > 1 && (c > best || c == best && n > fields) {
			best, fields = c, n
		}
	}
	if best == 0 {
		return 0
	}
	return best*1000 + fields
}

func looksLikeHeader(first []string) bool {
	seen := map[string]bool{}
	for _, c := range first {
		c = strings.TrimSpace(c)
		if c == "" {
			return false
		}
		if _, err := strconv.ParseFloat(c, 64); err == nil {
			return false
		}
		k := strings.ToLower(c)
		if seen[k] {
			return false
		}
		seen[k] = true
	}
	return true
}

// decodeReader turns the bytes of enc into UTF-8 as they are read.
func decodeReader(r io.Reader, enc string) io.Reader {
	switch enc {
	case "UTF-16LE", "UTF-16BE":
		le := enc == "UTF-16LE"
		return &convReader{src: r, conv: func(in []byte) ([]byte, int) {
			var out []byte
			i := 0
			for ; i+1 < len(in); i += 2 {
				u := uint16(in[i]) | uint16(in[i+1])<<8
				if !le {
					u = uint16(in[i])<<8 | uint16(in[i+1])
				}
				if utf16.IsSurrogate(rune(u)) {
					if i+3 >= len(in) {
						break // wait for the low half
					}
					u2 := uint16(in[i+2]) | uint16(in[i+3])<<8
					if !le {
						u2 = uint16(in[i+2])<<8 | uint16(in[i+3])
					}
					out = utf8.AppendRune(out, utf16.DecodeRune(rune(u), rune(u2)))
					i += 2
					continue
				}
				out = utf8.AppendRune(out, rune(u))
			}
			return out, i
		}}
	case "ISO-8859-1":
		return &convReader{src: r, conv: func(in []byte) ([]byte, int) {
			out := make([]byte, 0, len(in)+len(in)/4)
			for _, b := range in {
				out = utf8.AppendRune(out, rune(b))
			}
			return out, len(in)
		}}
	}
	return r
}

// convReader converts a stream in steps; conv returns the output for the
// complete units at the start of in and how many bytes it used.
type convReader struct {
	src  io.Reader
	conv func(in []byte) (out []byte, used int)
	in   []byte
	out  []byte
	buf  [32 << 10]byte
	err  error
}

func (c *convReader) Read(p []byte) (int, error) {
	for len(c.out) == 0 {
		if c.err != nil {
			return 0, c.err
		}
		n, err := c.src.Read(c.buf[:])
		c.in = append(c.in, c.buf[:n]...)
		out, used := c.conv(c.in)
		c.out = out
		c.in = append(c.in[:0], c.in[used:]...)
		if err != nil {
			c.err = err
		}
	}
	n := copy(p, c.out)
	c.out = c.out[n:]
	return n, nil
}

// openCSV returns a reader of the file's records and a close func.
func openCSV(path string, sn CSVSniff) (*csv.Reader, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open the file: %s", pathErr(err))
	}
	br := bufio.NewReaderSize(f, 64<<10)
	skip := 0
	if head, _ := br.Peek(3); len(head) >= 2 {
		switch {
		case head[0] == 0xFF && head[1] == 0xFE, head[0] == 0xFE && head[1] == 0xFF:
			skip = 2
		case len(head) == 3 && head[0] == 0xEF && head[1] == 0xBB && head[2] == 0xBF:
			skip = 3
		}
	}
	_, _ = br.Discard(skip)
	var src io.Reader = decodeReader(br, sn.Encoding)
	if sn.Options.SkipLines > 0 || sn.Options.transforms() {
		dr := bufio.NewReaderSize(src, 64<<10)
		if err := skipLines(dr, sn.Options.SkipLines); err != nil {
			_ = f.Close()
			return nil, nil, fmt.Errorf("cannot read the file: %s", pathErr(err))
		}
		src = dr
		if sn.Options.transforms() {
			src = &quoteReader{r: dr, o: sn.Options}
		}
	}
	r := csv.NewReader(src)
	r.Comma, r.FieldsPerRecord, r.LazyQuotes, r.ReuseRecord = sn.Delimiter, -1, true, true
	return r, func() { _ = f.Close() }, nil
}

// CountCSVRows counts the data rows of the file (a header is not one),
// reading it once from start to end without keeping it.
func CountCSVRows(ctx context.Context, path string, sn CSVSniff) (int, error) {
	r, closeFile, err := openCSV(path, sn)
	if err != nil {
		return 0, err
	}
	defer closeFile()
	n := 0
	for {
		if n%1000 == 0 {
			if err := ctx.Err(); err != nil {
				return n, err
			}
		}
		_, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, fmt.Errorf("the file cannot be read as CSV: %s", err)
		}
		n++
	}
	if sn.Header && n > 0 {
		n--
	}
	return n, nil
}

// AutoMap maps each target column to the file column of the same name (any
// case), or by position when the file has no header. -1: no source.
func AutoMap(targets []string, sn CSVSniff) []int {
	out := make([]int, len(targets))
	used := map[int]bool{}
	for i, t := range targets {
		out[i] = -1
		if !sn.Header {
			if i < len(sn.Columns) {
				out[i] = i
			}
			continue
		}
		for j, c := range sn.Columns {
			if !used[j] && strings.EqualFold(strings.TrimSpace(c), strings.TrimSpace(t)) {
				out[i], used[j] = j, true
				break
			}
		}
	}
	return out
}

// ImportSpec says what to import: the file, where it goes and which file
// column fills which table column (Targets[i] is filled from Sources[i]).
type ImportSpec struct {
	Path      string
	Sniff     CSVSniff
	Dialect   Dialect
	Schema    string
	Table     string
	Targets   []string
	Sources   []int
	BatchRows int  // rows per INSERT (default and cap 1000)
	InTx      bool // run inside the user's open transaction (a savepoint)
	// SkipBad leaves a row that cannot be read or fails its INSERT out
	// (in ImportResult.Skipped) instead of rolling everything back.
	SkipBad bool
	// MorePaths are more files read after Path into the same table, in
	// the same transaction (Sniff applies to all of them).
	MorePaths []string
	// Formats[i] says how the cells of Targets[i] are read (none: as is).
	Formats []ColumnFormat
}

func (s ImportSpec) pairs() (targets []string, sources []int) {
	for i, t := range s.Targets {
		if i < len(s.Sources) && s.Sources[i] >= 0 {
			targets, sources = append(targets, t), append(sources, s.Sources[i])
		}
	}
	return targets, sources
}

// pairFormats is Formats in the order of pairs.
func (s ImportSpec) pairFormats() []ColumnFormat {
	var out []ColumnFormat
	for i := range s.Targets {
		if i < len(s.Sources) && s.Sources[i] >= 0 {
			var f ColumnFormat
			if i < len(s.Formats) {
				f = s.Formats[i]
			}
			out = append(out, f)
		}
	}
	return out
}

// Paths are all the files of the import, Path first.
func (s ImportSpec) Paths() []string {
	return append([]string{s.Path}, s.MorePaths...)
}

// ColumnFormat says how the cells of one column are turned into values.
// An empty cell is always NULL.
type ColumnFormat struct {
	Trim     bool   // drop spaces around the text
	NullText string // this text (exactly) is NULL too, for example \N or NULL
	// DateLayout is the date format of the file, written with YYYY YY MM
	// DD HH mm ss (for example DD/MM/YYYY); the value is sent as
	// YYYY-MM-DD (with HH:mm:ss when the format has a time).
	DateLayout   string
	DecimalComma bool // 1.234,5 is sent as 1234.5
}

// IsZero reports whether the format leaves the cells as they are.
func (f ColumnFormat) IsZero() bool { return f == ColumnFormat{} }

// Describe is a short text of the format for a card ("" when IsZero).
func (f ColumnFormat) Describe() string {
	var parts []string
	if f.Trim {
		parts = append(parts, "trim")
	}
	if f.NullText != "" {
		parts = append(parts, "NULL = "+strconv.Quote(f.NullText))
	}
	if f.DateLayout != "" {
		parts = append(parts, "date "+f.DateLayout)
	}
	if f.DecimalComma {
		parts = append(parts, "decimal comma")
	}
	return strings.Join(parts, " · ")
}

var dateTokens = strings.NewReplacer("YYYY", "2006", "YY", "06", "MM", "01", "DD", "02", "HH", "15", "mm", "04", "ss", "05")

// goDateLayout turns DD/MM/YYYY style into a Go layout, and says whether
// it has a time part.
func goDateLayout(layout string) (string, bool, error) {
	if strings.TrimSpace(layout) == "" {
		return "", false, errors.New("type a date format like DD/MM/YYYY")
	}
	for _, c := range layout {
		if c >= '0' && c <= '9' {
			return "", false, errors.New("write the date format with YYYY, MM, DD, HH, mm and ss, not digits")
		}
	}
	g := dateTokens.Replace(layout)
	if !strings.Contains(g, "2006") && !strings.Contains(g, "06") || !strings.Contains(g, "01") || !strings.Contains(g, "02") {
		return "", false, errors.New("the date format needs a year (YYYY), a month (MM) and a day (DD)")
	}
	return g, strings.Contains(g, "15"), nil
}

// CheckDateLayout says what is wrong with a date format, or nil.
func CheckDateLayout(layout string) error {
	_, _, err := goDateLayout(layout)
	return err
}

// Apply turns one cell into the value bound for it (nil is NULL).
func (f ColumnFormat) Apply(v string) (any, error) {
	if f.Trim {
		v = strings.TrimSpace(v)
	}
	if v == "" || f.NullText != "" && v == f.NullText {
		return nil, nil
	}
	if f.DecimalComma {
		n := strings.NewReplacer(".", "", " ", "", "\u00a0", "", "'", "").Replace(v)
		n = strings.Replace(n, ",", ".", 1)
		if _, err := strconv.ParseFloat(n, 64); err != nil {
			return nil, fmt.Errorf("%q is not a number with a decimal comma", v)
		}
		v = n
	}
	if f.DateLayout != "" {
		g, withTime, err := goDateLayout(f.DateLayout)
		if err != nil {
			return nil, err
		}
		t, err := time.Parse(g, v)
		if err != nil {
			return nil, fmt.Errorf("%q does not match the date format %s", v, f.DateLayout)
		}
		if withTime {
			return t.Format("2006-01-02 15:04:05"), nil
		}
		return t.Format("2006-01-02"), nil
	}
	return v, nil
}

// csvSource reads the records of one or more files in turn; the header
// row of each file is dropped.
type csvSource struct {
	paths []string
	sn    CSVSniff
	idx   int
	r     *csv.Reader
	close func()
	row   int // data row in the current file
}

func (c *csvSource) file() string {
	if c.idx < len(c.paths) {
		return c.paths[c.idx]
	}
	return ""
}

// next is the next record, its data row and line in its file, or io.EOF
// after the last file.
func (c *csvSource) next() (rec []string, row, line int, err error) {
	for {
		if c.r == nil {
			if c.idx >= len(c.paths) {
				return nil, 0, 0, io.EOF
			}
			r, closeFile, err := openCSV(c.paths[c.idx], c.sn)
			if err != nil {
				return nil, 0, 0, err
			}
			c.r, c.close, c.row = r, closeFile, 0
			if c.sn.Header {
				if _, err := r.Read(); err != nil && err != io.EOF {
					return nil, 0, 0, fmt.Errorf("the file cannot be read as CSV: %s", err)
				}
			}
		}
		rec, err := c.r.Read()
		if err == io.EOF {
			c.close()
			c.r, c.close = nil, nil
			c.idx++
			continue
		}
		c.row++
		var pe *csv.ParseError
		switch {
		case err == nil:
			line, _ = c.r.FieldPos(0)
		case errors.As(err, &pe):
			line = pe.StartLine
		}
		line += c.sn.Options.SkipLines
		return rec, c.row, line, err
	}
}

func (c *csvSource) done() {
	if c.close != nil {
		c.close()
		c.r, c.close = nil, nil
	}
}

// rowsPerBatch keeps a statement under the bound-value limit of the dialect.
func (s ImportSpec) rowsPerBatch() int {
	n := s.BatchRows
	if n <= 0 || n > importRowsMax {
		n = importRowsMax
	}
	maxParams := 60000
	if _, lite := s.Dialect.(SQLiteDialect); lite {
		maxParams = 900
	}
	cols, _ := s.pairs()
	if len(cols) > 0 {
		n = min(n, max(maxParams/len(cols), 1))
	}
	return n
}

func (s ImportSpec) insertSQL(rows int) string {
	cols, _ := s.pairs()
	d := s.Dialect
	table := d.QuoteIdent(s.Table)
	if s.Schema != "" {
		table = d.QuoteIdent(s.Schema) + "." + table
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = d.QuoteIdent(c)
	}
	var b strings.Builder
	b.WriteString("INSERT INTO " + table + " (" + strings.Join(names, ", ") + ") VALUES ")
	n := 0
	for r := 0; r < rows; r++ {
		if r > 0 {
			b.WriteString(", ")
		}
		b.WriteByte('(')
		for c := range cols {
			if c > 0 {
				b.WriteString(", ")
			}
			n++
			b.WriteString(d.Placeholder(n))
		}
		b.WriteByte(')')
	}
	return b.String()
}

// ImportResult is how an import ended. FailedRow is the data row (1 is the
// first after the header) and FailedLine the line in the file; both 0 when
// no row failed. After any failure or a cancel nothing was written (Rows 0).
type ImportResult struct {
	Rows       int64
	FailedRow  int
	FailedLine int
	FailedFile string // set when the import has more than one file
	Err        error
	Cancelled  bool
	Elapsed    time.Duration
	// SkippedRows counts the rows left out with SkipBad; Skipped lists the
	// first importSkipKeep of them.
	SkippedRows int
	Skipped     []ImportSkip
}

// ImportSkip is one row a SkipBad import left out. File is set when the
// import read more than one file.
type ImportSkip struct {
	Row, Line int
	Err       string
	File      string
}

func (res *ImportResult) skip(row, line int, file string, err error) {
	res.SkippedRows++
	if len(res.Skipped) < importSkipKeep {
		res.Skipped = append(res.Skipped, ImportSkip{Row: row, Line: line, Err: err.Error(), File: file})
	}
}

// ImportErrorLog is the text of the error log of a SkipBad import: what
// was imported where, then one line per skipped row.
func ImportErrorLog(file, table string, res ImportResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Import of %s into %s\n%d rows imported, %d skipped\n\n", file, table, res.Rows, res.SkippedRows)
	for _, sk := range res.Skipped {
		where := ""
		if sk.File != "" {
			where = sk.File + ": "
		}
		fmt.Fprintf(&b, "%srow %d (line %d): %s\n", where, sk.Row, sk.Line, strings.Join(strings.Fields(sk.Err), " "))
	}
	if res.SkippedRows > len(res.Skipped) {
		fmt.Fprintf(&b, "… and %d more rows (only the first %d are listed)\n", res.SkippedRows-len(res.Skipped), len(res.Skipped))
	}
	return b.String()
}

type importRow struct {
	args []any
	line int
	row  int
	file string // "" when the import has one file
}

// where names the row in errors: "row 3 (line 4)", with the file first
// when the import has more than one.
func (r importRow) where() string {
	if r.file != "" {
		return fmt.Sprintf("%s: row %d (line %d)", r.file, r.row, r.line)
	}
	return fmt.Sprintf("row %d (line %d)", r.row, r.line)
}

// ImportCSV reads the file one row at a time and inserts it with bound
// values (never text in the SQL) in INSERTs of up to 1000 rows, all inside
// ONE transaction: the first row that fails rolls everything back and is
// named in the result. A batch that fails is rolled back to a savepoint and
// tried row by row, which finds the row. Cancelling rolls back too.
// progress gets the number of rows done after each batch.
func ImportCSV(ctx context.Context, sess Session, spec ImportSpec, progress func(int64)) (res ImportResult) {
	start := time.Now()
	defer func() { res.Elapsed = time.Since(start) }()
	targets, sources := spec.pairs()
	if len(targets) == 0 {
		res.Err = errors.New("map at least one column")
		return res
	}
	if err := spec.Sniff.Options.Valid(spec.Sniff.Delimiter); err != nil {
		res.Err = err
		return res
	}
	formats := spec.pairFormats()
	for _, f := range formats {
		if f.DateLayout != "" {
			if err := CheckDateLayout(f.DateLayout); err != nil {
				res.Err = err
				return res
			}
		}
	}
	paths := spec.Paths()
	multi := len(paths) > 1
	// Every file must open before anything is written.
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			res.Err = fmt.Errorf("cannot open the file: %s", pathErr(err))
			if multi {
				res.Err = fmt.Errorf("cannot open the file %s: %s", p, pathErr(err))
			}
			return res
		}
		_ = f.Close()
	}
	src := &csvSource{paths: paths, sn: spec.Sniff}
	defer src.done()
	if err := ctx.Err(); err != nil {
		res.Cancelled = true
		return res
	}
	undo := func() {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if spec.InTx {
			if r := sess.Run(cctx, "ROLLBACK TO SAVEPOINT "+importSavepoint, RunOptions{}); r.Err == nil {
				sess.Run(cctx, "RELEASE SAVEPOINT "+importSavepoint, RunOptions{})
			}
			return
		}
		_ = sess.Rollback(cctx)
	}
	fail := func(row, line int, file string, err error) ImportResult {
		undo()
		res.Rows, res.FailedRow, res.FailedLine, res.FailedFile, res.Err = 0, row, line, file, err
		return res
	}
	if spec.InTx {
		if r := sess.Run(ctx, "SAVEPOINT "+importSavepoint, RunOptions{}); r.Err != nil {
			res.Err = r.Err
			return res
		}
	} else if err := sess.Begin(ctx); err != nil {
		res.Err = err
		return res
	}
	perBatch := spec.rowsPerBatch()
	var done int64
	for {
		if ctx.Err() != nil {
			undo()
			res.Rows, res.Cancelled = 0, true
			return res
		}
		var batch []importRow
		eof := false
	readRows:
		for len(batch) < perBatch {
			rec, rowNo, line, err := src.next()
			if err == io.EOF {
				eof = true
				break
			}
			file := ""
			if multi {
				file = src.file()
			}
			at := importRow{row: rowNo, line: line, file: file}
			if rowNo == 0 { // the file itself could not be opened or read
				return fail(0, 0, file, err)
			}
			bad := func(e error) bool { // true: the import stops
				if spec.SkipBad {
					res.skip(rowNo, line, file, e)
					return false
				}
				return true
			}
			if err != nil {
				e := fmt.Errorf("%s cannot be read: %s", at.where(), err)
				if bad(e) {
					return fail(rowNo, line, file, e)
				}
				continue
			}
			if len(rec) != spec.Sniff.Fields {
				plural := "s"
				if len(rec) == 1 {
					plural = ""
				}
				e := fmt.Errorf("%s has %d field%s, expected %d", at.where(), len(rec), plural, spec.Sniff.Fields)
				if bad(e) {
					return fail(rowNo, line, file, e)
				}
				continue
			}
			args := make([]any, len(sources))
			for i, si := range sources {
				v, err := formats[i].Apply(spec.Sniff.Options.Restore(rec[si]))
				if err != nil {
					e := fmt.Errorf("%s, column %s: %s", at.where(), targets[i], err)
					if bad(e) {
						return fail(rowNo, line, file, e)
					}
					continue readRows
				}
				args[i] = v
			}
			at.args = args
			batch = append(batch, at)
		}
		if len(batch) > 0 && spec.SkipBad {
			n, err := insertBatchSkip(ctx, sess, spec, batch, &res)
			if err != nil {
				undo()
				if ctx.Err() != nil {
					res.Rows, res.Cancelled = 0, true
					return res
				}
				res.Rows, res.Err = 0, err
				return res
			}
			done += int64(n)
			if progress != nil {
				progress(done)
			}
		} else if len(batch) > 0 {
			if bad, err := insertBatch(ctx, sess, spec, batch); err != nil {
				if ctx.Err() != nil {
					undo()
					res.Rows, res.Cancelled = 0, true
					return res
				}
				return fail(bad.row, bad.line, bad.file, fmt.Errorf("%s: %w", bad.where(), err))
			}
			done += int64(len(batch))
			if progress != nil {
				progress(done)
			}
		}
		if eof {
			break
		}
	}
	if spec.InTx {
		if r := sess.Run(ctx, "RELEASE SAVEPOINT "+importSavepoint, RunOptions{}); r.Err != nil {
			undo()
			res.Err = r.Err
			return res
		}
	} else if err := sess.Commit(ctx); err != nil {
		undo()
		res.Err = err
		return res
	}
	res.Rows = done
	return res
}

// insertBatch inserts the batch in one statement under a savepoint. When it
// fails it goes back to the savepoint and tries the rows one at a time to
// find the row that fails (returned with the error).
func insertBatch(ctx context.Context, sess Session, spec ImportSpec, batch []importRow) (importRow, error) {
	if r := sess.Run(ctx, "SAVEPOINT "+importBatchSP, RunOptions{}); r.Err != nil {
		return batch[0], r.Err
	}
	args := make([]any, 0, len(batch)*len(batch[0].args))
	for _, row := range batch {
		args = append(args, row.args...)
	}
	r := sess.Run(ctx, spec.insertSQL(len(batch)), RunOptions{Args: args})
	if r.Err == nil {
		if rel := sess.Run(ctx, "RELEASE SAVEPOINT "+importBatchSP, RunOptions{}); rel.Err != nil {
			return batch[0], rel.Err
		}
		return importRow{}, nil
	}
	if len(batch) == 1 {
		return batch[0], r.Err
	}
	if ctx.Err() != nil {
		return batch[0], r.Err
	}
	if back := sess.Run(context.WithoutCancel(ctx), "ROLLBACK TO SAVEPOINT "+importBatchSP, RunOptions{}); back.Err != nil {
		return batch[0], r.Err
	}
	for _, row := range batch {
		if one := sess.Run(ctx, spec.insertSQL(1), RunOptions{Args: row.args}); one.Err != nil {
			return row, one.Err
		}
	}
	// Every row went in alone (the batch statement itself was the problem).
	if rel := sess.Run(ctx, "RELEASE SAVEPOINT "+importBatchSP, RunOptions{}); rel.Err != nil {
		return batch[0], rel.Err
	}
	return importRow{}, nil
}

// insertBatchSkip is insertBatch for a SkipBad import: when the batch
// fails, each row is tried under its own savepoint and a failing row is
// rolled back alone and recorded in res. It returns the rows inserted; an
// error means the transaction itself is broken (or cancelled).
func insertBatchSkip(ctx context.Context, sess Session, spec ImportSpec, batch []importRow, res *ImportResult) (int, error) {
	if r := sess.Run(ctx, "SAVEPOINT "+importBatchSP, RunOptions{}); r.Err != nil {
		return 0, r.Err
	}
	args := make([]any, 0, len(batch)*len(batch[0].args))
	for _, row := range batch {
		args = append(args, row.args...)
	}
	if r := sess.Run(ctx, spec.insertSQL(len(batch)), RunOptions{Args: args}); r.Err == nil {
		if rel := sess.Run(ctx, "RELEASE SAVEPOINT "+importBatchSP, RunOptions{}); rel.Err != nil {
			return 0, rel.Err
		}
		return len(batch), nil
	} else if ctx.Err() != nil {
		return 0, r.Err
	}
	if back := sess.Run(context.WithoutCancel(ctx), "ROLLBACK TO SAVEPOINT "+importBatchSP, RunOptions{}); back.Err != nil {
		return 0, back.Err
	}
	n := 0
	for _, row := range batch {
		if r := sess.Run(ctx, "SAVEPOINT "+importRowSP, RunOptions{}); r.Err != nil {
			return 0, r.Err
		}
		if one := sess.Run(ctx, spec.insertSQL(1), RunOptions{Args: row.args}); one.Err != nil {
			if ctx.Err() != nil {
				return 0, one.Err
			}
			if back := sess.Run(context.WithoutCancel(ctx), "ROLLBACK TO SAVEPOINT "+importRowSP, RunOptions{}); back.Err != nil {
				return 0, back.Err
			}
			res.skip(row.row, row.line, row.file, fmt.Errorf("%s: %w", row.where(), one.Err))
			continue
		}
		if rel := sess.Run(ctx, "RELEASE SAVEPOINT "+importRowSP, RunOptions{}); rel.Err != nil {
			return 0, rel.Err
		}
		n++
	}
	if rel := sess.Run(ctx, "RELEASE SAVEPOINT "+importBatchSP, RunOptions{}); rel.Err != nil {
		return 0, rel.Err
	}
	return n, nil
}
