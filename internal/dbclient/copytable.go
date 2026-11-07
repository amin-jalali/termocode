package dbclient

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Copy table data between connections (DBX-19). The rows of the source
// table are written to a private CSV file exactly as the server sent them
// (no spreadsheet formula quote, NULL as an empty cell) and then go through
// the normal import card into the target table. SniffCopyCSV reads that file
// back with the shape it is known to have: comma, a header row, UTF-8.

// copyCSVWriter is the RowWriter of a copy file.
type copyCSVWriter struct {
	w    *csv.Writer
	cols int
}

// NewCopyCSVWriter writes a header row and then every cell as its text.
func NewCopyCSVWriter(w io.Writer) RowWriter {
	return &copyCSVWriter{w: csv.NewWriter(w)}
}

func (c *copyCSVWriter) Columns(cols []Column) error {
	names := make([]string, len(cols))
	for i, col := range cols {
		names[i] = col.Name
	}
	c.cols = len(cols)
	return c.w.Write(names)
}

func (c *copyCSVWriter) Row(row []Cell) error {
	if c.cols == 0 {
		return errors.New("row before columns")
	}
	rec := make([]string, c.cols)
	for i := range rec {
		if i < len(row) && !row[i].Null {
			rec[i] = row[i].Text
		}
	}
	return c.w.Write(rec)
}

func (c *copyCSVWriter) End() error {
	c.w.Flush()
	return c.w.Error()
}

// WriteCopyCSV creates path (it must not exist; mode 0600) and writes the
// rows src produces into it. It returns the row count. On any error or a
// cancel the file is removed, so a failed copy leaves nothing behind.
func WriteCopyCSV(ctx context.Context, path string, src func(context.Context, RowSink) error, progress func(int64)) (n int64, err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, err
	}
	defer func() {
		if cerr := f.Close(); err == nil && cerr != nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	bw := bufio.NewWriterSize(f, 64<<10)
	w := NewCopyCSVWriter(bw)
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
	if sink.n == 0 && w.(*copyCSVWriter).cols == 0 {
		return 0, errors.New("the source table sent no columns")
	}
	return sink.n, nil
}

// SniffCopyCSV is SniffCSV for a file WriteCopyCSV wrote: comma, header,
// UTF-8, never guessed (a guess could pick ; or | when the data has many).
func SniffCopyCSV(path string) (CSVSniff, error) {
	f, err := os.Open(path)
	if err != nil {
		return CSVSniff{}, fmt.Errorf("cannot open the file: %s", pathErr(err))
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, sniffBytes))
	if err != nil {
		return CSVSniff{}, fmt.Errorf("cannot read the file: %s", pathErr(err))
	}
	text := string(raw)
	if strings.TrimSpace(text) == "" {
		return CSVSniff{}, errors.New("the file is empty")
	}
	recs := readRecords(text, ',', sampleRows+1, len(raw) == sniffBytes)
	if len(recs) == 0 {
		return CSVSniff{}, errors.New("the file has no rows")
	}
	sn := CSVSniff{Delimiter: ',', Encoding: "UTF-8", Fields: len(recs[0]), head: recs}
	return sn.WithHeader(true), nil
}
