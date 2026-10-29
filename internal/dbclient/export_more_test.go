package dbclient

import (
	"context"
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

// A SQL session streams the export with one cursor (no LIMIT pages); a
// session in a manual transaction reads through it.
func TestStreamRowsUsesTheSessionCursor(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"n"}, Rows: [][]driver.Value{{int64(1)}, {int64(2)}}}
	s := stubSession(t, "export-stream", stub, PostgresDialect{}, "postgres")
	if err := s.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	n := 0
	err := StreamRows(context.Background(), s, "postgres", "SELECT n FROM t", sinkFunc{cols: func([]Column) error { return nil }, row: func([]Cell) error { n++; return nil }})
	if err != nil || n != 2 {
		t.Fatalf("rows %d err %v", n, err)
	}
	if got := stub.Recorded(); len(got) != 1 || got[0] != "SELECT n FROM t" {
		t.Fatalf("statements %q", got)
	}
	_ = s.Rollback(context.Background())
}

// Streaming errors: closed session, server error, dropped connection,
// a sink that refuses the header, and a cancel that the driver words its
// own way all reach the caller.
func TestSQLSessionStreamRowsErrors(t *testing.T) {
	ctx := context.Background()
	ok := sinkFunc{cols: func([]Column) error { return nil }, row: func([]Cell) error { return nil }}
	stub := &dbtest.Stub{Columns: []string{"n"}, Rows: [][]driver.Value{{int64(1)}, {int64(2)}}}
	s := stubSession(t, "stream-errors", stub, PostgresDialect{}, "postgres")
	stub.QueryError = errors.New(`relation "nope" does not exist`)
	if err := s.StreamRows(ctx, "SELECT * FROM nope", nil, ok); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("server error = %v", err)
	}
	stub.QueryError = nil
	stub.RowsError, stub.RowsBefore = errors.New("connection reset by peer"), 1
	if err := s.StreamRows(ctx, "SELECT n FROM t", nil, ok); err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("drop = %v", err)
	}
	stub.RowsError = nil
	refuse := errors.New("disk full")
	if err := s.StreamRows(ctx, "SELECT n FROM t", nil, sinkFunc{cols: func([]Column) error { return refuse }, row: ok.row}); !errors.Is(err, refuse) {
		t.Errorf("header refused = %v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := streamError(cctx, errors.New("pq: canceling statement due to user request")); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel = %v", err)
	}
	_ = s.Close()
	if err := s.StreamRows(ctx, "SELECT 1", nil, ok); err == nil || err.Error() != "database session is closed" {
		t.Errorf("closed = %v", err)
	}
}

// The paged export: a trailing "--" comment cannot swallow the closing
// parenthesis; a server error or a cancel stops it; Redis has no SQL.
func TestStreamRowsPagedEdgeCases(t *testing.T) {
	ok := sinkFunc{cols: func([]Column) error { return nil }, row: func([]Cell) error { return nil }}
	p := &pagedSession{total: 3}
	if err := StreamRows(context.Background(), p, "mysql", "SELECT n FROM t -- newest first", ok); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.queries[0], "-- newest first\n) AS termocode_export") {
		t.Errorf("query %q", p.queries[0])
	}
	if err := StreamRows(context.Background(), &errSession{err: errors.New("permission denied for table t")}, "postgres", "SELECT * FROM t", ok); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("server error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := StreamRows(ctx, &pagedSession{total: 3}, "postgres", "SELECT n FROM t", ok); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel = %v", err)
	}
	if err := StreamRows(context.Background(), &pagedSession{}, "redis", "GET x", ok); err == nil || !strings.Contains(err.Error(), "does not use SQL") {
		t.Errorf("redis = %v", err)
	}
	if err := StreamRows(context.Background(), &pagedSession{total: 3}, "postgres", "SELECT n FROM t", sinkFunc{cols: func([]Column) error { return errors.New("no room") }, row: ok.row}); err == nil {
		t.Error("header error lost")
	}
}

type errSession struct {
	Session
	err error
}

func (e *errSession) Run(context.Context, string, RunOptions) Result { return Result{Err: e.err} }

// failWriter fails every write, like a full disk.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("no space left on device") }

func TestExportWritersEdgeCases(t *testing.T) {
	if _, err := NewRowWriter("xlsx", failWriter{}, ExportOptions{}); err == nil || !strings.Contains(err.Error(), `unknown export format "xlsx"`) {
		t.Errorf("unknown format = %v", err)
	}
	md, _ := NewRowWriter(FormatMarkdown, failWriter{}, ExportOptions{})
	if err := WriteResult(md, exportResult()); err == nil || !strings.Contains(err.Error(), "no space") {
		t.Errorf("markdown on a full disk = %v", err)
	}
	// A short row in Markdown leaves the missing cells empty, not NULL.
	got := exportString(t, FormatMarkdown, ExportOptions{}, Result{Columns: []Column{{Name: "a"}, {Name: "b"}}, Rows: [][]Cell{{{Text: "1"}}}})
	if !strings.Contains(got, "| 1 |  |") {
		t.Errorf("markdown:\n%s", got)
	}
	var b strings.Builder
	w, _ := NewRowWriter(FormatSQL, &b, ExportOptions{Driver: "postgres", Table: "t"})
	if err := w.Row([]Cell{{Text: "1"}}); err == nil {
		t.Error("a row before the columns was written")
	}
	if got := SQLLiteral(PostgresDialect{}, Cell{Text: "f", Kind: "bool"}); got != "FALSE" {
		t.Errorf("bool = %q", got)
	}
	if got := SQLLiteral(MySQLDialect{}, Cell{Text: "0x", Kind: "binary"}); got != "X''" {
		t.Errorf("empty binary = %q", got)
	}
	var rows int
	if err := WriteRows(sinkFunc{cols: func([]Column) error { return nil }, row: func([]Cell) error { rows++; return nil }}, exportResult()); err != nil || rows != 3 {
		t.Errorf("WriteRows: %d %v", rows, err)
	}
	if err := WriteRows(sinkFunc{cols: func([]Column) error { return nil }, row: func([]Cell) error { return errors.New("stop") }}, exportResult()); err == nil {
		t.Error("row error lost")
	}
}

// ExportToFile writes nothing when the format is unknown, when the cancel
// comes after the last row, or when the file appeared while exporting.
func TestExportToFileLateProblems(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")
	rowsSrc := func(ctx context.Context, s RowSink) error { return WriteRows(s, exportResult()) }
	if _, err := ExportToFile(context.Background(), path, "xlsx", ExportOptions{}, false, rowsSrc, nil); err == nil {
		t.Error("unknown format")
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, err := ExportToFile(ctx, path, FormatCSV, ExportOptions{}, false, func(ctx context.Context, s RowSink) error {
		err := rowsSrc(ctx, s)
		cancel()
		return err
	}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("late cancel = %v", err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	_, err = ExportToFile(ctx2, path, FormatCSV, ExportOptions{}, false, func(ctx context.Context, s RowSink) error {
		cancel2()
		return s.Row([]Cell{{Text: "x"}})
	}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancel at a row = %v", err)
	}
	_, err = ExportToFile(context.Background(), path, FormatCSV, ExportOptions{}, false, func(ctx context.Context, s RowSink) error {
		if err := os.WriteFile(path, []byte("someone else's"), 0o644); err != nil {
			return err
		}
		return rowsSrc(ctx, s)
	}, nil)
	if !errors.Is(err, ErrExportExists) {
		t.Errorf("file appeared = %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "someone else's" {
		t.Errorf("the other file was replaced: %q", b)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp files left: %v", entries)
	}
}
