package dbclient

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// failingRunner answers each statement from a table; a statement that
// starts with failAt returns an error.
type failingRunner struct {
	failAt string
	rows   map[string][][]Cell
}

func (f failingRunner) Run(_ context.Context, sql string, _ RunOptions) Result {
	if f.failAt != "" && strings.HasPrefix(sql, f.failAt) {
		return Result{Err: errors.New("denied: " + f.failAt)}
	}
	for prefix, rows := range f.rows {
		if strings.HasPrefix(sql, prefix) {
			return Result{Rows: rows}
		}
	}
	return Result{}
}

func textCells(vals ...string) []Cell {
	out := make([]Cell, len(vals))
	for i, v := range vals {
		out[i] = Cell{Text: v}
	}
	return out
}

func TestIntrospectSQLiteReadsEveryPragmaAndStopsAtTheFirstFailure(t *testing.T) {
	rows := map[string][][]Cell{
		"PRAGMA database_list":    {textCells("0", "main", ""), textCells("short")},
		"SELECT":                  {textCells("t", "table"), textCells("one")},
		"PRAGMA table_info":       {textCells("0", "id", "INTEGER", "1", "", "1"), textCells("1", "n", "TEXT", "0", "", "0"), textCells("short")},
		"PRAGMA index_list":       {textCells("0", "ix", "1"), textCells("short")},
		"PRAGMA index_info":       {textCells("0", "0", "n"), textCells("short")},
		"PRAGMA foreign_key_list": {textCells("0", "0", "u", "n", "id"), textCells("short")},
		"SELECT sql":              {textCells("CREATE TABLE t (id INTEGER PRIMARY KEY, n TEXT)"), textCells("")},
	}
	ctx := context.Background()
	top, err := introspectSQLite(ctx, failingRunner{rows: rows}, IntrospectionRequest{})
	if err != nil || len(top.Databases) != 1 || top.Databases[0] != "main" {
		t.Errorf("top = %+v %v", top, err)
	}
	obj, err := introspectSQLite(ctx, failingRunner{rows: rows}, IntrospectionRequest{Object: "t"})
	if err != nil || len(obj.Columns) != 2 || !obj.Columns[0].PK || len(obj.Indexes) != 1 || !obj.Indexes[0].Unique || obj.Indexes[0].Columns[0] != "n" || len(obj.ForeignKeys) != 1 {
		t.Errorf("object = %+v %v", obj, err)
	}
	for _, at := range []string{"PRAGMA database_list", "SELECT name", "PRAGMA table_info", "PRAGMA index_list", "PRAGMA index_info", "PRAGMA foreign_key_list"} {
		req := IntrospectionRequest{Object: "t"}
		if at == "PRAGMA database_list" || at == "SELECT name" {
			req = IntrospectionRequest{}
		}
		if _, err := introspectSQLite(ctx, failingRunner{failAt: at, rows: rows}, req); err == nil || !strings.Contains(err.Error(), "denied") {
			t.Errorf("%s failing = %v", at, err)
		}
	}
}

func TestSQLSessionOnAClosedPool(t *testing.T) {
	ctx := context.Background()
	for _, readonly := range []bool{false, true} {
		f := &fakeSQL{}
		s := fakeSession(t, f, PostgresDialect{}, "postgres", readonly)
		if err := s.db.Close(); err != nil {
			t.Fatal(err)
		}
		if r := s.Run(ctx, "SELECT 1", RunOptions{}); r.Err == nil {
			t.Errorf("readonly %v: a closed pool must be an error", readonly)
		}
	}
	rw := fakeSession(t, &fakeSQL{}, PostgresDialect{}, "postgres", false)
	_ = rw.db.Close()
	if err := rw.Begin(ctx); err == nil {
		t.Error("begin on a closed pool: want error")
	}
	if err := rw.Ping(ctx); err == nil {
		t.Error("ping on a closed pool: want error")
	}
}
