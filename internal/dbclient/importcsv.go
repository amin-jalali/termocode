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
}

const (
	sniffBytes      = 256 << 10
	sniffRecords    = 50
	sampleRows      = 5
	importRowsMax   = 1000
	importSavepoint = "termocode_import"
	importBatchSP   = "termocode_import_batch"
)

var sniffDelimiters = []rune{',', ';', '\t', '|'}

// SniffCSV reads the start of the file at path and guesses its encoding,
// delimiter and header. It reads at most 256 KB.
func SniffCSV(path string) (CSVSniff, error) {
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
	best, bestScore := ',', -1
	for _, d := range sniffDelimiters {
		recs := readRecords(text, d, sniffRecords, truncated)
		if score := delimiterScore(recs); score > bestScore {
			best, bestScore = d, score
		}
	}
	recs := readRecords(text, best, sampleRows+1, truncated)
	if len(recs) == 0 {
		return CSVSniff{}, errors.New("the file has no rows")
	}
	sn := CSVSniff{Delimiter: best, Encoding: enc, Fields: len(recs[0]), head: recs}
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
	r := csv.NewReader(decodeReader(br, sn.Encoding))
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
}

func (s ImportSpec) pairs() (targets []string, sources []int) {
	for i, t := range s.Targets {
		if i < len(s.Sources) && s.Sources[i] >= 0 {
			targets, sources = append(targets, t), append(sources, s.Sources[i])
		}
	}
	return targets, sources
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
	Err        error
	Cancelled  bool
	Elapsed    time.Duration
}

type importRow struct {
	args []any
	line int
	row  int
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
	r, closeFile, err := openCSV(spec.Path, spec.Sniff)
	if err != nil {
		res.Err = err
		return res
	}
	defer closeFile()
	if spec.Sniff.Header {
		if _, err := r.Read(); err != nil && err != io.EOF {
			res.Err = fmt.Errorf("the file cannot be read as CSV: %s", err)
			return res
		}
	}
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
	fail := func(row, line int, err error) ImportResult {
		undo()
		res.Rows, res.FailedRow, res.FailedLine, res.Err = 0, row, line, err
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
	rowNo := 0
	for {
		if ctx.Err() != nil {
			undo()
			res.Rows, res.Cancelled = 0, true
			return res
		}
		var batch []importRow
		eof := false
		for len(batch) < perBatch {
			rec, err := r.Read()
			if err == io.EOF {
				eof = true
				break
			}
			rowNo++
			line, _ := r.FieldPos(0)
			if err != nil {
				return fail(rowNo, line, fmt.Errorf("row %d (line %d) cannot be read: %s", rowNo, line, err))
			}
			if len(rec) != spec.Sniff.Fields {
				plural := "s"
				if len(rec) == 1 {
					plural = ""
				}
				return fail(rowNo, line, fmt.Errorf("row %d (line %d) has %d field%s, expected %d", rowNo, line, len(rec), plural, spec.Sniff.Fields))
			}
			args := make([]any, len(sources))
			for i, src := range sources {
				if rec[src] != "" {
					args[i] = rec[src]
				}
			}
			batch = append(batch, importRow{args: args, line: line, row: rowNo})
		}
		if len(batch) > 0 {
			if bad, err := insertBatch(ctx, sess, spec, batch); err != nil {
				if ctx.Err() != nil {
					undo()
					res.Rows, res.Cancelled = 0, true
					return res
				}
				return fail(bad.row, bad.line, fmt.Errorf("row %d (line %d): %w", bad.row, bad.line, err))
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
