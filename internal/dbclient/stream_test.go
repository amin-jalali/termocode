package dbclient

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

func streamSession(name string, stub *dbtest.Stub, readonly bool) *SQLSession {
	db := dbtest.Open(name, stub)
	s := newSQLSession(db, PostgresDialect{}, "postgres", Conn{}, readonly)
	s.newDB = func() (*sql.DB, error) { return dbtest.Open(name+"-ro", stub), nil }
	return s
}

func TestSQLSessionStreamRowsHasNoRowCap(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"n"}, ColumnTypes: []string{"INT4"}, Rows: [][]driver.Value{{int64(7)}}, RepeatRows: 25000}
	s := streamSession("stream-all", stub, false)
	defer s.Close()
	var cols []Column
	rows := 0
	var last Cell
	err := s.StreamRows(context.Background(), "SELECT n FROM t", nil, sinkFunc{
		cols: func(c []Column) error { cols = c; return nil },
		row:  func(r []Cell) error { rows++; last = r[0]; return nil },
	})
	if err != nil || rows != 25000 {
		t.Fatalf("rows=%d err=%v", rows, err)
	}
	if len(cols) != 1 || cols[0].Name != "n" || last.Text != "7" || last.Kind != "number" {
		t.Fatalf("cols=%+v last=%+v", cols, last)
	}
}

func TestSQLSessionStreamRowsStopsWhenTheSinkStops(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"n"}, Rows: [][]driver.Value{{int64(1)}}, RepeatRows: 1000000}
	s := streamSession("stream-stop", stub, false)
	defer s.Close()
	stop := errors.New("stop")
	n := 0
	err := s.StreamRows(context.Background(), "SELECT n FROM t", nil, sinkFunc{
		cols: func([]Column) error { return nil },
		row: func([]Cell) error {
			if n++; n == 100 {
				return stop
			}
			return nil
		},
	})
	if !errors.Is(err, stop) || stub.ReadCount() > 200 {
		t.Fatalf("err=%v read=%d", err, stub.ReadCount())
	}
}

func TestSQLSessionStreamRowsHonoursCancel(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"n"}, Rows: [][]driver.Value{{int64(1)}}, RepeatRows: 1000000}
	s := streamSession("stream-cancel", stub, false)
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	err := s.StreamRows(ctx, "SELECT n FROM t", nil, sinkFunc{
		cols: func([]Column) error { return nil },
		row: func([]Cell) error {
			if n++; n == 50 {
				cancel()
			}
			return nil
		},
	})
	if !errors.Is(err, context.Canceled) || stub.ReadCount() > 200 {
		t.Fatalf("err=%v read=%d", err, stub.ReadCount())
	}
}

func TestSQLSessionStreamRowsReadOnlySessionStartsAReadOnlyTransaction(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"n"}, Rows: [][]driver.Value{{int64(1)}}, RepeatRows: 3}
	s := streamSession("stream-ro", stub, true)
	defer s.Close()
	err := s.StreamRows(context.Background(), "SELECT n FROM t", nil, sinkFunc{
		cols: func([]Column) error { return nil },
		row:  func([]Cell) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(stub.Recorded(), "\n")
	if !strings.HasPrefix(got, "BEGIN READ ONLY\n") || !strings.Contains(got, "SELECT n FROM t") || !strings.HasSuffix(got, "ROLLBACK") {
		t.Fatalf("statements:\n%s", got)
	}
}
